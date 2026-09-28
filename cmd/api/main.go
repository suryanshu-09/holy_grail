package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"

	apihttp "github.com/suryanshu-09/holy_grail/internal/http"

	"github.com/suryanshu-09/holy_grail/internal/auth"
	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/database"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/embeddings"
	"github.com/suryanshu-09/holy_grail/internal/extraction"
	"github.com/suryanshu-09/holy_grail/internal/jobs"
	"github.com/suryanshu-09/holy_grail/internal/llm"
	"github.com/suryanshu-09/holy_grail/internal/logging"
	"github.com/suryanshu-09/holy_grail/internal/observability"
	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
	"github.com/suryanshu-09/holy_grail/internal/search"
	"github.com/suryanshu-09/holy_grail/internal/storage"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

const shutdownTimeout = 10 * time.Second

// openAIVisionAdapter bridges llm.VisionDescriber (OpenAI chat completions
// with image_url) to extraction.VisionDescriber so the extraction pipeline
// can describe images without an import cycle between the two packages.
// The figure-type string values match, so conversion is a plain field copy.
type openAIVisionAdapter struct {
	inner llm.VisionDescriber
}

// Describe implements extraction.VisionDescriber. It is nil-safe: a nil
// adapter or nil inner backend reports unknown without an error, mirroring
// the noop fallback. Backend errors are passed through so the pipeline can
// log and skip vision non-fatally.
func (a *openAIVisionAdapter) Describe(ctx context.Context, in extraction.DescribeInput) (extraction.DescribeOutput, error) {
	if a == nil || a.inner == nil {
		return extraction.DescribeOutput{FigureType: extraction.FigureTypeUnknown, DescribedBy: "noop"}, nil
	}
	out, err := a.inner.Describe(ctx, llm.VisionDescribeInput{
		Name:    in.Name,
		Page:    in.Page,
		Path:    in.Path,
		Data:    in.Data,
		Format:  in.Format,
		Context: in.Context,
	})
	res := extraction.DescribeOutput{
		Description: out.Description,
		FigureType:  out.FigureType,
		DescribedBy: out.DescribedBy,
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

// storageConfigFromApp builds an S3Config from AppConfig, falling back to
// the AWS_* credential/region conventions (see S3ConfigFromEnv) when the
// S3_* variants are empty so both conventions keep working.
func storageConfigFromApp(cfg *config.AppConfig) storage.S3Config {
	s3cfg := storage.S3Config{
		Endpoint:  cfg.S3Endpoint,
		Bucket:    cfg.S3Bucket,
		Region:    cfg.S3Region,
		AccessKey: cfg.S3AccessKey,
		SecretKey: cfg.S3SecretKey,
	}
	env := storage.S3ConfigFromEnv()
	if s3cfg.Endpoint == "" {
		s3cfg.Endpoint = env.Endpoint
	}
	if s3cfg.Bucket == "" {
		s3cfg.Bucket = env.Bucket
	}
	if s3cfg.Region == "" {
		s3cfg.Region = env.Region
	}
	if s3cfg.AccessKey == "" {
		s3cfg.AccessKey = env.AccessKey
	}
	if s3cfg.SecretKey == "" {
		s3cfg.SecretKey = env.SecretKey
	}
	return s3cfg
}

func main() {
	cfg := config.NewAppConfig()
	if err := cfg.ValidateProduction(); err != nil {
		slog.Error("invalid production configuration", "error", err)
		os.Exit(1)
	}

	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)

	// Production observability (Phase 26): JSON logging is selected by
	// logging.New when APP_ENV=production; the error reporter posts
	// redacted reports to SENTRY_DSN (disabled when empty) and the
	// monitor backs GET /api/v1/metrics (uptime/goroutines/db stats).
	reporter := observability.NewReporterFromEnv()
	if reporter.Enabled() {
		logger.Info("error reporting enabled")
	} else {
		logger.Info("error reporting disabled (no SENTRY_DSN)")
	}

	db, err := database.NewConnection(&cfg)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		reporter.Report(err, map[string]any{"operation": "db_connect"})
		os.Exit(1)
	}
	defer db.Close()
	monitor := apihttp.NewMonitor(db)

	documentRepo := documents.NewRepository(db)
	questionRepo := questions.NewRepository(db)
	topicRepo := topics.NewRepository(db)
	embeddingRepo := embeddings.NewRepository(db)

	store, err := storage.NewStorageFromConfig(cfg.StorageBackend, cfg.DataDir, storageConfigFromApp(&cfg))
	if err != nil {
		logger.Error("failed to initialise storage", "backend", cfg.StorageBackend, "error", err)
		os.Exit(1)
	}
	logger.Info("storage initialised", "backend", cfg.StorageBackend)

	extractor, err := extraction.NewService(cfg.DataDir)
	if err != nil {
		logger.Error("failed to initialise extraction service", "error", err)
		os.Exit(1)
	}
	if ocr, ocrErr := extraction.NewTesseractOCR(); ocrErr == nil {
		extractor = extractor.WithOCRer(ocr)
	} else {
		logger.Warn("OCR fallback disabled", "reason", ocrErr)
	}

	extractionSvc, err := extraction.NewExtractionService(cfg.DataDir, extractor, documentRepo, questionRepo)
	if err != nil {
		logger.Error("failed to initialise extraction pipeline", "error", err)
		os.Exit(1)
	}
	// Structured step logging (PLAN4 Phase 23): pipeline steps emit
	// document_id/job_id/question_id/operation/duration/status/error.
	extractionSvc = extractionSvc.WithStepLogger(observability.NewStepLogger(logger))
	// Wire topic classifier: LLM when OPENAI_API_KEY set, otherwise heuristic fallback.
	// Also wire LLM fallback for extraction when key is present (reuses same client).
	var embeddingPipeline *embeddings.Service
	var quizLLM quiz.QuizLLM
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		openai, oErr := llm.NewOpenAIClient(key, cfg.ChatModel, "")
		if oErr != nil {
			logger.Warn("failed to create OpenAI client", "error", oErr)
			heuristic := topics.NewHeuristicClassifier()
			extractionSvc = extractionSvc.WithTopicClassifier(heuristic, topicRepo)
			logger.Info("heuristic topic classifier enabled (OpenAI client creation failed)")
		} else {
			openai.SetAILogger(observability.NewAILogger(logger))
			fallback := &extraction.LLMFallback{Client: openai, MaxPages: 3}
			extractionSvc = extractionSvc.WithLLMFallback(fallback)
			logger.Info("LLM fallback enabled for extraction")
			classifier := topics.NewClassifier(openai, 3, 100*time.Millisecond)
			extractionSvc = extractionSvc.WithTopicClassifier(classifier, topicRepo)
			logger.Info("LLM topic classifier enabled")
			quizLLM = openai
			logger.Info("LLM quiz generator enabled")
		}
		// Wire vision describer: OpenAI vision when OPENAI_API_KEY is present.
		// Model resolves from optional OPENAI_VISION_MODEL (default gpt-4o-mini),
		// so no new required env. Failures are non-fatal: the pipeline runs
		// without descriptions. Without a key nothing is attached (nil-safe).
		if vdesc, vErr := llm.NewOpenAIVisionDescriber(key, "", ""); vErr != nil {
			logger.Warn("vision describer disabled", "error", vErr)
		} else {
			vdesc.SetAILogger(observability.NewAILogger(logger))
			extractionSvc = extractionSvc.WithVisionDescriber(&openAIVisionAdapter{inner: vdesc})
			logger.Info("vision describer enabled", "model", vdesc.Model)
		}
		embedder, embedErr := embeddings.NewOpenAIEmbedder(key, cfg.EmbeddingModel, "")
		if embedErr != nil {
			logger.Warn("embedding pipeline disabled", "error", embedErr)
		} else {
			embedder.SetAILogger(observability.NewAILogger(logger))
			embeddingPipeline, embedErr = embeddings.NewService(questionRepo, topicRepo, embeddingRepo, embedder)
			if embedErr != nil {
				logger.Warn("embedding pipeline disabled", "error", embedErr)
			} else {
				embeddingPipeline.BatchSize = cfg.EmbeddingBatchSize
				extractionSvc = extractionSvc.WithEmbeddingPipeline(embeddingPipeline)
				logger.Info("OpenAI embedding pipeline enabled", "model", embedder.Model())
			}
		}
	} else {
		heuristic := topics.NewHeuristicClassifier()
		extractionSvc = extractionSvc.WithTopicClassifier(heuristic, topicRepo)
		logger.Info("heuristic topic classifier enabled (no OPENAI_API_KEY)")
	}

	classificationPipeline, err := extraction.NewClassificationPipeline(extractionSvc, documentRepo, questionRepo)
	if err != nil {
		logger.Error("failed to initialise classification pipeline", "error", err)
		os.Exit(1)
	}

	// Wire vector + hybrid search: requires an embedder (OPENAI_API_KEY) and pgvector.
	// Hybrid falls back to vector-only when the keyword branch or embedder is unavailable.
	var searcher apihttp.Searcher
	var hybridSearcher apihttp.HybridSearcher
	var vectorSvc *search.Service
	var hybridSvc *search.HybridService
	var keywordRepo search.KeywordRepository
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		if queryEmbedder, qErr := embeddings.NewOpenAIEmbedder(key, cfg.EmbeddingModel, ""); qErr == nil {
			queryEmbedder.SetAILogger(observability.NewAILogger(logger))
			repo := search.NewRepository(db, search.DefaultMetric, queryEmbedder.Model())
			if svc, sErr := search.NewService(repo, queryEmbedder); sErr == nil {
				searcher = svc
				vectorSvc = svc
				logger.Info("vector search enabled", "metric", svc.Metric(), "model", svc.Model())
			} else {
				logger.Warn("vector search disabled", "error", sErr)
			}
			keywordRepo = search.NewKeywordRepository(db)
			if hs, hErr := search.NewHybridService(repo, keywordRepo, queryEmbedder); hErr == nil {
				hs.SetReranker(search.NewExactMatchReranker(0.1))
				hybridSearcher = hs
				hybridSvc = hs
				logger.Info("hybrid search enabled", "metric", hs.Metric(), "model", hs.Model())
			} else {
				logger.Warn("hybrid search disabled (vector-only fallback)", "error", hErr)
			}
		} else {
			logger.Warn("vector search embedder failed", "error", qErr)
		}
	} else {
		logger.Info("vector search disabled (no OPENAI_API_KEY)")
	}

	// Wire Phase 17 retrieval evaluation: the debug endpoint
	// (GET /api/v1/debug/eval) runs every strategy over the bundled dataset
	// via this runner. Requires all three backends; nil disables the endpoint.
	var evalRunner apihttp.EvalRunner
	if vectorSvc != nil && keywordRepo != nil && hybridSvc != nil {
		if runner, rErr := search.NewStrategyRunner(vectorSvc, keywordRepo, hybridSvc, search.RunnerConfig{}); rErr == nil {
			evalRunner = runner
			logger.Info("retrieval evaluation enabled (debug /api/v1/debug/eval)")
		} else {
			logger.Warn("retrieval evaluation disabled", "error", rErr)
		}
	} else {
		logger.Info("retrieval evaluation disabled (search pipeline unavailable)")
	}

	// Wire quiz generation: fake-safe deterministic Original-PYQ fallback when
	// no OPENAI key (LLM nil), LLM generator when the key is present.
	// Retrieval prefers hybrid search when available, else the questions list
	// API with topic resolution via the topics service.
	questionsSvc := questions.NewService(questionRepo)
	topicsSvc := topics.NewService(topicRepo)
	var quizRetriever quiz.Retriever
	if hybridSearcher != nil {
		quizRetriever = &quiz.HybridRetriever{Hybrid: hybridSearcher}
		logger.Info("quiz retrieval via hybrid search")
	} else {
		quizRetriever = &quiz.QuestionRetriever{
			Questions: questionsSvc,
			TopicsForQuestion: func(ctx context.Context, questionID string) ([]string, error) {
				ts, err := topicsSvc.ListTopicsForQuestion(ctx, questionID)
				if err != nil {
					return nil, err
				}
				names := make([]string, 0, len(ts))
				for _, t := range ts {
					names = append(names, t.Name)
				}
				return names, nil
			},
		}
		logger.Info("quiz retrieval via questions list (original-PYQ fallback safe)")
	}
	quizGenerator := &quiz.QuizGenerator{Retriever: quizRetriever, LLM: quizLLM}
	if quizLLM == nil {
		logger.Info("quiz LLM disabled (no OPENAI_API_KEY): deterministic Original-PYQ fallback")
	}

	// Wire quiz evaluation (Phase 15): record attempts and compute session
	// metrics (score, accuracy, attempted, correct, incorrect, average time)
	// plus per-topic accuracy and weak topics.
	quizEvalSvc := quiz.NewEvaluationService(quiz.NewEvaluationRepository(db))

	// Wire Phase 19 background jobs: Postgres-backed store with an
	// Asynq bridge. When REDIS_ADDR points at a reachable Redis the bridge
	// publishes task envelopes so the worker (asynq server path) picks jobs
	// up; otherwise it stays persist-only (nil client) and the API still
	// works with no Redis running (a worker in DB poll-loop mode claims them).
	jobStore := jobs.NewPostgresStore(db)
	jobEnqueuer := jobs.NewAsynqEnqueuer(jobStore, asynqPublishClient(logger), jobs.AsynqQueue)
	logger.Info("job queue enabled")

	// Wire Phase 20 authentication: users + sessions + preferences.
	// The handlers stay nil-safe (503 when unconfigured), and the
	// middleware attaches the user from Bearer/cookie credentials so
	// documents and quiz sessions are scoped per user.
	authSvc := auth.NewService(auth.NewPostgresStore(db))
	logger.Info("auth enabled (register/login/sessions/preferences)")

	deps := apihttp.RouterDeps{
		DB:             db,
		Documents:      documents.NewService(documentRepo, store),
		Extraction:     extractionSvc,
		Questions:      questionsSvc,
		Topics:         topicsSvc,
		Classifier:     classificationPipeline,
		Embedder:       embedderPipelineOrNil(embeddingPipeline),
		Searcher:       searcher,
		HybridSearcher: hybridSearcher,
		EvalRunner:     evalRunner,
		Quiz:           quizGenerator,
		QuizEval:       quizEvalSvc,
		QuizHistory:    quizEvalSvc,
		Auth:           authSvc,
		Jobs:           jobStore,
		JobEnqueuer:    jobEnqueuer,
		Reporter:       reporter,
		Monitor:        monitor,
	}

	server := &http.Server{
		Addr:         cfg.Host + ":" + cfg.Port,
		Handler:      apihttp.NewRouter(&cfg, deps),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("starting server", "addr", server.Addr, "env", cfg.Env)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			reporter.Report(err, map[string]any{"operation": "serve"})
			os.Exit(1)
		}
	}()

	<-stop
	logger.Info("shutting down server")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Warn("server forced to shutdown", "error", err)
	} else {
		logger.Info("server exited properly")
	}
}

