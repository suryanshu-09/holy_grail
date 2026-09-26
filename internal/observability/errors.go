// Error reporting for production (PLAN5 Phase 26 — Error tracking).
//
// Reporter posts redacted error payloads to the endpoint configured via the
// SENTRY_DSN environment variable using only the standard library
// (net/http + encoding/json). When SENTRY_DSN is empty the reporter is
// disabled and Report is a no-op (aside from a local slog entry), so local
// development works with no configuration.
//
// All methods are nil-safe: a nil *Reporter never panics. Report never
// propagates transport errors — error tracking must never break the request
// path — and never sends private data: messages and string extras are
// passed through RedactValue (email masking, whitespace collapsing,
// truncation), and the DSN secret itself is never logged.
package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// reportTimeout bounds the outbound POST so a slow/unreachable endpoint can
// never stall request handling or process shutdown for long.
const reportTimeout = 5 * time.Second

// reportMaxLen caps redacted message/extra strings in outbound payloads.
const reportMaxLen = 500

// reportPayload is the JSON body POSTed to SENTRY_DSN.
type reportPayload struct {
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Time    string         `json:"time"`
	Extra   map[string]any `json:"extra,omitempty"`
}

// Reporter posts error reports to a Sentry-compatible DSN endpoint.
// The zero value is disabled; use NewReporter/NewReporterFromEnv.
type Reporter struct {
	dsn    string
	client *http.Client
	log    *slog.Logger
}

// NewReporter returns a Reporter posting to dsn (empty dsn = disabled).
// A nil logger falls back to slog.Default(). It never dials out.
func NewReporter(dsn string, logger *slog.Logger) *Reporter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reporter{
		dsn:    dsn,
		client: &http.Client{Timeout: reportTimeout},
		log:    logger,
	}
}

// NewReporterFromEnv returns a Reporter configured from the SENTRY_DSN
// environment variable (disabled when empty/unset).
func NewReporterFromEnv() *Reporter {
	return NewReporter(os.Getenv("SENTRY_DSN"), nil)
}

// Enabled reports whether the reporter will attempt to post reports.
// It is nil-safe (nil reporter is disabled).
func (r *Reporter) Enabled() bool {
	return r != nil && r.dsn != ""
}

// Report sends err to the configured endpoint with redacted extras.
// It is nil-safe: a nil reporter, nil error, or disabled reporter returns
// immediately without network I/O. Transport/encoding failures are logged
// locally at Warn and never returned, so callers need no error handling.
func (r *Reporter) Report(err error, extra map[string]any) {
	if r == nil || err == nil || r.dsn == "" {
		return
	}
	msg := SanitizeError(err)
	payload := reportPayload{
		Level:   "error",
		Message: msg,
		Time:    time.Now().UTC().Format(time.RFC3339),
		Extra:   redactExtras(extra),
	}
	body, jErr := json.Marshal(payload)
	if jErr != nil {
		r.log.Warn("error report encoding failed", "error", SanitizeError(jErr))
		return
	}
	req, rErr := http.NewRequest(http.MethodPost, r.dsn, bytes.NewReader(body))
	if rErr != nil {
		r.log.Warn("error report request failed", "error", SanitizeError(rErr))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, hErr := r.client.Do(req)
	if hErr != nil {
		r.log.Warn("error report delivery failed", "error", SanitizeError(hErr))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Never log the DSN (it carries the secret key); status is enough.
		r.log.Warn("error report rejected", "status_code", resp.StatusCode)
	}
}

// redactExtras returns a copy of extra with string values redacted.
// Non-string values pass through; a nil/empty input yields nil so the
// field is omitted from the payload.
func redactExtras(extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return nil
	}
	out := make(map[string]any, len(extra))
	for k, v := range extra {
		if s, ok := v.(string); ok {
			out[k] = RedactValue(s, reportMaxLen)
			continue
		}
		out[k] = v
	}
	return out
}
