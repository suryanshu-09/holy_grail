package topics

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TopicLabel represents a single assigned topic for a question.
// It is the unit returned by the LLM and persisted after normalization.
type TopicLabel struct {
	// Topic is the normalized topic name (e.g. "Deadlock", "Paging").
	Topic string `json:"topic"`
	// Confidence in [0,1]; higher means the classifier is more certain.
	Confidence float64 `json:"confidence"`
	// Subject is the subject/parent domain for the topic (e.g. "Operating Systems").
	// It may be empty before normalization or when subject is carried at the
	// ClassificationResult level.
	Subject string `json:"subject,omitempty"`
}

// ClassificationResult is the LLM output for one question: a subject and
// zero or more topic labels. It is kept flat and deterministic.
type ClassificationResult struct {
	// Subject is the primary subject (e.g. "Operating Systems").
	Subject string `json:"subject"`
	// Topics are the fine-grained topics with per-topic confidence.
	Topics []TopicLabel `json:"topics"`
}

// ClassificationRequest is the structured input used to build an LLM prompt.
// It is not sent as JSON to the LLM – its fields are rendered into a
// prompt string – but it defines the request schema and validation rules.
type ClassificationRequest struct {
	// QuestionID is optional passthrough so the caller can correlate results.
	QuestionID string `json:"question_id,omitempty"`
	// QuestionText is the raw question text to classify (required).
	QuestionText string `json:"question_text"`
	// SubjectHint is an optional hint (e.g. document subject or syllabus name)
	// that helps the model stay consistent.
	SubjectHint string `json:"subject_hint,omitempty"`
}

// ClassificationResponse is the strict JSON envelope the LLM must return.
// It mirrors ClassificationResult but exists so callers can distinguish
// wire format from internal representation if they diverge.
type ClassificationResponse struct {
	Subject string       `json:"subject"`
	Topics  []TopicLabel `json:"topics"`
}

// Validate checks a TopicLabel for required fields and confidence range.
func (t TopicLabel) Validate() error {
	if strings.TrimSpace(t.Topic) == "" {
		return fmt.Errorf("topic name is required")
	}
	if t.Confidence < 0 || t.Confidence > 1 {
		return fmt.Errorf("confidence %v out of range [0,1]", t.Confidence)
	}
	// Subject is optional; if present it must be non-whitespace.
	if t.Subject != "" && strings.TrimSpace(t.Subject) == "" {
		return fmt.Errorf("subject must not be whitespace")
	}
	return nil
}

// Validate checks a ClassificationResult for required fields and delegates
// to each TopicLabel.
func (r ClassificationResult) Validate() error {
	if strings.TrimSpace(r.Subject) == "" {
		return fmt.Errorf("subject is required")
	}
	if r.Topics == nil {
		return fmt.Errorf("topics is required (use empty array if none)")
	}
	if len(r.Topics) > 10 {
		return fmt.Errorf("too many topics: %d > 10", len(r.Topics))
	}
	for i, t := range r.Topics {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("topics[%d]: %w", i, err)
		}
	}
	return nil
}

// Validate checks the request schema.
func (r ClassificationRequest) Validate() error {
	if strings.TrimSpace(r.QuestionText) == "" {
		return fmt.Errorf("question_text is required")
	}
	if len(r.QuestionText) > 20000 {
		return fmt.Errorf("question_text too long: %d > 20000", len(r.QuestionText))
	}
	return nil
}

// ToResponse converts a ClassificationResult into the wire Response type.
// This keeps the internal and wire representations aligned.
func (r ClassificationResult) ToResponse() ClassificationResponse {
	return ClassificationResponse{
		Subject: r.Subject,
		Topics:  r.Topics,
	}
}

// FromResponse converts a wire response back to internal result.
func FromResponse(resp ClassificationResponse) ClassificationResult {
	return ClassificationResult{
		Subject: resp.Subject,
		Topics:  resp.Topics,
	}
}

// MarshalResult validates and marshals a result to canonical JSON.
func MarshalResult(r ClassificationResult) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("marshal classification result: %w", err)
	}
	return string(b), nil
}

