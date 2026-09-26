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
// origin(s) (comma-separated allowlist, "*" allows all). Preflight OPTIONS
// requests are answered directly. When a concrete origin is configured,
// credentialed requests (session cookie) are allowed via
// Access-Control-Allow-Credentials, Vary: Origin is set, and only a
// matching request Origin is echoed back.
func CORS(allowedOrigin string) func(http.Handler) http.Handler {
	allowlist := parseOriginAllowlist(allowedOrigin)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Vary", "Origin")
			if len(allowlist) == 0 {
				h.Set("Access-Control-Allow-Origin", "*")
			} else if len(allowlist) == 1 && allowlist[0] == "*" {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				allowOrigin := allowlist[0]
				if reqOrigin := r.Header.Get("Origin"); reqOrigin != "" {
					for _, o := range allowlist {
						if o == reqOrigin {
							allowOrigin = reqOrigin
							break
						}
					}
				}
				h.Set("Access-Control-Allow-Origin", allowOrigin)
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token")
			h.Set("Access-Control-Max-Age", "86400")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func parseOriginAllowlist(s string) []string {
	var out []string
	for _, part := range splitComma(s) {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			part := s[start:i]
			// trim spaces
			for len(part) > 0 && (part[0] == ' ' || part[0] == '\t') {
				part = part[1:]
			}
			for len(part) > 0 && (part[len(part)-1] == ' ' || part[len(part)-1] == '\t') {
				part = part[:len(part)-1]
			}
			out = append(out, part)
			start = i + 1
		}
	}
	return out
}

// SecurityHeaders sets hardened HTTP security headers, including HSTS
// (HTTPS enforcement), nosniff content-type, deny framing, and a
// restrictive referrer policy.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// RateLimitMiddleware enforces a global per-client-IP rate limit using the
// shared RateLimiter. Excess requests get 429 JSON + Retry-After.
// A nil limiter disables limiting. OPTIONS preflights bypass the limiter
// so CORS handshakes are never throttled.
func RateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			if !limiter.Allow(ClientIP(r)) {
				w.Header().Set("Retry-After", "1")
				Error(w, http.StatusTooManyRequests, "rate limit exceeded, try again later")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
