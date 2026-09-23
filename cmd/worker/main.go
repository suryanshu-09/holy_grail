// Command worker runs Phase 19 background jobs: it connects to PostgreSQL
// and, when REDIS_ADDR is set, serves Asynq handlers for the four job types
// (process_document, extract_questions, classify_questions,
// generate_embeddings); otherwise it polls the jobs store directly. Each
// handler executes ExtractionService pipeline steps through jobs.Runner,
// which provides timeout contexts, progress callbacks, retry with backoff,
// and structured logging (document_id/job_id/operation/duration).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"

	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/database"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/embeddings"
	"github.com/suryanshu-09/holy_grail/internal/extraction"
	"github.com/suryanshu-09/holy_grail/internal/jobs"
	"github.com/suryanshu-09/holy_grail/internal/llm"
	"github.com/suryanshu-09/holy_grail/internal/logging"
	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

const (
	// asynqConcurrency bounds how many tasks run at once. Document
	// processing is expensive (PDF/LLM/embeddings), so stay modest.
	asynqConcurrency = 4
	// pollInterval is the idle delay of the Redis-free DB poll loop.
	pollInterval = 2 * time.Second
	// retryBackoffBase is the base delay between inline retry attempts.
	retryBackoffBase = 2 * time.Second
)

// openAIVisionAdapter bridges llm.VisionDescriber to
// extraction.VisionDescriber so the pipeline can describe images without an
// import cycle between the two packages. It mirrors cmd/api.
type openAIVisionAdapter struct {
	inner llm.VisionDescriber
}

// Describe implements extraction.VisionDescriber. It is nil-safe: a nil
// adapter or nil inner backend reports unknown without an error.
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

// jobPayload carries optional per-job inputs from the envelope payload.
type jobPayload struct {
	DocumentID  string `json:"document_id"`
	StoragePath string `json:"storage_path"`
}

// extractionProcessor implements jobs.Processor by calling ExtractionService
// pipeline steps. It owns the Processor side of the jobs/extraction boundary:
// the jobs package never imports extraction.
type extractionProcessor struct {
	dataDir    string
	docs       documents.Repository
	pages      *extraction.Service
	pipeline   *extraction.ExtractionService
	embeddings *embeddings.Service
	logger     *slog.Logger
}

var _ jobs.Processor = (*extractionProcessor)(nil)

func (p *extractionProcessor) log() *slog.Logger {
	if p.logger == nil {
		return slog.Default()
	}
	return p.logger
}

// resolveDocument determines the document id and its stored PDF path: the
// authoritative document id comes from the job row (falling back to the task
// payload), and the storage path from the documents repository (falling back
// to the payload, then to the canonical layout resolved by the pipeline).
func (p *extractionProcessor) resolveDocument(ctx context.Context, job jobs.Job) (documentID, storagePath string, err error) {
	if job.DocumentID != nil && strings.TrimSpace(*job.DocumentID) != "" {
		documentID = strings.TrimSpace(*job.DocumentID)
	}
	var payload jobPayload
	if len(job.Payload) > 0 {
		_ = json.Unmarshal(job.Payload, &payload)
	}
	if documentID == "" {
		documentID = strings.TrimSpace(payload.DocumentID)
	}
	if documentID == "" {
		return "", "", fmt.Errorf("worker: job %q carries no document id", job.ID)
	}
	if doc, gerr := p.docs.GetByID(ctx, documentID); gerr == nil && doc.StoragePath != nil {
		storagePath = *doc.StoragePath
	}
	if storagePath == "" {
		storagePath = payload.StoragePath
	}
	return documentID, storagePath, nil
}

