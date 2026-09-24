package quiz

import (
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

// newValidQuizQuestion returns a minimal valid quiz item for boundary tests.
func newValidQuizQuestion() QuizQuestion {
	return QuizQuestion{
		SourceQuestionID: "q1",
		DocumentID:       "d1",
		Question:         "What are the four necessary conditions for deadlock?",
		Options:          []string{"Mutual exclusion", "Hold and wait", "No preemption", "Circular wait"},
		CorrectAnswer:    0,
		Explanation:      "All four must hold simultaneously for deadlock.",
	}
}

func TestStripMarkdownFencesBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain passthrough", `{"a":1}`, `{"a":1}`},
		{"json fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"bare fence", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"uppercase fence", "```JSON\n{\"a\":1}\n```", `{"a":1}`},
		{"unclosed fence", "```json\n{\"a\":1}", `{"a":1}`},
		{"surrounding whitespace", "  {\"a\":1}  ", `{"a":1}`},
		{"non fence prose kept", "here is json {\"a\":1}", "here is json {\"a\":1}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripMarkdownFences(tc.input); got != tc.want {
				t.Fatalf("StripMarkdownFences(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestExtractJSONBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"object passthrough", `{"a":1}`, `{"a":1}`},
		{"prose wrapped", `Here you go: {"a":1} done`, `{"a":1}`},
		{"no braces returns trimmed", `just prose`, `just prose`},
		{"unbalanced returns trimmed", `{"a":1`, `{"a":1`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractJSON(tc.input); got != tc.want {
				t.Fatalf("ExtractJSON(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestValidateQuestionBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*QuizQuestion)
		wantErr bool
	}{
		{"valid", func(*QuizQuestion) {}, false},
		{"empty source id", func(q *QuizQuestion) { q.SourceQuestionID = "  " }, true},
		{"empty document id", func(q *QuizQuestion) { q.DocumentID = "" }, true},
		{"empty question", func(q *QuizQuestion) { q.Question = " " }, true},
		{"question too long", func(q *QuizQuestion) { q.Question = strings.Repeat("x", MaxQuestionLen+1) }, true},
		{"question at cap", func(q *QuizQuestion) { q.Question = strings.Repeat("x", MaxQuestionLen) }, false},
		{"too few options", func(q *QuizQuestion) { q.Options = q.Options[:3] }, true},
		{"too many options", func(q *QuizQuestion) { q.Options = append(q.Options, "Extra") }, true},
		{"empty option", func(q *QuizQuestion) { q.Options[2] = "  " }, true},
		{"duplicate options case-insensitive", func(q *QuizQuestion) { q.Options[3] = "  mutual EXCLUSION " }, true},
		{"correct answer negative", func(q *QuizQuestion) { q.CorrectAnswer = -1 }, true},
		{"correct answer overflow", func(q *QuizQuestion) { q.CorrectAnswer = RequiredOptions }, true},
		{"correct answer last valid", func(q *QuizQuestion) { q.CorrectAnswer = RequiredOptions - 1 }, false},
		{"empty explanation", func(q *QuizQuestion) { q.Explanation = "" }, true},
		{"explanation too long", func(q *QuizQuestion) { q.Explanation = strings.Repeat("y", MaxExplanationLen+1) }, true},
		{"explanation at cap", func(q *QuizQuestion) { q.Explanation = strings.Repeat("y", MaxExplanationLen) }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := newValidQuizQuestion()
			tc.mutate(&q)
			err := ValidateQuestion(q)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateQuestion succeeded, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateQuestion: %v", err)
			}
		})
	}
}

func TestValidateResponseBoundaries(t *testing.T) {
	valid := newValidQuizQuestion()

	t.Run("empty rejected", func(t *testing.T) {
		if err := ValidateResponse(QuizResponse{}); err == nil {
			t.Fatalf("empty response accepted")
		}
		if err := ValidateResponse(QuizResponse{Questions: nil}); err == nil {
			t.Fatalf("nil questions accepted")
		}
	})

	t.Run("over max rejected", func(t *testing.T) {
		qs := make([]QuizQuestion, 0, MaxNumQuestions+1)
		for i := 0; i < MaxNumQuestions+1; i++ {
			q := valid
			q.SourceQuestionID = strings.Repeat("q", 3) + string(rune('a'+i/26)) + string(rune('a'+i%26))
			q.Question = strings.Repeat("question text ", 2) + string(rune('a'+i/26)) + string(rune('a'+i%26))
			qs = append(qs, q)
		}
		if err := ValidateResponse(QuizResponse{Questions: qs}); err == nil {
			t.Fatalf("oversized response accepted")
		}
	})

	t.Run("duplicate normalized text rejected", func(t *testing.T) {
		a := valid
		b := valid
		b.SourceQuestionID = "q2"
		b.Question = "  WHAT are the four necessary conditions for deadlock? "
		if err := ValidateResponse(QuizResponse{Questions: []QuizQuestion{a, b}}); err == nil {
			t.Fatalf("duplicate text accepted")
		}
	})

	t.Run("per-item error wrapped with index", func(t *testing.T) {
		bad := valid
		bad.Explanation = ""
		err := ValidateResponse(QuizResponse{Questions: []QuizQuestion{bad}})
		if err == nil || !strings.Contains(err.Error(), "questions[0]") {
			t.Fatalf("err = %v, want questions[0] context", err)
		}
	})
}

func TestParseQuizResponseStrictness(t *testing.T) {
	srcs := testSources()

	t.Run("nil sources skips preservation check", func(t *testing.T) {
		raw := `{"questions": [{"source_question_id": "anything", "document_id": "anydoc", "question": "Q?", "options": ["A","B","C","D"], "correct_answer": 1, "explanation": "E"}]}`
		resp, err := ParseQuizResponse(raw, nil)
		if err != nil {
			t.Fatalf("ParseQuizResponse: %v", err)
		}
		if len(resp.Questions) != 1 {
			t.Fatalf("len = %d, want 1", len(resp.Questions))
		}
	})

	t.Run("trailing data rejected", func(t *testing.T) {
		raw := `{"questions": [{"source_question_id": "q1", "document_id": "d1", "question": "Q?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": "E"}]} {"stray": true}`
		if _, err := ParseQuizResponse(raw, srcs); err == nil {
			t.Fatalf("trailing data accepted")
		}
	})

	t.Run("prose prefix tolerated via extraction", func(t *testing.T) {
		raw := `Sure, here is the quiz: {"questions": [{"source_question_id": "q1", "document_id": "d1", "question": "Q?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": "E"}]}`
		if _, err := ParseQuizResponse(raw, srcs); err != nil {
			t.Fatalf("prose-prefixed JSON rejected: %v", err)
		}
	})

	t.Run("non object rejected", func(t *testing.T) {
		if _, err := ParseQuizResponse(`[1,2,3]`, srcs); err == nil {
			t.Fatalf("array input accepted")
		}
	})

	t.Run("empty questions rejected", func(t *testing.T) {
		if _, err := ParseQuizResponse(`{"questions": []}`, srcs); err == nil {
			t.Fatalf("empty questions accepted")
		}
	})

	t.Run("validate helper mirrors parse", func(t *testing.T) {
		raw := `{"questions": [{"source_question_id": "q1", "document_id": "d1", "question": "Q?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": "E"}]}`
		if err := ValidateQuizJSON(raw, srcs); err != nil {
			t.Fatalf("ValidateQuizJSON: %v", err)
		}
		if err := ValidateQuizJSON(`{bad`, srcs); err == nil {
			t.Fatalf("malformed JSON accepted")
		}
	})
}

func TestValidateAgainstSourcesEmpty(t *testing.T) {
	resp, err := ParseQuizResponse(
		`{"questions": [{"source_question_id": "q1", "document_id": "d1", "question": "Q?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": "E"}]}`,
		[]questions.Question{},
	)
	if err == nil || !strings.Contains(err.Error(), "unknown source_question_id") {
		_ = resp
		if err == nil {
			t.Fatalf("expected unknown source error against empty set")
		}
		t.Fatalf("err = %v, want unknown source_question_id", err)
	}
}

func TestDedupeQuestionsKeepsFirst(t *testing.T) {
	first := newValidQuizQuestion()
	first.Question = "First text?"
	second := newValidQuizQuestion()
	second.SourceQuestionID = "q2"
	second.Question = "  FIRST text? " // normalized duplicate of first
	third := newValidQuizQuestion()
	third.SourceQuestionID = "q3"
	third.Question = "Unique text?"

	out := DedupeQuestions([]QuizQuestion{first, second, third})
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0].SourceQuestionID != "q1" || out[1].SourceQuestionID != "q3" {
		t.Fatalf("dedupe kept wrong items: %+v", out)
	}
}
