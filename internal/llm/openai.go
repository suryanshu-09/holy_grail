package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// ClassifyTopics sends a chat completion request for topic classification.
// It is deterministic (temperature 0) and validates the response strictly:
//   - strips markdown code fences (```json ... ```)
//   - extracts the JSON object if wrapped in prose
//   - validates required fields: subject non-empty, topics array, each topic name
//     non-empty, confidence in [0,1], max 10 topics, no unknown fields.
// On any validation failure it returns ErrInvalidResponse.
func (c *OpenAIClient) ClassifyTopics(ctx context.Context, prompt string) (string, error) {
	reqBody := chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: "You are a precise topic classifier. Return ONLY valid JSON matching {\"subject\": string, \"topics\": [{\"topic\": string, \"confidence\": 0.0-1.0, \"subject\": string}]} with no markdown, no extra text."},
			{Role: "user", Content: prompt},
		},
		MaxTok: 800,
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
	content := out.Choices[0].Message.Content

	// Strict JSON extraction: markdown fence stripping + JSON object extraction.
	cleaned := stripClassifyFences(content)
	cleaned = strings.TrimSpace(cleaned)
	if !strings.HasPrefix(cleaned, "{") {
		cleaned = extractClassifyJSON(cleaned)
	}
	// Validate strictly before returning.
	if err := validateClassificationJSON(cleaned); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	// Return canonical cleaned JSON (already validated).
	return cleaned, nil
}

// GenerateQuiz sends a chat completion request for quiz generation.
// It is deterministic (temperature 0) and validates the response strictly:
//   - strips markdown code fences (```json ... ```)
//   - extracts the JSON object if wrapped in prose
//   - validates required fields: questions array (1-50 items), each item with
//     source_question_id, document_id, question, exactly 4 distinct non-empty
//     options, correct_answer in bounds, explanation, no unknown fields,
//     no duplicates.
// On any validation failure it returns ErrInvalidResponse.
func (c *OpenAIClient) GenerateQuiz(ctx context.Context, prompt string) (string, error) {
	reqBody := chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: "You are a precise quiz generator. Return ONLY valid JSON matching {\"questions\": [{\"id\": string, \"source_question_id\": string, \"document_id\": string, \"question\": string, \"options\": [string x4], \"correct_answer\": 0-3, \"explanation\": string}]} with no markdown, no extra text."},
			{Role: "user", Content: prompt},
		},
		MaxTok: 2000,
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
	content := out.Choices[0].Message.Content

	// Strict JSON extraction: markdown fence stripping + JSON object extraction.
	cleaned := stripQuizFences(content)
	cleaned = strings.TrimSpace(cleaned)
	if !strings.HasPrefix(cleaned, "{") {
		cleaned = extractQuizJSON(cleaned)
	}
	// Validate strictly before returning.
	if err := validateQuizJSON(cleaned); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	// Return canonical cleaned JSON (already validated).
	return cleaned, nil
}

// stripQuizFences removes ```json / ``` wrappers.
func stripQuizFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if idx := strings.Index(s, "\n"); idx != -1 {
		s = s[idx+1:]
	} else {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```JSON")
		s = strings.TrimPrefix(s, "```")
	}
	if idx := strings.LastIndex(s, "```"); idx != -1 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

// extractQuizJSON finds outermost { ... } when JSON is wrapped in prose.
func extractQuizJSON(s string) string {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start != -1 && end != -1 && end > start {
		return strings.TrimSpace(s[start : end+1])
	}
	return s
}

