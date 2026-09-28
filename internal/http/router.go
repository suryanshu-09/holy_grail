package http

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/auth"
	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/extraction"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/jobs"
	"github.com/suryanshu-09/holy_grail/internal/observability"
	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

// APIVersion is the current versioned API prefix.
const APIVersion = "/api/v1"

// Pinger matches the subset of *sql.DB needed by the readiness endpoint.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// RouterDeps wires the handler layer to the service layer.
type RouterDeps struct {
	DB             Pinger
	Documents      *documents.Service
	Extraction     *extraction.ExtractionService
	Questions      *questions.Service
	Topics         *topics.Service
	Classifier     ClassifierPipeline
	Embedder       EmbeddingPipeline
	Searcher       Searcher
	HybridSearcher HybridSearcher
	EvalRunner     EvalRunner
	Quiz           QuizGenerator
	QuizEval       QuizEvaluator
	QuizHistory    QuizHistoryLister
	Auth           *auth.Service
	Jobs           jobs.Store
	JobEnqueuer    jobs.Enqueuer
	// Reporter receives redacted panic reports (nil/disabled = local log only).
	Reporter *observability.Reporter
	// Monitor backs GET /api/v1/metrics (nil = default monitor without db stats).
	Monitor *Monitor
}

// NewRouter builds the versioned route table wrapped in middleware.
func NewRouter(cfg *config.AppConfig, deps RouterDeps) http.Handler {
	mux := http.NewServeMux()

	mux.Handle(APIVersion+"/health", handleHealth())
	mux.Handle(APIVersion+"/ready", handleReady(deps.DB))
	mon := deps.Monitor
	if mon == nil {
		var sqldb *sql.DB
		if v, ok := deps.DB.(*sql.DB); ok {
			sqldb = v
		}
		mon = NewMonitor(sqldb)
	}
	mux.Handle(APIVersion+"/metrics", handleMetrics(mon))
	mux.Handle(APIVersion+"/documents", handleDocuments(deps.Documents, cfg.MaxUploadBytes))
	mux.Handle(APIVersion+"/documents/{id}", handleDocumentByID(deps.Documents))
	mux.Handle("POST "+APIVersion+"/documents/{id}/extract", handleExtractDocument(deps.Documents, deps.Extraction))
	mux.Handle("POST "+APIVersion+"/documents/{id}/extract-preview", handleExtractPreview(deps.Documents, deps.Extraction))
	mux.Handle("POST "+APIVersion+"/documents/{id}/embed", handleEmbedDocument(deps.Documents, deps.Embedder))
	mux.Handle("GET "+APIVersion+"/documents/{id}/images/{name}", handleDocumentImages(cfg.DataDir, deps.Documents))
	mux.Handle("GET "+APIVersion+"/documents/{id}/images", handleListDocumentImages(cfg.DataDir, deps.Documents))
	mux.Handle(APIVersion+"/questions", handleQuestions(deps.Questions))
	mux.Handle("GET "+APIVersion+"/questions/{id}", handleQuestionByID(deps.Questions))
	mux.Handle("GET "+APIVersion+"/questions/{id}/topics", handleGetQuestionTopics(deps.Topics))
	mux.Handle("PATCH "+APIVersion+"/questions/{id}/topics", handleCorrectQuestionTopics(deps.Topics))
	mux.Handle(APIVersion+"/topics", handleTopics(deps.Topics))
	mux.Handle("POST "+APIVersion+"/topics/merge", handleMergeTopics(deps.Topics))
	if deps.Classifier != nil {
		mux.Handle("POST "+APIVersion+"/documents/{id}/classify", handleClassifyDocument(deps.Classifier))
	} else {
		mux.Handle("POST "+APIVersion+"/documents/{id}/classify", handleClassifyDocumentWithServices(deps.Documents, deps.Topics, deps.Questions))
	}
	mux.Handle("GET "+APIVersion+"/topics/{id}/questions", handleTopicQuestions(deps.Questions, deps.Topics))
	mux.Handle(APIVersion+"/search", handleSearch(deps.Searcher, deps.HybridSearcher))
	mux.Handle(APIVersion+"/debug/eval", handleDebugEval(deps.EvalRunner))
	var retrievalTopics RetrievalTopicLister
	if deps.Topics != nil {
		retrievalTopics = deps.Topics
	}
	mux.Handle(APIVersion+"/debug/retrieval", handleDebugRetrieval(deps.Searcher, deps.HybridSearcher, retrievalTopics))
	mux.Handle(APIVersion+"/quiz/generate", handleQuizGenerate(deps.Quiz))
	mux.Handle("POST "+APIVersion+"/quiz/sessions", handleCreateQuizSession(deps.QuizEval))
	mux.Handle("GET "+APIVersion+"/quiz/sessions", handleQuizHistory(deps.QuizHistory))
	mux.Handle("GET "+APIVersion+"/quiz/sessions/{id}", handleGetQuizSession(deps.QuizEval))
	mux.Handle("POST "+APIVersion+"/quiz/sessions/{id}/attempts", handleSubmitQuizAttempt(deps.QuizEval))
	mux.Handle("POST "+APIVersion+"/quiz/sessions/{id}/attempts/bulk", handleSubmitQuizAttemptsBulk(deps.QuizEval))
	mux.Handle("POST "+APIVersion+"/documents/{id}/process", handleProcessDocument(deps.Documents, deps.Jobs, deps.JobEnqueuer))
	mux.Handle("GET "+APIVersion+"/jobs/{id}", handleGetJob(deps.Jobs))
	mux.Handle("GET "+APIVersion+"/documents/{id}/processing-status", handleProcessingStatus(deps.Documents, deps.Jobs))
	mux.Handle("POST "+APIVersion+"/auth/register", handleRegister(deps.Auth, cfg.Env))
	mux.Handle("POST "+APIVersion+"/auth/login", handleLogin(deps.Auth, cfg.Env))
	mux.Handle("POST "+APIVersion+"/auth/logout", handleLogout(deps.Auth, cfg.Env))
	mux.Handle(APIVersion+"/auth/me", handleMe())
	mux.Handle(APIVersion+"/users/me/preferences", handlePreferences(deps.Auth))
	// Phase 27 analytics + study mode (nil-safe: handlers degrade to 503 or
	// deterministic fallbacks when optional deps are missing).
	analyticsDeps := AnalyticsDeps{Eval: deps.QuizEval, History: deps.QuizHistory}
	if deps.Questions != nil {
		analyticsDeps.Questions = deps.Questions
	}
	mux.Handle(APIVersion+"/analytics/mastery", handleAnalyticsMastery(analyticsDeps))
	mux.Handle(APIVersion+"/analytics/history", handleAnalyticsHistory(analyticsDeps))
	mux.Handle(APIVersion+"/analytics/difficulty", handleAnalyticsDifficulty(analyticsDeps))
	mux.Handle(APIVersion+"/analytics/timing", handleAnalyticsTiming(analyticsDeps))
	mux.Handle(APIVersion+"/analytics/readiness", handleAnalyticsReadiness(analyticsDeps))
	studyDeps := StudyDeps{Searcher: deps.Searcher, Quiz: deps.Quiz}
	if deps.Topics != nil {
		studyDeps.Topics = deps.Topics
	}
	mux.Handle(APIVersion+"/study/guide", handleStudyGuide(studyDeps))

	handler := auth.OptionalAuth(deps.Auth)(mux)
	handler = auth.CSRFMiddleware(cfg.CORSAllowedOrigin)(handler)
	if deps.Reporter != nil && deps.Reporter.Enabled() {
		handler = recoverWithReporter(deps.Reporter)(handler)
	} else {
		handler = httpx.Recover(handler)
	}
	handler = httpx.RequestLogging(handler)
	handler = httpx.SecurityHeaders(handler)
	handler = httpx.RateLimitMiddleware(globalRateLimiter(cfg))(handler)
	return httpx.CORS(cfg.CORSAllowedOrigin)(handler)
}

// recoverWithReporter converts panics into 500 JSON errors (like
// httpx.Recover) and additionally forwards a redacted report to the
// configured error tracker. A nil/disabled reporter degrades to local
// logging only; reporting failures never affect the response.
func recoverWithReporter(reporter *observability.Reporter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					err := fmt.Errorf("panic: %v", rec)
					if reporter != nil {
						reporter.Report(err, map[string]any{"path": r.URL.Path})
					}
					slog.Error("panic recovered", "panic", rec, "path", r.URL.Path)
					httpx.Error(w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// globalRateLimiter builds the global per-IP rate limiter from
// RATE_LIMIT_RPS (requests per second). Non-positive values disable it.
func globalRateLimiter(cfg *config.AppConfig) *httpx.RateLimiter {
	if cfg == nil || cfg.RateLimitRPS <= 0 {
		return nil
	}
	return httpx.NewRateLimiter(cfg.RateLimitRPS, time.Second)
}