// StripMarkdownFences removes ```json / ``` wrappers that LLMs often emit.
// It is deterministic and handles common variants (```, ```json, ```JSON).
func StripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Remove opening fence line. Handle ```json, ```JSON, ``` json etc.
	// Find first newline after opening fence; if none, strip prefix only.
	if idx := strings.Index(s, "\n"); idx != -1 {
		opening := strings.TrimSpace(s[:idx])
		_ = opening // opening is like ```json or ```
		s = s[idx+1:]
	} else {
		// No newline – just strip the fence markers
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```JSON")
		s = strings.TrimPrefix(s, "```")
	}
	// Remove closing fence (and any trailing content after it)
	if idx := strings.LastIndex(s, "```"); idx != -1 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

// ExtractJSON extracts the first JSON object substring from s by locating
// the outermost { ... } pair. If no braces are found the trimmed input is
// returned. This is used for strict extraction after fence stripping.
func ExtractJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start != -1 && end != -1 && end > start {
		return strings.TrimSpace(s[start : end+1])
	}
	return s
}

// ParseClassificationResult strips markdown fences, extracts the JSON object,
// unmarshals strictly, validates, and returns the result.
func ParseClassificationResult(raw string) (ClassificationResult, error) {
	cleaned := StripMarkdownFences(raw)
	cleaned = strings.TrimSpace(cleaned)
	// If the LLM wrapped JSON in extra prose, try to extract the object.
	if !strings.HasPrefix(cleaned, "{") {
		cleaned = ExtractJSON(cleaned)
	}
	// Strict decoding: disallow unknown fields.
	dec := json.NewDecoder(strings.NewReader(cleaned))
	dec.DisallowUnknownFields()
	var resp ClassificationResponse
	if err := dec.Decode(&resp); err != nil {
		return ClassificationResult{}, fmt.Errorf("invalid classification JSON: %w", err)
	}
	// Ensure no trailing non-whitespace after the single JSON object.
	// Decoder will have consumed one value; check for extra content.
	if dec.More() {
		return ClassificationResult{}, fmt.Errorf("invalid classification JSON: trailing data")
	}
	result := FromResponse(resp)
	if err := result.Validate(); err != nil {
		return ClassificationResult{}, err
	}
	return result, nil
}

// ValidateClassificationJSON validates that raw (possibly fence-wrapped) JSON
// conforms to the ClassificationResponse schema.
func ValidateClassificationJSON(raw string) error {
	_, err := ParseClassificationResult(raw)
	return err
}

// BuildClassificationPrompt creates a deterministic prompt for topic
// classification. It instructs the model to return strict JSON matching
// ClassificationResponse and lists the required fields.
func BuildClassificationPrompt(req ClassificationRequest) string {
	var b strings.Builder
	b.WriteString("You are a topic classifier for Previous Year Questions.\n")
	b.WriteString("Classify the following question into a subject and fine-grained topics.\n")
	if strings.TrimSpace(req.SubjectHint) != "" {
		b.WriteString(fmt.Sprintf("Subject hint: %s\n", strings.TrimSpace(req.SubjectHint)))
	}
	b.WriteString("\nQuestion:\n")
	b.WriteString(strings.TrimSpace(req.QuestionText))
	b.WriteString("\n\nRespond ONLY with JSON of the form:\n")
	b.WriteString(`{"subject": "string", "topics": [{"topic": "string", "confidence": 0.0-1.0, "subject": "string (optional)"}]}` + "\n")
	b.WriteString("Rules:\n")
	b.WriteString("- subject is required, non-empty.\n")
	b.WriteString("- topics is an array (use [] if none), max 10 entries.\n")
	b.WriteString("- each topic: topic name required, confidence 0-1, subject optional.\n")
	b.WriteString("- Return ONLY the JSON object, no markdown fences, no extra text.\n")
	return b.String()
}

// BuildSimplePrompt is a convenience helper that takes raw question text and
// an optional subject hint and returns a deterministic prompt string.
// It keeps the LLM Client interface simple (prompt string -> JSON string)
// while centralizing prompt construction here for consistency.
func BuildSimplePrompt(questionText, subjectHint string) string {
	return BuildClassificationPrompt(ClassificationRequest{
		QuestionText: questionText,
		SubjectHint:  subjectHint,
	})
}
