package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIClient is a minimal OpenAI-compatible client implementing Client.
// It is intentionally small: only the fields needed for ExtractQuestionsFromText
// are implemented. The caller is responsible for providing a valid apiKey.
type OpenAIClient struct {
	APIKey  string
	Model   string
	BaseURL string
	Client  *http.Client
}

// NewOpenAIClient constructs an OpenAIClient. If baseURL is empty the default
// https://api.openai.com is used. If model is empty the default "gpt-3.5-turbo"
// is used. apiKey must be provided.
func NewOpenAIClient(apiKey, model, baseURL string) (*OpenAIClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("openai: api key required")
	}
	if model == "" {
		model = "gpt-3.5-turbo"
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &OpenAIClient{APIKey: apiKey, Model: model, BaseURL: baseURL, Client: &http.Client{Timeout: 15 * time.Second}}, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	MaxTok   int           `json:"max_tokens,omitempty"`
	Temp     float32       `json:"temperature,omitempty"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	FinishReason string      `json:"finish_reason"`
	Message      chatMessage `json:"message"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Choices []chatChoice `json:"choices"`
}

// ExtractQuestionsFromText sends a chat request with the prompt and returns
// the model's text content (choices[0].message.content) as-is.
func (c *OpenAIClient) ExtractQuestionsFromText(ctx context.Context, prompt string) (string, error) {
	reqBody := chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: "You are a helpful JSON extractor. Return only the JSON array requested."},
			{Role: "user", Content: prompt},
		},
		MaxTok: 1000,
		Temp:   0.0,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("openai: marshal request: %w", err)
	}

	url := c.BaseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, io.NopCloser(io.MultiReader(bytesNewReader(raw))))
	if err != nil {
		return "", fmt.Errorf("openai: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai: status %d: %s", resp.StatusCode, string(b))
	}
	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("openai: decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai: no choices in response")
	}
	return out.Choices[0].Message.Content, nil
}

// bytesNewReader wraps bytes.NewReader without importing bytes in many files.
func bytesNewReader(b []byte) *byteReader { return &byteReader{b: b, i: 0} }

type byteReader struct {
	b []byte
	i int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func (r *byteReader) Close() error { return nil }
