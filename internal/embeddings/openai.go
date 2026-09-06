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
)

// OpenAIEmbedder is a small OpenAI-compatible embeddings client.
type OpenAIEmbedder struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

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
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}
	body, err := json.Marshal(struct {
		Input []string `json:"input"`
		Model string   `json:"model"`
	}{Input: inputs, Model: e.model})
	if err != nil {
		return nil, fmt.Errorf("embeddings: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embeddings: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	response, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings: request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		return nil, fmt.Errorf("embeddings: status %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}

	var payload struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("embeddings: decode response: %w", err)
	}
	if len(payload.Data) != len(inputs) {
		return nil, fmt.Errorf("embeddings: response count %d, want %d", len(payload.Data), len(inputs))
	}
	sort.Slice(payload.Data, func(i, j int) bool { return payload.Data[i].Index < payload.Data[j].Index })
	vectors := make([][]float32, len(inputs))
	for i, item := range payload.Data {
		if item.Index != i {
			return nil, fmt.Errorf("embeddings: response missing index %d", i)
		}
		if len(item.Embedding) != e.Dimensions() {
			return nil, fmt.Errorf("embeddings: response vector dimension %d, want %d", len(item.Embedding), e.Dimensions())
		}
		vectors[i] = item.Embedding
	}
	return vectors, nil
}
