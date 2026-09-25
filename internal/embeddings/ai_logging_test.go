package embeddings

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/observability"
)

type aiCapture struct {
	mu      sync.Mutex
	records []aiRecord
}

type aiRecord struct {
	level slog.Level
	msg   string
	attrs map[string]any
}

type aiCaptureHandler struct{ c *aiCapture }

func (h *aiCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *aiCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	m := map[string]any{}
	r.Attrs(func(a slog.Attr) bool { m[a.Key] = a.Value.Any(); return true })
	h.c.mu.Lock()
	h.c.records = append(h.c.records, aiRecord{level: r.Level, msg: r.Message, attrs: m})
	h.c.mu.Unlock()
	return nil
}
func (h *aiCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *aiCaptureHandler) WithGroup(string) slog.Handler      { return h }

func testEmbedServer(t *testing.T, usage string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vector := make([]float32, DefaultDimensions)
		vector[0] = 1
		data := []map[string]any{{"index": 0, "embedding": vector}}
		body := map[string]any{"data": data}
		if usage != "" {
			var u map[string]any
			if err := json.Unmarshal([]byte(usage), &u); err != nil {
				t.Errorf("bad usage fixture: %v", err)
			} else {
				body["usage"] = u
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
}

func TestEmbedLogsModelLatencyTokens(t *testing.T) {
	const secretInput = "super-secret-question-text-xyz"
	ts := testEmbedServer(t, `{"prompt_tokens":42,"total_tokens":42}`)
	defer ts.Close()

	cap := &aiCapture{}
	logger := slog.New(&aiCaptureHandler{c: cap})
	e, err := NewOpenAIEmbedder("k", "", ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	e.client = ts.Client()
	e.SetAILogger(observability.NewAILogger(logger))

	vecs, err := e.Embed(context.Background(), []string{secretInput})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("vectors = %d, want 1", len(vecs))
	}
	if len(cap.records) != 1 {
		t.Fatalf("records = %d, want 1", len(cap.records))
	}
	rec := cap.records[0]
	if rec.msg != "ai_call" {
		t.Errorf("msg = %q, want ai_call", rec.msg)
	}
	if rec.attrs["model"] != DefaultModel {
		t.Errorf("model = %v, want %q", rec.attrs["model"], DefaultModel)
	}
	if rec.attrs["operation"] != "embed" {
		t.Errorf("operation = %v, want embed", rec.attrs["operation"])
	}
	if rec.attrs["prompt_version"] != PromptVersionEmbed {
		t.Errorf("prompt_version = %v, want %q", rec.attrs["prompt_version"], PromptVersionEmbed)
	}
	if rec.attrs["status"] != "success" {
		t.Errorf("status = %v, want success", rec.attrs["status"])
	}
	if _, ok := rec.attrs["latency"]; !ok {
		t.Error("missing latency attr")
	}
	// Tokens from usage block when available.
	toInt := func(v any) int64 {
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
	found := false
	for _, k := range []string{"input_tokens", "total_tokens"} {
		if v, ok := rec.attrs[k]; ok && toInt(v) == 42 {
			found = true
		}
	}
	if !found {
		t.Errorf("usage tokens not logged: %+v", rec.attrs)
	}
	// Input text must never appear in the log.
	for k, v := range rec.attrs {
		if s, ok := v.(string); ok && strings.Contains(s, secretInput) {
			t.Errorf("attr %q leaks input text", k)
		}
		if k == "input" || k == "prompt" || k == "response" {
			t.Errorf("private key %q must not be logged", k)
		}
	}
}

func TestEmbedLogsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("bad key"))
	}))
	defer ts.Close()

	cap := &aiCapture{}
	logger := slog.New(&aiCaptureHandler{c: cap})
	e, _ := NewOpenAIEmbedder("k", "", ts.URL)
	e.client = ts.Client()
	e.SetAILogger(observability.NewAILogger(logger))

	if _, err := e.Embed(context.Background(), []string{"hello"}); err == nil {
		t.Fatal("expected error")
	}
	if len(cap.records) != 1 {
		t.Fatalf("records = %d, want 1", len(cap.records))
	}
	rec := cap.records[0]
	if rec.level != slog.LevelError {
		t.Errorf("level = %v, want Error", rec.level)
	}
	if rec.attrs["status"] != "error" {
		t.Errorf("status = %v, want error", rec.attrs["status"])
	}
	if rec.attrs["error"] == "" {
		t.Error("error attr must be set on failure")
	}
}

func TestEmbedNilLoggerDoesNotPanic(t *testing.T) {
	ts := testEmbedServer(t, "")
	defer ts.Close()
	e, _ := NewOpenAIEmbedder("k", "", ts.URL)
	e.client = ts.Client()
	if _, err := e.Embed(context.Background(), []string{"x"}); err != nil {
		t.Fatalf("Embed with nil logger: %v", err)
	}
}
