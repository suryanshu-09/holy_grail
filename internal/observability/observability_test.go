package observability

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// captureHandler records slog records for assertions.
type captureHandler struct {
	mu      sync.Mutex
	records []captured
	level   slog.Level
}

type captured struct {
	level slog.Level
	msg   string
	attrs map[string]any
}

func newCapture(level slog.Level) (*captureHandler, *slog.Logger) {
	h := &captureHandler{level: level}
	return h, slog.New(h)
}

func (h *captureHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	m := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, captured{level: r.Level, msg: r.Message, attrs: m})
	h.mu.Unlock()
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func last(h *captureHandler) captured { return h.records[len(h.records)-1] }

func TestStepLogRequiredFields(t *testing.T) {
	h, logger := newCapture(slog.LevelInfo)
	sl := NewStepLogger(logger)
	sl.Log(context.Background(), StepEntry{
		DocumentID: "doc-1",
		JobID:      "job-2",
		QuestionID: "q-3",
		Operation:  "question_extraction",
		Duration:   4210 * time.Millisecond,
		Status:     StatusSuccess,
		Extra:      map[string]any{"questions": 87},
	})
	if len(h.records) != 1 {
		t.Fatalf("records = %d, want 1", len(h.records))
	}
	got := last(h).attrs
	for k, want := range map[string]any{
		"document_id": "doc-1",
		"job_id":      "job-2",
		"question_id": "q-3",
		"operation":   "question_extraction",
		"status":      StatusSuccess,
	} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
	if toInt(got["questions"]) != 87 {
		t.Errorf("questions = %v, want 87", got["questions"])
	}
	if _, ok := got["duration"]; !ok {
		t.Error("missing duration attr")
	}
	if _, ok := got["duration_ms"]; !ok {
		t.Error("missing duration_ms attr")
	}
	if _, ok := got["error"]; !ok {
		t.Error("missing error attr")
	}
}

func TestStepLogErrorStatusAndLevel(t *testing.T) {
	h, logger := newCapture(slog.LevelInfo)
	sl := NewStepLogger(logger)
	sl.Log(context.Background(), StepEntry{
		Operation: "embed",
		Err:       errors.New("boom failed"),
	})
	rec := last(h)
	if rec.level != slog.LevelError {
		t.Errorf("level = %v, want Error", rec.level)
	}
	if rec.attrs["status"] != StatusError {
		t.Errorf("status = %v, want error", rec.attrs["status"])
	}
	if rec.attrs["error"] != "boom failed" {
		t.Errorf("error = %v, want sanitized message", rec.attrs["error"])
	}
	if rec.attrs["operation"] != "embed" {
		t.Errorf("operation = %v, want embed", rec.attrs["operation"])
	}
}

func TestStepStartMeasuresDuration(t *testing.T) {
	h, logger := newCapture(slog.LevelInfo)
	sl := NewStepLogger(logger)
	finish := sl.Start(context.Background(), "op", StepEntry{DocumentID: "abc123"})
	time.Sleep(5 * time.Millisecond)
	finish(nil, map[string]any{"questions": 87})
	rec := last(h)
	if rec.level != slog.LevelInfo {
		t.Errorf("level = %v, want Info", rec.level)
	}
	if rec.attrs["status"] != StatusSuccess {
		t.Errorf("status = %v, want success", rec.attrs["status"])
	}
	ms, ok := rec.attrs["duration_ms"].(int64)
	if !ok || ms < 0 {
		t.Errorf("duration_ms = %v, want >= 0 int64", rec.attrs["duration_ms"])
	}
}

func TestAILogFieldsAndTokens(t *testing.T) {
	h, logger := newCapture(slog.LevelInfo)
	al := NewAILogger(logger)
	al.Log(context.Background(), AIEntry{
		Model:         "gpt-4o-mini",
		PromptVersion: "extract-v3",
		InputTokens:   1200,
		OutputTokens:  300,
		Latency:       1500 * time.Millisecond,
		Operation:     "question_extraction",
	})
	rec := last(h)
	if rec.msg != "ai_call" {
		t.Errorf("msg = %q, want ai_call", rec.msg)
	}
	if rec.attrs["model"] != "gpt-4o-mini" {
		t.Errorf("model = %v", rec.attrs["model"])
	}
	if rec.attrs["prompt_version"] != "extract-v3" {
		t.Errorf("prompt_version = %v", rec.attrs["prompt_version"])
	}
	if toInt(rec.attrs["input_tokens"]) != 1200 {
		t.Errorf("input_tokens = %v, want 1200", rec.attrs["input_tokens"])
	}
	if toInt(rec.attrs["output_tokens"]) != 300 {
		t.Errorf("output_tokens = %v, want 300", rec.attrs["output_tokens"])
	}
	if toInt(rec.attrs["total_tokens"]) != 1500 {
		t.Errorf("total_tokens = %v, want 1500", rec.attrs["total_tokens"])
	}
	if _, ok := rec.attrs["latency"]; !ok {
		t.Error("missing latency attr")
	}
	// Private data must never appear: no prompt/response keys exist.
	for _, k := range []string{"prompt", "response", "content", "input", "output_text"} {
		if _, ok := rec.attrs[k]; ok {
			t.Errorf("private key %q must not be logged", k)
		}
	}
}

func TestAILogErrorAndObserveAI(t *testing.T) {
	h, logger := newCapture(slog.LevelInfo)
	al := NewAILogger(logger)
	errBoom := errors.New("provider timeout")
	_ = al.ObserveAI(context.Background(), AIEntry{Model: "m", PromptVersion: "v1"}, func() (int, int, error) {
		return 10, 5, errBoom
	})
	rec := last(h)
	if rec.level != slog.LevelError {
		t.Errorf("level = %v, want Error", rec.level)
	}
	if rec.attrs["status"] != StatusError {
		t.Errorf("status = %v, want error", rec.attrs["status"])
	}
	if rec.attrs["error"] != "provider timeout" {
		t.Errorf("error = %v", rec.attrs["error"])
	}
}

func TestRedaction(t *testing.T) {
	if got := SanitizeError(nil); got != "" {
		t.Errorf("SanitizeError(nil) = %q, want empty", got)
	}
	got := RedactValue("contact user@example.com failed\nwith newline", 500)
	if contains(got, "user@example.com") {
		t.Errorf("email not redacted: %q", got)
	}
	if !contains(got, "[redacted-email]") {
		t.Errorf("missing redaction placeholder: %q", got)
	}
	if contains(got, "\n") {
		t.Errorf("newlines not collapsed: %q", got)
	}
	long := RedactValue(string(make([]byte, 0)), 10)
	if long != "" {
		t.Errorf("empty input = %q, want empty", long)
	}
	big := ""
	for i := 0; i < 600; i++ {
		big += "a"
	}
	if got := RedactValue(big, 500); !contains(got, "[truncated]") {
		t.Errorf("long value not truncated: len=%d", len(got))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || search(s, sub))
}

// toInt normalizes slog numeric values (int64/int) for assertions.
func toInt(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		return -1
	}
}

func search(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestNilLoggerFallback(t *testing.T) {
	if NewStepLogger(nil).Logger() == nil {
		t.Error("StepLogger nil fallback is nil")
	}
	if NewAILogger(nil).Logger() == nil {
		t.Error("AILogger nil fallback is nil")
	}
	// Should not panic.
	NewStepLogger(nil).Log(context.Background(), StepEntry{Operation: "x"})
	NewAILogger(nil).Log(context.Background(), AIEntry{Model: "m"})
}
