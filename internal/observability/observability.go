// Package observability provides structured step logging and AI call logging
// per PLAN4 Phase 23 (Observability and Debugging).
//
// Every processing step logs document_id, job_id, question_id, operation,
// duration, status and error. AI calls log model, prompt version, tokens,
// latency and error — never private user data (prompts/responses).
package observability

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// Step statuses.
const (
	StatusSuccess = "success"
	StatusError   = "error"
)

// StepEntry is a single processing-step observation.
type StepEntry struct {
	DocumentID string
	JobID      string
	QuestionID string
	Operation  string
	Duration   time.Duration
	Status     string
	Err        error
	// Extra carries optional numeric context (e.g. questions=87).
	// Keys are emitted as-is alongside the required fields.
	Extra map[string]any
}

// AIEntry is a single AI/LLM call observation. It deliberately has no
// fields for prompt text, completion text, or user content so private
// data can never be logged through this type.
type AIEntry struct {
	Model         string
	PromptVersion string
	InputTokens   int
	OutputTokens  int
	Latency       time.Duration
	Err           error
	// Optional correlation with the pipeline step that made the call.
	DocumentID string
	JobID      string
	QuestionID string
	Operation  string
}

// StepLogger emits structured step logs. AILogger emits AI call logs.
// Both wrap a *slog.Logger and fall back to slog.Default() on nil.
type StepLogger struct{ log *slog.Logger }
type AILogger struct{ log *slog.Logger }

// NewStepLogger returns a StepLogger backed by logger (or slog.Default()).
func NewStepLogger(logger *slog.Logger) *StepLogger {
	if logger == nil {
		logger = slog.Default()
	}
	return &StepLogger{log: logger}
}

// NewAILogger returns an AILogger backed by logger (or slog.Default()).
func NewAILogger(logger *slog.Logger) *AILogger {
	if logger == nil {
		logger = slog.Default()
	}
	return &AILogger{log: logger}
}

// Logger exposes the underlying slog logger (useful for tests/composition).
func (l *StepLogger) Logger() *slog.Logger {
	if l == nil || l.log == nil {
		return slog.Default()
	}
	return l.log
}

// Logger exposes the underlying slog logger.
func (l *AILogger) Logger() *slog.Logger {
	if l == nil || l.log == nil {
		return slog.Default()
	}
	return l.log
}

// stepAttrs converts a StepEntry to slog attributes using the exact
// PLAN4 field names.
func (e StepEntry) attrs() []any {
	status := e.Status
	if status == "" {
		if e.Err != nil {
			status = StatusError
		} else {
			status = StatusSuccess
		}
	}
	attrs := []any{
		slog.String("document_id", e.DocumentID),
		slog.String("job_id", e.JobID),
		slog.String("question_id", e.QuestionID),
		slog.String("operation", e.Operation),
		slog.String("duration", e.Duration.String()),
		slog.Float64("duration_s", e.Duration.Seconds()),
		slog.Int64("duration_ms", e.Duration.Milliseconds()),
		slog.String("status", status),
	}
	if e.Err != nil {
		attrs = append(attrs, slog.String("error", SanitizeError(e.Err)))
	} else {
		attrs = append(attrs, slog.String("error", ""))
	}
	for k, v := range e.Extra {
		attrs = append(attrs, slog.Any(k, v))
	}
	return attrs
}

// Log emits the step entry. Success goes to Info, failures to Error.
func (l *StepLogger) Log(_ context.Context, e StepEntry) {
	logger := l.Logger()
	attrs := e.attrs()
	status := StatusSuccess
	if e.Status != "" {
		status = e.Status
	} else if e.Err != nil {
		status = StatusError
	}
	if status == StatusError {
		logger.Error("step", attrs...)
	} else {
		logger.Info("step", attrs...)
	}
}

// Start begins timing an operation. Call the returned finish function
// with the step's error (nil on success) to emit the log.
//
//	finish := logger.Start(ctx, "question_extraction", observability.StepEntry{DocumentID: "abc123"})
//	... do work ...
//	finish(nil, map[string]any{"questions": 87})
func (l *StepLogger) Start(_ context.Context, operation string, base StepEntry) func(err error, extra map[string]any) {
	start := time.Now()
	return func(err error, extra map[string]any) {
		e := base
		e.Operation = operation
		e.Duration = time.Since(start)
		e.Err = err
		if e.Status == "" {
			if err != nil {
				e.Status = StatusError
			} else {
				e.Status = StatusSuccess
			}
		}
		if e.Extra == nil {
			e.Extra = extra
		} else {
			for k, v := range extra {
				e.Extra[k] = v
			}
		}
		l.Log(context.Background(), e)
	}
}

