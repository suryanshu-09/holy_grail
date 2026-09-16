package llm

import (
	"context"
	"fmt"
)

// Client is a minimal interface to call an LLM. Implementations may wrap
// HTTP clients to OpenAI, Anthropic, or other providers. For now it's a
// scaffold used by the extraction LLM fallback and topic classification.
type Client interface {
	// ExtractQuestionsFromText sends a prompt (text) to the model and returns
	// a JSON string the caller must validate.
	ExtractQuestionsFromText(ctx context.Context, prompt string) (string, error)
	// ClassifyTopics sends a prompt and returns a JSON string that must conform
	// to the topic classification schema (subject + topics with confidence 0-1).
	// The prompt is opaque to the client; callers build it via
	// topics.BuildClassificationPrompt for determinism. The returned string is
	// raw JSON (already stripped of markdown fences when produced by OpenAI)
	// that the caller must validate with topics.ParseClassificationResult.
	ClassifyTopics(ctx context.Context, prompt string) (string, error)
	// GenerateQuiz sends a prompt and returns a JSON string that must conform
	// to the quiz schema (questions with source_question_id, document_id,
	// question, exactly 4 options, correct_answer index, explanation).
	// The prompt is opaque to the client; callers build it via
	// quiz.BuildQuizPrompt for determinism. The returned string is
	// raw JSON (already stripped of markdown fences when produced by OpenAI)
	// that the caller must validate with quiz.ParseQuizResponse.
	GenerateQuiz(ctx context.Context, prompt string) (string, error)
}

// ErrInvalidResponse means the model returned something we couldn't parse.
var ErrInvalidResponse = fmt.Errorf("llm: invalid response")
