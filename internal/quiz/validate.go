package quiz

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

// Strict validation limits.
const (
	// RequiredOptions is the exact number of MCQ options per quiz question.
	RequiredOptions = 4
	// MaxQuestionLen caps the quiz question stem length.
	MaxQuestionLen = 5000
	// MaxExplanationLen caps explanation length.
	MaxExplanationLen = 5000
)

// StripMarkdownFences removes ``` / ```json wrappers LLMs often emit.
func StripMarkdownFences(s string) string {
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

// ExtractJSON returns the first {...} object substring, or trimmed input.
func ExtractJSON(s string) string {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start != -1 && end != -1 && end > start {
		return strings.TrimSpace(s[start : end+1])
	}
	return s
}

// normalizeKey lowercases and trims text for dedupe comparison.
func normalizeKey(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

// ValidateQuestion checks one quiz item: required fields, options length,
// correct_answer bounds, explanation, and length caps.
func ValidateQuestion(q QuizQuestion) error {
	if strings.TrimSpace(q.SourceQuestionID) == "" {
		return fmt.Errorf("source_question_id is required")
	}
	if strings.TrimSpace(q.DocumentID) == "" {
		return fmt.Errorf("document_id is required")
	}
	if strings.TrimSpace(q.Question) == "" {
		return fmt.Errorf("question is required")
	}
	if len(q.Question) > MaxQuestionLen {
		return fmt.Errorf("question too long: %d > %d", len(q.Question), MaxQuestionLen)
	}
	if len(q.Options) != RequiredOptions {
		return fmt.Errorf("options must contain exactly %d entries, got %d", RequiredOptions, len(q.Options))
	}
	seen := make(map[string]struct{}, len(q.Options))
	for i, opt := range q.Options {
		if strings.TrimSpace(opt) == "" {
			return fmt.Errorf("options[%d] must be non-empty", i)
		}
		k := normalizeKey(opt)
		if _, dup := seen[k]; dup {
			return fmt.Errorf("options[%d] duplicates another option", i)
		}
		seen[k] = struct{}{}
	}
	if q.CorrectAnswer < 0 || q.CorrectAnswer >= len(q.Options) {
		return fmt.Errorf("correct_answer %d out of bounds [0,%d)", q.CorrectAnswer, len(q.Options))
	}
	if strings.TrimSpace(q.Explanation) == "" {
		return fmt.Errorf("explanation is required")
	}
	if len(q.Explanation) > MaxExplanationLen {
		return fmt.Errorf("explanation too long: %d > %d", len(q.Explanation), MaxExplanationLen)
	}
	return nil
}

// DedupeQuestions removes duplicate quiz items, keeping the first occurrence.
// Two items are duplicates if they share a source_question_id (case-sensitive,
// IDs are opaque) or a normalized question text. The input order is preserved.
func DedupeQuestions(qs []QuizQuestion) []QuizQuestion {
	seenSource := make(map[string]struct{}, len(qs))
	seenText := make(map[string]struct{}, len(qs))
	out := make([]QuizQuestion, 0, len(qs))
	for _, q := range qs {
		textKey := normalizeKey(q.Question)
		if _, dup := seenSource[q.SourceQuestionID]; dup {
			continue
		}
		if textKey != "" {
			if _, dup := seenText[textKey]; dup {
				continue
			}
		}
		seenSource[q.SourceQuestionID] = struct{}{}
		if textKey != "" {
			seenText[textKey] = struct{}{}
		}
		out = append(out, q)
	}
	return out
}

// findDuplicates reports duplicate source_question_id values and duplicate
// question texts for strict validation errors.
func findDuplicates(qs []QuizQuestion) error {
	seenSource := make(map[string]int, len(qs))
	seenText := make(map[string]int, len(qs))
	for i, q := range qs {
		if prev, ok := seenSource[q.SourceQuestionID]; ok {
			return fmt.Errorf("questions[%d] duplicates source_question_id of questions[%d] (%q)", i, prev, q.SourceQuestionID)
		}
		seenSource[q.SourceQuestionID] = i
		if k := normalizeKey(q.Question); k != "" {
			if prev, ok := seenText[k]; ok {
				return fmt.Errorf("questions[%d] duplicates question text of questions[%d]", i, prev)
			}
			seenText[k] = i
		}
	}
	return nil
}

// ValidateResponse checks the full response shape: non-empty list, per-item
// validity, and no duplicates. It does not check source preservation; use
// ValidateAgainstSources when the retrieved set is known.
func ValidateResponse(resp QuizResponse) error {
	if len(resp.Questions) == 0 {
		return fmt.Errorf("questions must contain at least 1 item")
	}
	if len(resp.Questions) > MaxNumQuestions {
		return fmt.Errorf("too many questions: %d > %d", len(resp.Questions), MaxNumQuestions)
	}
	for i, q := range resp.Questions {
		if err := ValidateQuestion(q); err != nil {
			return fmt.Errorf("questions[%d]: %w", i, err)
		}
	}
	if err := findDuplicates(resp.Questions); err != nil {
		return err
	}
	return nil
}

// ValidateAgainstSources additionally enforces source ID preservation: every
// source_question_id must belong to the retrieved set, and each document_id
// must equal its source question's document_id.
func ValidateAgainstSources(resp QuizResponse, sources []questions.Question) error {
	if err := ValidateResponse(resp); err != nil {
		return err
	}
	byID := make(map[string]questions.Question, len(sources))
	for _, s := range sources {
		byID[s.ID] = s
	}
	for i, q := range resp.Questions {
		src, ok := byID[q.SourceQuestionID]
		if !ok {
			return fmt.Errorf("questions[%d]: unknown source_question_id %q (not in retrieved set)", i, q.SourceQuestionID)
		}
		if q.DocumentID != src.DocumentID {
			return fmt.Errorf("questions[%d]: document_id %q does not match source document %q", i, q.DocumentID, src.DocumentID)
		}
	}
	return nil
}

// ParseQuizResponse strips fences/prose, strictly decodes the JSON envelope
// (unknown fields rejected, trailing data rejected), and validates it.
// Source preservation is checked only when sources is non-nil.
func ParseQuizResponse(raw string, sources []questions.Question) (QuizResponse, error) {
	cleaned := StripMarkdownFences(strings.TrimSpace(raw))
	if !strings.HasPrefix(cleaned, "{") {
		cleaned = ExtractJSON(cleaned)
	}
	dec := json.NewDecoder(strings.NewReader(cleaned))
	dec.DisallowUnknownFields()
	var resp QuizResponse
	if err := dec.Decode(&resp); err != nil {
		return QuizResponse{}, fmt.Errorf("invalid quiz JSON: %w", err)
	}
	if dec.More() {
		return QuizResponse{}, fmt.Errorf("invalid quiz JSON: trailing data")
	}
	if sources != nil {
		if err := ValidateAgainstSources(resp, sources); err != nil {
			return QuizResponse{}, err
		}
	} else if err := ValidateResponse(resp); err != nil {
		return QuizResponse{}, err
	}
	return resp, nil
}

// ValidateQuizJSON validates raw LLM output without returning the parsed value.
func ValidateQuizJSON(raw string, sources []questions.Question) error {
	_, err := ParseQuizResponse(raw, sources)
	return err
}
