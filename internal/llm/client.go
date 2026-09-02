package llm

import (
	"context"
	"fmt"
)

// Client is a minimal interface to call an LLM. Implementations may wrap
// HTTP clients to OpenAI, Anthropic, or other providers. For now it's a
// scaffold used by the extraction LLM fallback.
type Client interface {
	// ExtractQuestionsFromText sends a prompt (text) to the model and returns
	// a JSON string the caller must validate.
	ExtractQuestionsFromText(ctx context.Context, prompt string) (string, error)
}

// ErrInvalidResponse means the model returned something we couldn't parse.
var ErrInvalidResponse = fmt.Errorf("llm: invalid response")
