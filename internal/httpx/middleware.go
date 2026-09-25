package httpx

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/observability"
)

// LogError logs an error using the default structured logger.
func LogError(msg string, err error) {
	slog.Error(msg, "error", err)
}

// statusRecorder captures the response status for request logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// RequestLogging logs each request with the PLAN4 Phase 23 structured field
// names so HTTP logs join with pipeline/job step logs: document_id, job_id,
// question_id, operation ("http_request"), duration (+ duration_s /
// duration_ms), status ("success" for <400, "error" otherwise) and error.
// Method, path and the numeric status_code ride along as extras; when the
// request context carries correlation ids (see internal/observability
// WithJobID/WithDocumentID/WithQuestionID) they are emitted too.
func RequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		elapsed := time.Since(start)
		status := "success"
		errMsg := ""
		if rec.status >= 400 {
			status = "error"
			errMsg = http.StatusText(rec.status)
		}
		slog.Info("request",
			"document_id", observability.DocumentIDFromContext(r.Context()),
			"job_id", observability.JobIDFromContext(r.Context()),
			"question_id", observability.QuestionIDFromContext(r.Context()),
			"operation", "http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"status_code", rec.status,
			"duration", elapsed.String(),
			"duration_s", elapsed.Seconds(),
			"duration_ms", elapsed.Milliseconds(),
			"error", errMsg,
		)
	})
}

// Recover converts panics into 500 JSON errors instead of crashing the server.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "panic", rec, "path", r.URL.Path)
				Error(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// CORS returns a middleware allowing cross-origin requests from the given
// origin ("*" allows all). Preflight OPTIONS requests are answered directly.
// When a concrete origin is configured, credentialed requests (session
// cookie) are allowed via Access-Control-Allow-Credentials.
func CORS(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := allowedOrigin
			if origin == "" {
				origin = "*"
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if origin != "*" {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
