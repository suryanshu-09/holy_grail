package extraction

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/llm"
)

// LLMFallback provides a small wrapper that can be used to call an LLM when
// deterministic parsing is uncertain. It is intentionally provider-agnostic.
// The fallback accepts a client implementing internal/llm.Client.

type LLMFallback struct {
	Client llm.Client
	// MaxPages is the maximum number of pages to include in one LLM call to
	// keep prompts compact. Defaults to 3 when zero.
	MaxPages int
	// MinInterval enforces rate limiting between calls; zero disables it.
	MinInterval time.Duration
	// lastCall tracks last successful call for rate limiting.
	mu         sync.Mutex
	lastCall   time.Time
	lastPrompt string
	lastHash   string
}

// BuildPrompt builds a compact prompt for the model containing the document
// id, page ranges and the normalized text. It instructs the model to return
// strict JSON matching the PreviewQuestion array schema.
func (f *LLMFallback) BuildPrompt(documentID string, pages []Page) string {
	if f.MaxPages == 0 {
		f.MaxPages = 3
	}
	// enforce MaxPages limit already here so callers get deterministic truncation
	if f.MaxPages > 0 && len(pages) > f.MaxPages {
		pages = pages[:f.MaxPages]
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Document ID: %s\n", documentID))
	b.WriteString("Extracted pages (keep only relevant text).\n")
	for _, p := range pages {
		b.WriteString(fmt.Sprintf("--- Page %d ---\n", p.Number))
		// keep text brief: truncate very long pages
		text := p.Text
		if len(text) > 5000 {
			text = text[:5000]
		}
		b.WriteString(text)
		b.WriteString("\n")
	}
	b.WriteString("\nRespond ONLY with JSON of the form:\n[ { \"number\": optional string, \"text\": string, \"start_page\": int, \"end_page\": int, \"options\": [strings], \"type\": string, \"confidence\": float }, ... ]\n")
	b.WriteString("Do not include any extraneous text. If you cannot identify questions, return an empty array: []\n")
	return b.String()
}

// Wait enforces MinInterval rate limiting. It sleeps if needed.
func (f *LLMFallback) Wait(ctx context.Context) error {
	if f.MinInterval <= 0 {
		return nil
	}
	f.mu.Lock()
	elapsed := time.Since(f.lastCall)
	f.mu.Unlock()
	if elapsed < f.MinInterval {
		wait := f.MinInterval - elapsed
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// LastPromptHash returns the sha256 hash of the last prompt sent.
func (f *LLMFallback) LastPromptHash() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastHash
}

// Call runs the LLM on the provided pages and returns parsed PreviewQuestions.
// It validates returned JSON strictly and records prompt hash for reproducibility.
func (f *LLMFallback) Call(ctx context.Context, documentID string, pages []Page) ([]PreviewQuestion, error) {
	if f.Client == nil {
		return nil, fmt.Errorf("llm fallback: no client configured")
	}
	prompt := f.BuildPrompt(documentID, pages)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(prompt)))[:16]
	f.mu.Lock()
	f.lastPrompt = prompt
	f.lastHash = hash
	f.lastCall = time.Now()
	f.mu.Unlock()

	resp, err := f.Client.ExtractQuestionsFromText(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm fallback: client error: %w", err)
	}
	resp = strings.TrimSpace(resp)
	// handle common LLM wrapper: ```json ... ```
	if strings.HasPrefix(resp, "```") {
		// strip markdown fences
		resp = strings.TrimPrefix(resp, "```json")
		resp = strings.TrimPrefix(resp, "```")
		resp = strings.TrimSuffix(resp, "```")
		resp = strings.TrimSpace(resp)
	}
	// Some models return {"questions": [...]} envelope; handle both forms.
	if strings.HasPrefix(resp, "{") {
		var envelope struct {
			Questions []PreviewQuestion `json:"questions"`
		}
		if err := json.Unmarshal([]byte(resp), &envelope); err == nil && envelope.Questions != nil {
			respBytes, _ := json.Marshal(envelope.Questions)
			resp = string(respBytes)
		}
	}
	// strict JSON expected: parse into []PreviewQuestion
	var out []PreviewQuestion
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		return nil, fmt.Errorf("llm fallback: parse: %w", err)
	}
	// validate each question: must have text and page numbers; confidence in [0,1]; type known
	for i, q := range out {
		if strings.TrimSpace(q.Text) == "" {
			return nil, fmt.Errorf("llm fallback: question %d has empty text", i)
		}
		if q.StartPage < 1 || q.EndPage < 1 {
			return nil, fmt.Errorf("llm fallback: question %d has invalid pages %d-%d", i, q.StartPage, q.EndPage)
		}
		if q.Confidence < 0 || q.Confidence > 1 {
			// clamp rather than fail, but record note
			if q.Confidence < 0 {
				out[i].Confidence = 0
			} else if q.Confidence > 1 {
				out[i].Confidence = 1
			}
		}
		if q.Type != "" {
			switch q.Type {
			case "MCQ", "MSQ", "numerical", "descriptive", "true_false", "unknown":
			default:
				// normalize unknown types to unknown
				out[i].Type = "unknown"
			}
		} else {
			out[i].Type = "unknown"
		}
		// ensure offsets within reasonable range; leave as is
	}
	return out, nil
}