// embedderPipelineOrNil converts the concrete *embeddings.Service to the
// EmbeddingPipeline interface without creating a typed-nil: a nil *Service
// wrapped in an interface is != nil and would bypass the handler's
// nil guard (503) and panic on first use. Callers must pass the result.
func embedderPipelineOrNil(svc *embeddings.Service) apihttp.EmbeddingPipeline {
	if svc == nil {
		return nil
	}
	return svc
}

// asynqPublishClient builds the Asynq publish bridge for the job enqueuer.
// It returns nil (persist-only mode) when REDIS_ADDR is unset or unreachable
// so the API keeps working with no Redis running. The nil is a true
// interface nil (not a typed-nil pointer) so the bridge's nil guard holds.
func asynqPublishClient(logger *slog.Logger) jobs.AsynqClient {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		return nil
	}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		logger.Warn("job queue publish disabled (redis unreachable, persist-only)", "redis_addr", addr, "error", err)
		return nil
	}
	_ = conn.Close()
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: addr})
	logger.Info("job queue publishing to redis", "redis_addr", addr)
	return &asynqClientAdapter{client: client}
}

// asynqClientAdapter adapts *asynq.Client to jobs.AsynqClient (see
// internal/jobs.AsynqClient). Duplicate/TaskID-conflict errors are treated
// as success: the deterministic TaskID means the task is already queued,
// which is the desired end state for store-level dedup redeliveries.
type asynqClientAdapter struct {
	client *asynq.Client
}

func (a *asynqClientAdapter) Close() {
	if a != nil && a.client != nil {
		_ = a.client.Close()
	}
}

func (a *asynqClientAdapter) EnqueueTask(ctx context.Context, taskType string, payload []byte, opts jobs.AsynqTaskOptions) (string, error) {
	task := asynq.NewTask(taskType, payload)
	asynqOpts := []asynq.Option{
		asynq.Queue(opts.Queue),
		asynq.MaxRetry(opts.MaxRetry),
	}
	if opts.TaskID != "" {
		asynqOpts = append(asynqOpts, asynq.TaskID(opts.TaskID))
	}
	if opts.TimeoutSeconds > 0 {
		asynqOpts = append(asynqOpts, asynq.Timeout(time.Duration(opts.TimeoutSeconds)*time.Second))
	}
	info, err := a.client.EnqueueContext(ctx, task, asynqOpts...)
	if err != nil {
		if errors.Is(err, asynq.ErrDuplicateTask) || errors.Is(err, asynq.ErrTaskIDConflict) {
			return opts.TaskID, nil
		}
		return "", err
	}
	return info.ID, nil
}
