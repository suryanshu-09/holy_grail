package llm

import (
	"context"
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

func newAICapture() (*aiCapture, *slog.Logger) {
	c := &aiCapture{}
	h := &aiCaptureHandler{c: c}
	return c, slog.New(h)
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

func lastAIRecord(c *aiCapture) aiRecord { return c.records[len(c.records)-1] }

// AI logs must carry model, prompt version, tokens, latency and error —
// and must never contain prompt/response content.
func assertNoPII(t *testing.T, attrs map[string]any, secret string) {
	t.Helper()
	for k, v := range attrs {
		for _, banned := range []string{"prompt", "response", "content", "input", "output_text"} {
			if k == banned {
				t.Errorf("private key %q must not be logged", k)
			}
		}
		if s, ok := v.(string); ok && secret != "" && strings.Contains(s, secret) {
			t.Errorf("attr %q leaks prompt content: %q", k, s)
		}
	}
	for _, want := range []string{"model", "prompt_version", "input_tokens", "output_tokens", "latency", "error", "status"} {
		if _, ok := attrs[want]; !ok {
			t.Errorf("missing ai_call attr %q", want)
		}
	}
}

func TestExtractLogsModelLatencyTokens(t *testing.T) {
	const secret = "super-secret-prompt-marker-xyz"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"[]"}}],"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150}}`))
	}))
	defer ts.Close()

	cap, logger := newAICapture()
	c, err := NewOpenAIClient("k", "gpt-test-log", ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	c.Client = ts.Client()
	c.SetAILogger(observability.NewAILogger(logger))

	if _, err := c.ExtractQuestionsFromText(context.Background(), secret); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(cap.records) != 1 {
		t.Fatalf("records = %d, want 1", len(cap.records))
	}
	rec := lastAIRecord(cap)
	if rec.msg != "ai_call" {
		t.Errorf("msg = %q, want ai_call", rec.msg)
	}
	if rec.attrs["model"] != "gpt-test-log" {
		t.Errorf("model = %v", rec.attrs["model"])
	}
	if rec.attrs["prompt_version"] != PromptVersionExtract {
		t.Errorf("prompt_version = %v, want %q", rec.attrs["prompt_version"], PromptVersionExtract)
	}
	if rec.attrs["status"] != "success" {
		t.Errorf("status = %v, want success", rec.attrs["status"])
	}
	assertNoPII(t, rec.attrs, secret)
}

func TestExtractLogsErrorWithoutTokens(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer ts.Close()

	cap, logger := newAICapture()
	c, _ := NewOpenAIClient("k", "gpt-test-log", ts.URL)
	c.Client = ts.Client()
	c.SetAILogger(observability.NewAILogger(logger))

	if _, err := c.ExtractQuestionsFromText(context.Background(), "hello"); err == nil {
		t.Fatal("expected error")
	}
	rec := lastAIRecord(cap)
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

func TestClassifyAndQuizLogOperations(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"subject\": \"Math\", \"topics\": []}"}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`))
	}))
	defer ts.Close()

	cap, logger := newAICapture()
	c, _ := NewOpenAIClient("k", "m", ts.URL)
	c.Client = ts.Client()
	c.SetAILogger(observability.NewAILogger(logger))

	if _, err := c.ClassifyTopics(context.Background(), "classify me"); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	rec := lastAIRecord(cap)
	if rec.attrs["operation"] != "classify_topics" {
		t.Errorf("operation = %v, want classify_topics", rec.attrs["operation"])
	}
	if rec.attrs["prompt_version"] != PromptVersionClassify {
		t.Errorf("prompt_version = %v", rec.attrs["prompt_version"])
	}
	assertNoPII(t, rec.attrs, "classify me")
}

func TestVisionDescribeLogsModelAndUsage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"description\": \"A triangle.\", \"figure_type\": \"diagram\"}"}}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`))
	}))
	defer ts.Close()

	cap, logger := newAICapture()
	d, _ := NewOpenAIVisionDescriber("k", "gpt-4o-mini", ts.URL)
	d.Client = ts.Client()
	d.SetAILogger(observability.NewAILogger(logger))

	out, err := d.Describe(context.Background(), VisionDescribeInput{Name: "fig.png", Data: []byte{1, 2, 3}, Context: "secret-context-abc"})
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if out.Description != "A triangle." {
		t.Errorf("description = %q", out.Description)
	}
	rec := lastAIRecord(cap)
	if rec.msg != "ai_call" || rec.attrs["model"] != "gpt-4o-mini" {
		t.Errorf("record = %+v, want ai_call with model", rec)
	}
	if rec.attrs["operation"] != "describe_image" {
		t.Errorf("operation = %v, want describe_image", rec.attrs["operation"])
	}
	if rec.attrs["prompt_version"] != PromptVersionVision {
		t.Errorf("prompt_version = %v", rec.attrs["prompt_version"])
	}
	assertNoPII(t, rec.attrs, "secret-context-abc")
}

func TestVisionDescribeLogsError(t *testing.T) {
	cap, logger := newAICapture()
	d, _ := NewOpenAIVisionDescriber("k", "m", "http://example.com")
	d.SetAILogger(observability.NewAILogger(logger))
	if _, err := d.Describe(context.Background(), VisionDescribeInput{}); err == nil {
		t.Fatal("expected error for empty image")
	}
	rec := lastAIRecord(cap)
	if rec.level != slog.LevelError || rec.attrs["status"] != "error" {
		t.Errorf("record = %+v, want error ai_call", rec)
	}
}

func TestNilAILoggerDoesNotPanic(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"[]"}}]}`))
	}))
	defer ts.Close()
	c, _ := NewOpenAIClient("k", "m", ts.URL)
	c.Client = ts.Client()
	// AI is nil: must not panic and must still return content.
	if _, err := c.ExtractQuestionsFromText(context.Background(), "x"); err != nil {
		t.Fatalf("Extract with nil logger: %v", err)
	}
}
