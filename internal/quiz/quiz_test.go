package quiz

import (
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

func testSources() []questions.Question {
	qt := "Explain the four necessary conditions for deadlock."
	sub := "Operating Systems"
	diff := "medium"
	qtype := "descriptive"
	return []questions.Question{
		{ID: "q1", DocumentID: "d1", QuestionText: &qt, Subject: &sub, Difficulty: &diff, QuestionType: &qtype},
		{ID: "q2", DocumentID: "d1", QuestionText: strPtr("What is paging in OS?"), Subject: &sub},
	}
}

func strPtr(s string) *string { return &s }

func TestRequestValidateModes(t *testing.T) {
	for _, m := range []QuizMode{ModeOriginal, ModeMCQ, ModeSimilar, ModeMixed} {
		r := QuizRequest{Mode: m}
		if err := r.Validate(); err != nil {
			t.Fatalf("mode %q should validate: %v", m, err)
		}
		if r.NumQuestions != DefaultNumQuestions {
			t.Fatalf("default length not applied: %d", r.NumQuestions)
		}
	}
	bad := QuizRequest{Mode: "invent"}
	if err := bad.Validate(); err == nil {
		t.Fatal("invalid mode should fail")
	}
	bad2 := QuizRequest{Mode: ModeMCQ, NumQuestions: 100}
	if err := bad2.Validate(); err == nil {
		t.Fatal("overlong quiz should fail")
	}
	bad3 := QuizRequest{Mode: ModeMCQ, NumQuestions: 5, Difficulty: "expert"}
	if err := bad3.Validate(); err == nil {
		t.Fatal("bad difficulty should fail")
	}
}

func TestPromptBuildersGrounded(t *testing.T) {
	srcs := testSources()
	modes := map[QuizMode]func(QuizRequest, []questions.Question) string{
		ModeOriginal: BuildOriginalPrompt,
		ModeMCQ:      BuildMCQPrompt,
		ModeSimilar:  BuildSimilarPrompt,
		ModeMixed:    BuildMixedPrompt,
	}
	req := QuizRequest{Mode: ModeMCQ, NumQuestions: 2, Difficulty: "medium", Topics: []string{"Deadlock"}, Subject: "Operating Systems", Query: "Quiz me on deadlocks"}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	for m, fn := range modes {
		p := fn(req, srcs)
		if !strings.Contains(p, "ONLY source of truth") && !strings.Contains(p, "source of truth") {
			t.Fatalf("mode %s prompt not grounded", m)
		}
		if !strings.Contains(p, "q1") || !strings.Contains(p, "d1") {
			t.Fatalf("mode %s prompt missing source IDs", m)
		}
		if !strings.Contains(p, "EXACTLY 2") {
			t.Fatalf("mode %s prompt missing length constraint", m)
		}
		if !strings.Contains(p, "medium") || !strings.Contains(p, "Deadlock") || !strings.Contains(p, "Operating Systems") {
			t.Fatalf("mode %s prompt missing difficulty/topic/subject filter", m)
		}
	}
	if _, err := BuildQuizPrompt(QuizRequest{Mode: ModeMCQ, NumQuestions: 2}, nil); err == nil {
		t.Fatal("empty sources should fail")
	}
}

func TestParseAndValidate(t *testing.T) {
	srcs := testSources()
	raw := `{"questions": [{"source_question_id": "q1", "document_id": "d1", "question": "What are the four necessary conditions for deadlock?", "options": ["A", "B", "C", "D"], "correct_answer": 2, "explanation": "Because ..."}]}`
	resp, err := ParseQuizResponse(raw, srcs)
	if err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
	if len(resp.Questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(resp.Questions))
	}
	// malformed JSON
	if _, err := ParseQuizResponse(`{bad`, srcs); err == nil {
		t.Fatal("malformed JSON should fail")
	}
	// correct_answer out of bounds
	badBounds := `{"questions": [{"source_question_id": "q1", "document_id": "d1", "question": "Q?", "options": ["A","B","C","D"], "correct_answer": 4, "explanation": "E"}]}`
	if _, err := ParseQuizResponse(badBounds, srcs); err == nil {
		t.Fatal("out-of-bounds correct_answer should fail")
	}
	// wrong options length
	badOpts := `{"questions": [{"source_question_id": "q1", "document_id": "d1", "question": "Q?", "options": ["A","B"], "correct_answer": 0, "explanation": "E"}]}`
	if _, err := ParseQuizResponse(badOpts, srcs); err == nil {
		t.Fatal("wrong options length should fail")
	}
	// missing explanation
	noExpl := `{"questions": [{"source_question_id": "q1", "document_id": "d1", "question": "Q?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": ""}]}`
	if _, err := ParseQuizResponse(noExpl, srcs); err == nil {
		t.Fatal("empty explanation should fail")
	}
	// unknown source id
	unknown := `{"questions": [{"source_question_id": "nope", "document_id": "d1", "question": "Q?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": "E"}]}`
	if _, err := ParseQuizResponse(unknown, srcs); err == nil {
		t.Fatal("unknown source id should fail")
	}
	// document mismatch
	mismatch := `{"questions": [{"source_question_id": "q1", "document_id": "WRONG", "question": "Q?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": "E"}]}`
	if _, err := ParseQuizResponse(mismatch, srcs); err == nil {
		t.Fatal("document mismatch should fail")
	}
	// duplicates rejected
	dup := `{"questions": [
	  {"source_question_id": "q1", "document_id": "d1", "question": "Same?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": "E"},
	  {"source_question_id": "q1", "document_id": "d1", "question": "Different?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": "E"}]}`
	if _, err := ParseQuizResponse(dup, srcs); err == nil {
		t.Fatal("duplicate source ids should fail")
	}
	// unknown field rejected
	extra := `{"questions": [{"source_question_id": "q1", "document_id": "d1", "question": "Q?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": "E", "bogus": 1}]}`
	if _, err := ParseQuizResponse(extra, srcs); err == nil {
		t.Fatal("unknown field should fail")
	}
	// fences tolerated
	fenced := "```json\n" + raw + "\n```"
	if _, err := ParseQuizResponse(fenced, srcs); err != nil {
		t.Fatalf("fenced JSON should parse: %v", err)
	}
}

func TestDedupe(t *testing.T) {
	qs := []QuizQuestion{
		{SourceQuestionID: "q1", DocumentID: "d1", Question: "Hello?", Options: []string{"A", "B", "C", "D"}, CorrectAnswer: 0, Explanation: "E"},
		{SourceQuestionID: "q1", DocumentID: "d1", Question: "Other?", Options: []string{"A", "B", "C", "D"}, CorrectAnswer: 0, Explanation: "E"},
		{SourceQuestionID: "q2", DocumentID: "d1", Question: "  hello? ", Options: []string{"A", "B", "C", "D"}, CorrectAnswer: 0, Explanation: "E"},
	}
	out := DedupeQuestions(qs)
	if len(out) != 1 {
		t.Fatalf("expected 1 after dedupe, got %d", len(out))
	}
}