// normalizeQuizKey lowercases and collapses whitespace for dedupe comparison.
func normalizeQuizKey(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

// validateQuizJSON strictly validates the quiz envelope without source
// preservation (the client sees only an opaque prompt). Source ID membership
// is checked later by the quiz service via ValidateAgainstSources.
func validateQuizJSON(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("empty response")
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	var resp struct {
		Questions []struct {
			ID               string   `json:"id,omitempty"`
			SourceQuestionID string   `json:"source_question_id"`
			DocumentID       string   `json:"document_id"`
			Question         string   `json:"question"`
			Options          []string `json:"options"`
			CorrectAnswer    int      `json:"correct_answer"`
			Explanation      string   `json:"explanation"`
		} `json:"questions"`
	}
	if err := dec.Decode(&resp); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if dec.More() {
		return fmt.Errorf("trailing data after JSON object")
	}
	if len(resp.Questions) == 0 {
		return fmt.Errorf("questions must contain at least 1 item")
	}
	if len(resp.Questions) > 50 {
		return fmt.Errorf("too many questions: %d > 50", len(resp.Questions))
	}
	seenSource := make(map[string]int, len(resp.Questions))
	seenText := make(map[string]int, len(resp.Questions))
	for i, q := range resp.Questions {
		if strings.TrimSpace(q.SourceQuestionID) == "" {
			return fmt.Errorf("questions[%d]: source_question_id is required", i)
		}
		if strings.TrimSpace(q.DocumentID) == "" {
			return fmt.Errorf("questions[%d]: document_id is required", i)
		}
		if strings.TrimSpace(q.Question) == "" {
			return fmt.Errorf("questions[%d]: question is required", i)
		}
		if len(q.Question) > 5000 {
			return fmt.Errorf("questions[%d]: question too long: %d > 5000", i, len(q.Question))
		}
		if len(q.Options) != 4 {
			return fmt.Errorf("questions[%d]: options must contain exactly 4 entries, got %d", i, len(q.Options))
		}
		seenOpts := make(map[string]struct{}, len(q.Options))
		for j, opt := range q.Options {
			if strings.TrimSpace(opt) == "" {
				return fmt.Errorf("questions[%d]: options[%d] must be non-empty", i, j)
			}
			k := normalizeQuizKey(opt)
			if _, dup := seenOpts[k]; dup {
				return fmt.Errorf("questions[%d]: options[%d] duplicates another option", i, j)
			}
			seenOpts[k] = struct{}{}
		}
		if q.CorrectAnswer < 0 || q.CorrectAnswer >= len(q.Options) {
			return fmt.Errorf("questions[%d]: correct_answer %d out of bounds [0,%d)", i, q.CorrectAnswer, len(q.Options))
		}
		if strings.TrimSpace(q.Explanation) == "" {
			return fmt.Errorf("questions[%d]: explanation is required", i)
		}
		if len(q.Explanation) > 5000 {
			return fmt.Errorf("questions[%d]: explanation too long: %d > 5000", i, len(q.Explanation))
		}
		if prev, ok := seenSource[q.SourceQuestionID]; ok {
			return fmt.Errorf("questions[%d] duplicates source_question_id of questions[%d] (%q)", i, prev, q.SourceQuestionID)
		}
		seenSource[q.SourceQuestionID] = i
		if k := normalizeQuizKey(q.Question); k != "" {
			if prev, ok := seenText[k]; ok {
				return fmt.Errorf("questions[%d] duplicates question text of questions[%d]", i, prev)
			}
			seenText[k] = i
		}
	}
	return nil
}
// stripClassifyFences removes ```json / ``` wrappers.
func stripClassifyFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if idx := strings.Index(s, "\n"); idx != -1 {
		s = s[idx+1:]
	} else {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```JSON")
		s = strings.TrimPrefix(s, "```")
	}
	if idx := strings.LastIndex(s, "```"); idx != -1 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

// extractClassifyJSON finds outermost { ... } when JSON is wrapped in prose.
func extractClassifyJSON(s string) string {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start != -1 && end != -1 && end > start {
		return strings.TrimSpace(s[start : end+1])
	}
	return s
}

// validateClassificationJSON strictly validates the classification envelope.
func validateClassificationJSON(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("empty response")
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	var resp struct {
		Subject string `json:"subject"`
		Topics  []struct {
			Topic      string  `json:"topic"`
			Confidence float64 `json:"confidence"`
			Subject    string  `json:"subject,omitempty"`
		} `json:"topics"`
	}
	if err := dec.Decode(&resp); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if dec.More() {
		return fmt.Errorf("trailing data after JSON object")
	}
	if strings.TrimSpace(resp.Subject) == "" {
		return fmt.Errorf("subject is required")
	}
	if resp.Topics == nil {
		return fmt.Errorf("topics is required")
	}
	if len(resp.Topics) > 10 {
		return fmt.Errorf("too many topics: %d > 10", len(resp.Topics))
	}
	for i, t := range resp.Topics {
		if strings.TrimSpace(t.Topic) == "" {
			return fmt.Errorf("topics[%d]: topic name is required", i)
		}
		if t.Confidence < 0 || t.Confidence > 1 {
			return fmt.Errorf("topics[%d]: confidence %v out of range [0,1]", i, t.Confidence)
		}
	}
	return nil
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
