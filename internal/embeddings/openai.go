package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/observability"
)

// PromptVersionEmbed is emitted in embedding AI call logs (PLAN4 Phase 23).
// Embeddings have no prompt template; the version marks the request schema.
// Input text is never logged.
const PromptVersionEmbed = "embed-v1"

// OpenAIEmbedder is a small OpenAI-compatible embeddings client.
//
// AI is an optional AI call logger (nil disables logging). When set, every
// Embed call logs model, prompt version, token usage (when the provider
// returns a usage block), latency and error. Input texts are never logged.
type OpenAIEmbedder struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
	// AI, when non-nil, receives one ai_call log per Embed call.
	AI *observability.AILogger
}

// SetAILogger attaches an AI call logger (nil disables logging).
func (e *OpenAIEmbedder) SetAILogger(l *observability.AILogger) { e.AI = l }

// NewOpenAIEmbedder creates an embedder pinned to one model. The schema uses
// 1536-dimensional pgvector columns, so only the compatible default model is
// supported until a deliberate schema migration is made.
func NewOpenAIEmbedder(apiKey, model, baseURL string) (*OpenAIEmbedder, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("embeddings: api key required")
	}
	if model == "" {
		model = DefaultModel
	}
	if model != DefaultModel {
		return nil, fmt.Errorf("embeddings: model %q is incompatible with vector(%d); migrate and re-index before changing models", model, DefaultDimensions)
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &OpenAIEmbedder{
		apiKey:  apiKey,
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (e *OpenAIEmbedder) Model() string { return e.model }

func (e *OpenAIEmbedder) Dimensions() int { return DefaultDimensions }

func (e *OpenAIEmbedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	start := time.Now()
	fail := func(err error, inTok int) ([][]float32, error) {
		if e.AI != nil {
			e.AI.Log(ctx, observability.AIEntry{
				Model:         e.model,
				PromptVersion: PromptVersionEmbed,
				InputTokens:   inTok,
				Latency:       time.Since(start),
				Operation:     "embed",
				Err:           err,
			})
		}
		return nil, err
	}
	if len(inputs) == 0 {
		if e.AI != nil {
			e.AI.Log(ctx, observability.AIEntry{
				Model:         e.model,
				PromptVersion: PromptVersionEmbed,
				Latency:       time.Since(start),
				Operation:     "embed",
			})
		}
		return [][]float32{}, nil
	}
	body, err := json.Marshal(struct {
		Input []string `json:"input"`
		Model string   `json:"model"`
	}{Input: inputs, Model: e.model})
	if err != nil {
		return fail(fmt.Errorf("embeddings: marshal request: %w", err), 0)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return fail(fmt.Errorf("embeddings: create request: %w", err), 0)
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	response, err := e.client.Do(req)
	if err != nil {
		return fail(fmt.Errorf("embeddings: request: %w", err), 0)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		return fail(fmt.Errorf("embeddings: status %d: %s", response.StatusCode, strings.TrimSpace(string(message))), 0)
	}

	var payload struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Usage *struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage,omitempty"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return fail(fmt.Errorf("embeddings: decode response: %w", err), 0)
	}
	inTok := 0
	if payload.Usage != nil {
		inTok = payload.Usage.PromptTokens
		if inTok == 0 {
			inTok = payload.Usage.TotalTokens
		}
	}
	if len(payload.Data) != len(inputs) {
		return fail(fmt.Errorf("embeddings: response count %d, want %d", len(payload.Data), len(inputs)), inTok)
	}
	sort.Slice(payload.Data, func(i, j int) bool { return payload.Data[i].Index < payload.Data[j].Index })
	vectors := make([][]float32, len(inputs))
	for i, item := range payload.Data {
		if item.Index != i {
			return fail(fmt.Errorf("embeddings: response missing index %d", i), inTok)
		}
		if len(item.Embedding) != e.Dimensions() {
			return fail(fmt.Errorf("embeddings: response vector dimension %d, want %d", len(item.Embedding), e.Dimensions()), inTok)
		}
		vectors[i] = item.Embedding
	}
	if e.AI != nil {
		e.AI.Log(ctx, observability.AIEntry{
			Model:         e.model,
			PromptVersion: PromptVersionEmbed,
			InputTokens:   inTok,
			Latency:       time.Since(start),
			Operation:     "embed",
		})
	}
	return vectors, nil
}