// aiAttrs converts an AIEntry to slog attributes. No prompt/response
// content is ever emitted.
func (e AIEntry) attrs() []any {
	attrs := []any{
		slog.String("model", e.Model),
		slog.String("prompt_version", e.PromptVersion),
		slog.Int("input_tokens", e.InputTokens),
		slog.Int("output_tokens", e.OutputTokens),
		slog.Int("total_tokens", e.InputTokens+e.OutputTokens),
		slog.String("latency", e.Latency.String()),
		slog.Float64("latency_s", e.Latency.Seconds()),
		slog.Int64("latency_ms", e.Latency.Milliseconds()),
	}
	if e.Err != nil {
		attrs = append(attrs, slog.String("status", StatusError), slog.String("error", SanitizeError(e.Err)))
	} else {
		attrs = append(attrs, slog.String("status", StatusSuccess), slog.String("error", ""))
	}
	if e.DocumentID != "" {
		attrs = append(attrs, slog.String("document_id", e.DocumentID))
	}
	if e.JobID != "" {
		attrs = append(attrs, slog.String("job_id", e.JobID))
	}
	if e.QuestionID != "" {
		attrs = append(attrs, slog.String("question_id", e.QuestionID))
	}
	if e.Operation != "" {
		attrs = append(attrs, slog.String("operation", e.Operation))
	}
	return attrs
}

// Log emits the AI call observation. Failures go to Error, successes to Info.
func (l *AILogger) Log(_ context.Context, e AIEntry) {
	e.InputTokens = max(0, e.InputTokens)
	e.OutputTokens = max(0, e.OutputTokens)
	if e.Latency < 0 {
		e.Latency = 0
	}
	logger := l.Logger()
	attrs := e.attrs()
	if e.Err != nil {
		logger.Error("ai_call", attrs...)
	} else {
		logger.Info("ai_call", attrs...)
	}
}

// ObserveAI times fn and logs the AI observation. Token counts are
// supplied by the caller (from the provider response / usage block).
// fn must not pass prompt/response bodies — only metadata is logged.
func (l *AILogger) ObserveAI(_ context.Context, e AIEntry, fn func() (inputTokens, outputTokens int, err error)) error {
	start := time.Now()
	in, out, err := fn()
	e.InputTokens = in
	e.OutputTokens = out
	e.Latency = time.Since(start)
	e.Err = err
	l.Log(context.Background(), e)
	return err
}

// Correlation keys carry pipeline IDs through a context so the extraction
// pipeline, jobs runner and HTTP layer all emit the same document_id,
// job_id and question_id field names without changing function signatures.
type ctxKey string

const (
	jobIDKey      ctxKey = "observability_job_id"
	documentIDKey ctxKey = "observability_document_id"
	questionIDKey ctxKey = "observability_question_id"
)

// WithJobID returns a context carrying the job id for step logging.
func WithJobID(ctx context.Context, jobID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, jobIDKey, jobID)
}

// WithDocumentID returns a context carrying the document id for step logging.
func WithDocumentID(ctx context.Context, documentID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, documentIDKey, documentID)
}

// WithQuestionID returns a context carrying the question id for step logging.
func WithQuestionID(ctx context.Context, questionID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, questionIDKey, questionID)
}

// JobIDFromContext returns the job id carried by ctx, or "" when absent.
func JobIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(jobIDKey).(string); ok {
		return v
	}
	return ""
}

// DocumentIDFromContext returns the document id carried by ctx, or "".
func DocumentIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(documentIDKey).(string); ok {
		return v
	}
	return ""
}

// QuestionIDFromContext returns the question id carried by ctx, or "".
func QuestionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(questionIDKey).(string); ok {
		return v
	}
	return ""
}

// BaseFromContext builds a StepEntry pre-filled with the correlation IDs
// carried by ctx. Callers set Operation before logging.
func BaseFromContext(ctx context.Context) StepEntry {
	return StepEntry{
		DocumentID: DocumentIDFromContext(ctx),
		JobID:      JobIDFromContext(ctx),
		QuestionID: QuestionIDFromContext(ctx),
	}
}

// SanitizeError renders an error for logs without leaking private data.
// It redacts email-like tokens and truncates overlong messages.
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	return RedactValue(err.Error(), 500)
}

// RedactValue masks email-like substrings with "[redacted-email]",
// collapses whitespace/newlines (which often carry pasted user content),
// and truncates to maxLen runes with an ellipsis marker.
func RedactValue(s string, maxLen int) string {
	if s == "" {
		return ""
	}
	if maxLen <= 0 {
		maxLen = 500
	}
	// Collapse newlines/tabs to single spaces to avoid multi-line dumps.
	s = strings.Join(strings.Fields(s), " ")
	s = redactEmails(s)
	if r := []rune(s); len(r) > maxLen {
		s = string(r[:maxLen]) + "…[truncated]"
	}
	return s
}

// redactEmails replaces email-like tokens with a placeholder.
func redactEmails(s string) string {
	fields := strings.Fields(s)
	for i, f := range fields {
		trimmed := strings.Trim(f, "<>( ),;:.\"'")
		if isEmailLike(trimmed) {
			fields[i] = strings.Replace(f, trimmed, "[redacted-email]", 1)
		}
	}
	return strings.Join(fields, " ")
}

func isEmailLike(s string) bool {
	at := strings.IndexByte(s, '@')
	if at <= 0 || at >= len(s)-1 {
		return false
	}
	dot := strings.LastIndexByte(s, '.')
	return dot > at+1 && dot < len(s)-1
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
