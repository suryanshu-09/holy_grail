package observability

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestReporterNilSafe(t *testing.T) {
	var r *Reporter
	if r.Enabled() {
		t.Error("nil reporter must not be enabled")
	}
	// Must not panic.
	r.Report(nil, nil)
	r.Report(errors.New("boom"), map[string]any{"k": "v"})
}

func TestReporterDisabledNoIO(t *testing.T) {
	r := NewReporter("", nil)
	if r.Enabled() {
		t.Error("empty DSN must be disabled")
	}
	// Must not panic or dial out.
	r.Report(errors.New("boom"), nil)
	r.Report(nil, nil)
}

func TestReporterFromEnv(t *testing.T) {
	t.Setenv("SENTRY_DSN", "https://example.invalid/1")
	if !NewReporterFromEnv().Enabled() {
		t.Error("expected enabled reporter with SENTRY_DSN set")
	}
	t.Setenv("SENTRY_DSN", "")
	if NewReporterFromEnv().Enabled() {
		t.Error("expected disabled reporter with empty SENTRY_DSN")
	}
}

func TestReporterRedactsMessageAndExtras(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		raw, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Errorf("body is not JSON: %v", err)
		}
		mu.Lock()
		bodies = append(bodies, decoded)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := NewReporter(srv.URL, nil)
	r.Report(errors.New("contact user@example.com failed"), map[string]any{
		"path":   "/api/v1/quiz",
		"detail": "notify admin@example.org\nwith newline",
		"count":  3,
	})
	if len(bodies) != 1 {
		t.Fatalf("requests = %d, want 1", len(bodies))
	}
	body := bodies[0]
	msg, _ := body["message"].(string)
	if strings.Contains(msg, "user@example.com") {
		t.Errorf("email not redacted in message: %q", msg)
	}
	if !strings.Contains(msg, "[redacted-email]") {
		t.Errorf("missing redaction placeholder in message: %q", msg)
	}
	extra, _ := body["extra"].(map[string]any)
	detail, _ := extra["detail"].(string)
	if strings.Contains(detail, "admin@example.org") || strings.Contains(detail, "\n") {
		t.Errorf("extra not redacted: %q", detail)
	}
	if extra["path"] != "/api/v1/quiz" {
		t.Errorf("path extra = %v, want preserved", extra["path"])
	}
	if body["level"] != "error" {
		t.Errorf("level = %v, want error", body["level"])
	}
}

func TestReporterDeliveryFailureIsSilent(t *testing.T) {
	// Unroutable endpoint: Report must not panic, block long, or propagate.
	r := NewReporter("http://127.0.0.1:1/unreachable", nil)
	r.Report(errors.New("boom"), nil)
}

func TestReporterNon2xxIsSilent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	r := NewReporter(srv.URL, nil)
	r.Report(errors.New("boom"), nil)
}