// pdfPath resolves the absolute PDF path for split stages that re-read the
// stored file, mirroring the pipeline's canonical layout with traversal
// guards. An empty storagePath falls back to documents/<id>/original.pdf.
func (p *extractionProcessor) pdfPath(documentID, storagePath string) (string, error) {
	if documentID == "" || strings.ContainsAny(documentID, `/\`) || strings.Contains(documentID, "..") {
		return "", fmt.Errorf("worker: unsafe document id %q", documentID)
	}
	rel := filepath.Join("documents", documentID, "original.pdf")
	if storagePath != "" {
		rel = filepath.FromSlash(storagePath)
	}
	absRoot, err := filepath.Abs(p.dataDir)
	if err != nil {
		return "", fmt.Errorf("worker: resolve data dir: %w", err)
	}
	dest := filepath.Join(absRoot, rel)
	parent, err := filepath.Rel(absRoot, dest)
	if err != nil || parent == ".." || strings.HasPrefix(parent, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("worker: storage path escapes data root: %q", storagePath)
	}
	return dest, nil
}

// report is a best-effort progress helper: persistence failures are logged
// and never fail the job itself.
func (p *extractionProcessor) report(ctx context.Context, rep jobs.ProgressFunc, job jobs.Job, progress int, step jobs.ProgressStep) {
	if rep == nil {
		return
	}
	if err := rep(ctx, progress, step); err != nil {
		p.log().Debug("worker: progress report failed (non-fatal)",
			"job_id", job.ID, "step", string(step), "error", err.Error())
	}
}

// ProcessDocument runs the full pipeline for one document.
func (p *extractionProcessor) ProcessDocument(ctx context.Context, job jobs.Job, rep jobs.ProgressFunc) error {
	documentID, storagePath, err := p.resolveDocument(ctx, job)
	if err != nil {
		return err
	}
	p.report(ctx, rep, job, int(jobs.ProgressForStep(jobs.StepUploaded)), jobs.StepUploaded)
	p.report(ctx, rep, job, int(jobs.ProgressForStep(jobs.StepExtracting)), jobs.StepExtracting)
	if _, err := p.pipeline.Extract(ctx, documentID, storagePath); err != nil {
		return err
	}
	// The full pipeline already ran every stage; sweep the remaining
	// milestones so the UI progress bar completes.
	p.report(ctx, rep, job, int(jobs.ProgressForStep(jobs.StepQuestions)), jobs.StepQuestions)
	p.report(ctx, rep, job, int(jobs.ProgressForStep(jobs.StepImages)), jobs.StepImages)
	p.report(ctx, rep, job, int(jobs.ProgressForStep(jobs.StepClassifying)), jobs.StepClassifying)
	p.report(ctx, rep, job, int(jobs.ProgressForStep(jobs.StepEmbeddings)), jobs.StepEmbeddings)
	return nil
}

// ExtractQuestions re-runs page extraction, then persists detected questions.
func (p *extractionProcessor) ExtractQuestions(ctx context.Context, job jobs.Job, rep jobs.ProgressFunc) error {
	documentID, storagePath, err := p.resolveDocument(ctx, job)
	if err != nil {
		return err
	}
	p.report(ctx, rep, job, int(jobs.ProgressForStep(jobs.StepExtracting)), jobs.StepExtracting)
	pdfPath, err := p.pdfPath(documentID, storagePath)
	if err != nil {
		return err
	}
	pages, err := p.pages.ExtractFile(ctx, documentID, pdfPath)
	if err != nil {
		return fmt.Errorf("worker: extract pages: %w", err)
	}
	p.report(ctx, rep, job, int(jobs.ProgressForStep(jobs.StepQuestions)), jobs.StepQuestions)
	if err := p.pipeline.ExtractQuestions(ctx, pages); err != nil {
		return fmt.Errorf("worker: extract questions: %w", err)
	}
	return nil
}

// ClassifyQuestions runs topic classification for a document's questions.
func (p *extractionProcessor) ClassifyQuestions(ctx context.Context, job jobs.Job, rep jobs.ProgressFunc) error {
	documentID, _, err := p.resolveDocument(ctx, job)
	if err != nil {
		return err
	}
	p.report(ctx, rep, job, int(jobs.ProgressForStep(jobs.StepClassifying)), jobs.StepClassifying)
	if err := p.pipeline.ClassifyDocument(ctx, documentID); err != nil {
		return fmt.Errorf("worker: classify questions: %w", err)
	}
	return nil
}

// GenerateEmbeddings embeds a document's questions. When no embedder is
// configured it logs and succeeds, mirroring the pipeline's non-fatal
// embedding behavior inside ProcessDocument.
func (p *extractionProcessor) GenerateEmbeddings(ctx context.Context, job jobs.Job, rep jobs.ProgressFunc) error {
	documentID, _, err := p.resolveDocument(ctx, job)
	if err != nil {
		return err
	}
	p.report(ctx, rep, job, int(jobs.ProgressForStep(jobs.StepEmbeddings)), jobs.StepEmbeddings)
	if p.embeddings == nil {
		p.log().Warn("worker: embeddings skipped (no embedder configured)",
			"job_id", job.ID, "document_id", documentID)
		return nil
	}
	if _, err := p.embeddings.EmbedDocument(ctx, documentID); err != nil {
		return fmt.Errorf("worker: generate embeddings: %w", err)
	}
	return nil
}

// supportedJobTypes lists every job type the worker handles.
func supportedJobTypes() []jobs.JobType {
	return []jobs.JobType{
		jobs.TypeProcessDocument,
		jobs.TypeExtractQuestions,
		jobs.TypeClassifyQuestions,
		jobs.TypeGenerateEmbeddings,
	}
}

// makeAsynqHandler builds the Asynq task handler shared by all four job
// types: it decodes the envelope and executes the persistent job through the
// runner (claim, timeout, retry, progress, logging). Malformed payloads wrap
// asynq.SkipRetry so poison tasks archive instead of retrying forever.
func makeAsynqHandler(runner *jobs.Runner, logger *slog.Logger) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, task *asynq.Task) error {
		env, err := jobs.ParseTaskPayload(task.Type(), task.Payload())
		if err != nil {
			logger.Error("worker: malformed task payload, skipping retry",
				"task_type", task.Type(), "error", err.Error())
			return fmt.Errorf("%w: %w", asynq.SkipRetry, err)
		}
		return runner.RunByID(ctx, env.JobID)
	}
}

// runAsynq serves the four job handlers from Redis until ctx is cancelled.
func runAsynq(ctx context.Context, logger *slog.Logger, redisAddr string, runner *jobs.Runner) error {
	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
		asynq.Config{
			Concurrency: asynqConcurrency,
			Queues:      map[string]int{jobs.AsynqQueue: asynqConcurrency},
			Logger:      asynqLogger{logger: logger},
		},
	)
	mux := asynq.NewServeMux()
	handler := makeAsynqHandler(runner, logger)
	for _, t := range supportedJobTypes() {
		taskType := jobs.AsynqTaskType(t)
		mux.HandleFunc(taskType, handler)
		logger.Info("worker: handler registered", "task_type", taskType)
	}

	go func() {
		<-ctx.Done()
		logger.Info("worker: shutting down asynq server")
		srv.Shutdown()
	}()

	logger.Info("worker: serving asynq", "redis_addr", redisAddr)
	if err := srv.Run(mux); err != nil {
		return fmt.Errorf("worker: asynq server: %w", err)
	}
	return nil
}

// asynqLogger adapts slog to asynq's Logger interface.
type asynqLogger struct {
	logger *slog.Logger
}

func (l asynqLogger) Debug(args ...interface{}) { l.logger.Debug(fmt.Sprint(args...)) }
func (l asynqLogger) Info(args ...interface{})  { l.logger.Info(fmt.Sprint(args...)) }
func (l asynqLogger) Warn(args ...interface{})  { l.logger.Warn(fmt.Sprint(args...)) }
func (l asynqLogger) Error(args ...interface{}) { l.logger.Error(fmt.Sprint(args...)) }
func (l asynqLogger) Fatal(args ...interface{}) {
	l.logger.Error(fmt.Sprint(args...))
	os.Exit(1)
}

func main() {
	cfg := config.NewAppConfig()

	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.NewConnection(&cfg)
	if err != nil {
		logger.Error("worker: failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	documentRepo := documents.NewRepository(db)
	questionRepo := questions.NewRepository(db)
	topicRepo := topics.NewRepository(db)
	embeddingRepo := embeddings.NewRepository(db)

	extractor, err := extraction.NewService(cfg.DataDir)
	if err != nil {
		logger.Error("worker: failed to initialise extraction service", "error", err)
		os.Exit(1)
	}
	if ocr, ocrErr := extraction.NewTesseractOCR(); ocrErr == nil {
		extractor = extractor.WithOCRer(ocr)
	} else {
		logger.Warn("worker: OCR fallback disabled", "reason", ocrErr)
	}

	extractionSvc, err := extraction.NewExtractionService(cfg.DataDir, extractor, documentRepo, questionRepo)
	if err != nil {
		logger.Error("worker: failed to initialise extraction pipeline", "error", err)
		os.Exit(1)
	}

	// Wire the classifier, LLM fallback, vision describer, and embedding
	// pipeline exactly like cmd/api so worker output matches inline output.
	var embeddingPipeline *embeddings.Service
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		openai, oErr := llm.NewOpenAIClient(key, "gpt-3.5-turbo", "")
		if oErr != nil {
			logger.Warn("worker: failed to create OpenAI client", "error", oErr)
			extractionSvc = extractionSvc.WithTopicClassifier(topics.NewHeuristicClassifier(), topicRepo)
			logger.Info("worker: heuristic topic classifier enabled (OpenAI client creation failed)")
		} else {
			extractionSvc = extractionSvc.WithLLMFallback(&extraction.LLMFallback{Client: openai, MaxPages: 3})
			logger.Info("worker: LLM fallback enabled for extraction")
			extractionSvc = extractionSvc.WithTopicClassifier(topics.NewClassifier(openai, 3, 100*time.Millisecond), topicRepo)
			logger.Info("worker: LLM topic classifier enabled")
		}
		if vdesc, vErr := llm.NewOpenAIVisionDescriber(key, "", ""); vErr != nil {
			logger.Warn("worker: vision describer disabled", "error", vErr)
		} else {
			extractionSvc = extractionSvc.WithVisionDescriber(&openAIVisionAdapter{inner: vdesc})
			logger.Info("worker: vision describer enabled", "model", vdesc.Model)
		}
		if embedder, embedErr := embeddings.NewOpenAIEmbedder(key, cfg.EmbeddingModel, ""); embedErr != nil {
			logger.Warn("worker: embedding pipeline disabled", "error", embedErr)
		} else if svc, sErr := embeddings.NewService(questionRepo, topicRepo, embeddingRepo, embedder); sErr != nil {
			logger.Warn("worker: embedding pipeline disabled", "error", sErr)
		} else {
			svc.BatchSize = cfg.EmbeddingBatchSize
			embeddingPipeline = svc
			extractionSvc = extractionSvc.WithEmbeddingPipeline(svc)
			logger.Info("worker: OpenAI embedding pipeline enabled", "model", embedder.Model())
		}
	} else {
		extractionSvc = extractionSvc.WithTopicClassifier(topics.NewHeuristicClassifier(), topicRepo)
		logger.Info("worker: heuristic topic classifier enabled (no OPENAI_API_KEY)")
	}

	// The questions repository is unused directly here: question access goes
	// through the extraction pipeline and embedding service.
	_ = questionRepo

	store := jobs.NewPostgresStore(db)
	processor := &extractionProcessor{
		dataDir:    cfg.DataDir,
		docs:       documentRepo,
		pages:      extractor,
		pipeline:   extractionSvc,
		embeddings: embeddingPipeline,
		logger:     logger,
	}
	runner := jobs.NewRunner(store, processor, logger).
		WithPollInterval(pollInterval).
		WithRetryBackoff(retryBackoffBase)

	// REDIS_ADDR explicitly set (non-empty) selects the Asynq server path;
	// unset selects the Redis-free DB poll loop. The shared config default is
	// intentionally ignored so local runs without Redis Just Work.
	if redisAddr := strings.TrimSpace(os.Getenv("REDIS_ADDR")); redisAddr != "" {
		if err := runAsynq(ctx, logger, redisAddr, runner); err != nil {
			logger.Error("worker: asynq exited", "error", err)
			os.Exit(1)
		}
		return
	}

	logger.Info("worker: REDIS_ADDR unset, using DB poll loop")
	if err := runner.RunPollLoop(ctx); err != nil {
		logger.Error("worker: poll loop exited", "error", err)
		os.Exit(1)
	}
}
