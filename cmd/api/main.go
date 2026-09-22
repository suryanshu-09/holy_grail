package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	apihttp "github.com/suryanshu-09/holy_grail/internal/http"

	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/database"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/embeddings"
	"github.com/suryanshu-09/holy_grail/internal/extraction"
	"github.com/suryanshu-09/holy_grail/internal/llm"
	"github.com/suryanshu-09/holy_grail/internal/logging"
	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
	"github.com/suryanshu-09/holy_grail/internal/search"
	"github.com/suryanshu-09/holy_grail/internal/storage"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

const shutdownTimeout = 10 * time.Second

func main() {
	cfg := config.NewAppConfig()

	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)

	db, err := database.NewConnection(&cfg)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	documentRepo := documents.NewRepository(db)
	questionRepo := questions.NewRepository(db)
	topicRepo := topics.NewRepository(db)
	embeddingRepo := embeddings.NewRepository(db)

	store, err := storage.NewLocalStore(cfg.DataDir)
	if err != nil {
		logger.Error("failed to initialise local storage", "error", err)
		os.Exit(1)
	}

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
	// Wire topic classifier: LLM when OPENAI_API_KEY set, otherwise heuristic fallback.
	// Also wire LLM fallback for extraction when key is present (reuses same client).
	var embeddingPipeline *embeddings.Service
	var quizLLM quiz.QuizLLM
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		openai, oErr := llm.NewOpenAIClient(key, "gpt-3.5-turbo", "")
		if oErr != nil {
			logger.Warn("failed to create OpenAI client", "error", oErr)
			heuristic := topics.NewHeuristicClassifier()
			extractionSvc = extractionSvc.WithTopicClassifier(heuristic, topicRepo)
			logger.Info("heuristic topic classifier enabled (OpenAI client creation failed)")
		} else {
			fallback := &extraction.LLMFallback{Client: openai, MaxPages: 3}
			extractionSvc = extractionSvc.WithLLMFallback(fallback)
			logger.Info("LLM fallback enabled for extraction")
			classifier := topics.NewClassifier(openai, 3, 100*time.Millisecond)
			extractionSvc = extractionSvc.WithTopicClassifier(classifier, topicRepo)
			logger.Info("LLM topic classifier enabled")
			quizLLM = openai
			logger.Info("LLM quiz generator enabled")
		}
		embedder, embedErr := embeddings.NewOpenAIEmbedder(key, cfg.EmbeddingModel, "")
		if embedErr != nil {
			logger.Warn("embedding pipeline disabled", "error", embedErr)
		} else {
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

	deps := apihttp.RouterDeps{
		DB:             db,
		Documents:      documents.NewService(documentRepo, store),
		Extraction:     extractionSvc,
		Questions:      questionsSvc,
		Topics:         topicsSvc,
		Classifier:     classificationPipeline,
		Embedder:       embeddingPipeline,
		Searcher:       searcher,
		HybridSearcher: hybridSearcher,
		EvalRunner:     evalRunner,
		Quiz:           quizGenerator,
		QuizEval:       quizEvalSvc,
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
