package quiz

import (
	"fmt"
	"strings"
)

// QuizMode selects how quiz questions are generated from retrieved PYQs.
type QuizMode string

const (
	// ModeOriginal shows the original PYQ text directly (no rewriting).
	ModeOriginal QuizMode = "original"
	// ModeMCQ converts each descriptive PYQ into a 4-option MCQ.
	ModeMCQ QuizMode = "mcq"
	// ModeSimilar generates a new question based on (but distinct from) a PYQ.
	ModeSimilar QuizMode = "similar"
	// ModeMixed combines multiple question types across the quiz.
	ModeMixed QuizMode = "mixed"
)

// Supported quiz lengths and difficulties.
const (
	MinNumQuestions     = 1
	MaxNumQuestions     = 50
	DefaultNumQuestions = 10
)

// AllowedDifficulties is the accepted set for QuizRequest.Difficulty.
// Empty string means "any difficulty" (no filtering).
var AllowedDifficulties = map[string]struct{}{
	"easy":   {},
	"medium": {},
	"hard":   {},
}

// QuizRequest is the input for quiz generation.
// Retrieval (topic/difficulty/subject filtering + top-K) happens before the
// prompt builders run; these fields are rendered into the LLM prompt so the
// model respects the same constraints, and are validated by Validate().
type QuizRequest struct {
	// Mode selects original/mcq/similar/mixed generation. Required.
	Mode QuizMode `json:"mode"`
	// NumQuestions is the desired quiz length. 0 means DefaultNumQuestions.
	NumQuestions int `json:"num_questions"`
	// Difficulty filters by question difficulty (easy/medium/hard, "" = any).
	Difficulty string `json:"difficulty,omitempty"`
	// Topics filters by topic names (case-insensitive, AND/OR handled upstream).
	Topics []string `json:"topics,omitempty"`
	// Subject optionally constrains the quiz to one subject (e.g. "Operating Systems").
	Subject string `json:"subject,omitempty"`
	// Query is the original user request (e.g. "Quiz me on deadlocks").
	Query string `json:"query,omitempty"`
}

// QuizQuestion is a single generated quiz item with source traceability.
// SourceQuestionID must match one of the retrieved PYQ IDs; DocumentID must
// match that source question's document so the UI can link back ("View Original").
type QuizQuestion struct {
	// ID is the generated quiz question ID (server-assigned, may be empty pre-generation).
	ID string `json:"id,omitempty"`
	// SourceQuestionID is the retrieved PYQ this item was derived from. Required.
	SourceQuestionID string `json:"source_question_id"`
	// DocumentID is the source PYQ's document. Required (traceability).
	DocumentID string `json:"document_id"`
	// Question is the quiz question text (original or generated). Required.
	Question string `json:"question"`
	// Options are the MCQ choices. Exactly 4 entries required.
	Options []string `json:"options"`
	// CorrectAnswer is the 0-based index into Options. Required.
	CorrectAnswer int `json:"correct_answer"`
	// Explanation justifies the answer, grounded in the source PYQ. Required.
	Explanation string `json:"explanation"`
}

// QuizResponse is the strict JSON envelope the LLM must return and the
// service returns to callers.
type QuizResponse struct {
	Questions []QuizQuestion `json:"questions"`
}

// Validate checks the request schema: mode, length bounds, difficulty set,
// and topic/subject hygiene. It normalizes Mode/Difficulty/Topics in place.
func (r *QuizRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("quiz request is nil")
	}
	switch QuizMode(strings.ToLower(strings.TrimSpace(string(r.Mode)))) {
	case ModeOriginal, ModeMCQ, ModeSimilar, ModeMixed:
		r.Mode = QuizMode(strings.ToLower(strings.TrimSpace(string(r.Mode))))
	default:
		return fmt.Errorf("invalid mode %q: must be one of original, mcq, similar, mixed", string(r.Mode))
	}
	if r.NumQuestions == 0 {
		r.NumQuestions = DefaultNumQuestions
	}
	if r.NumQuestions < MinNumQuestions || r.NumQuestions > MaxNumQuestions {
		return fmt.Errorf("num_questions %d out of range [%d,%d]", r.NumQuestions, MinNumQuestions, MaxNumQuestions)
	}
	if d := strings.ToLower(strings.TrimSpace(r.Difficulty)); d != "" {
		if _, ok := AllowedDifficulties[d]; !ok {
			return fmt.Errorf("invalid difficulty %q: must be one of easy, medium, hard (or empty)", r.Difficulty)
		}
		r.Difficulty = d
	} else {
		r.Difficulty = ""
	}
	cleaned := make([]string, 0, len(r.Topics))
	for _, t := range r.Topics {
		if s := strings.TrimSpace(t); s != "" {
			cleaned = append(cleaned, s)
		}
	}
	r.Topics = cleaned
	r.Subject = strings.TrimSpace(r.Subject)
	r.Query = strings.TrimSpace(r.Query)
	return nil
}

// NormalizedDifficulty returns the lowercase difficulty or "" for any.
func (r QuizRequest) NormalizedDifficulty() string {
	return strings.ToLower(strings.TrimSpace(r.Difficulty))
}
