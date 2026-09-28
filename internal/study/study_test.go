package study

import (
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

func strp(s string) *string { return &s }

func testPYQs() []questions.Question {
	return []questions.Question{
		{
			ID:             "q1",
			DocumentID:     "d1",
			QuestionNumber: strp("5"),
			QuestionText:   strp("Explain deadlock prevention with resource allocation graph and banker algorithm for deadlock avoidance."),
			Subject:        strp("Operating Systems"),
		},
		{
			ID:           "q2",
			DocumentID:   "d1",
			QuestionText: strp("What is deadlock detection? Describe resource allocation and recovery from deadlock deadlock."),
		},
		{
			ID:           "q3",
			DocumentID:   "d2",
			QuestionText: strp("Solve $x^2 + 2x + 1 = 0$ using the quadratic formula."),
		},
	}
}

func TestBuildStudyGuideFlow(t *testing.T) {
	g := BuildStudyGuide("Deadlock", "Operating Systems", testPYQs(), true)
	if g.Topic != "Deadlock" || g.Subject != "Operating Systems" {
		t.Fatalf("topic/subject wrong: %+v", g)
	}
	if !g.WeakTopic {
		t.Fatal("weak flag should propagate")
	}
	if !strings.Contains(g.Explanation, "Deadlock") || !strings.Contains(g.Explanation, "weak area") {
		t.Fatalf("explanation should name topic + weak cue: %q", g.Explanation)
	}
	if !strings.Contains(g.Example, "q1") || !strings.Contains(g.Example, "deadlock prevention") {
		t.Fatalf("example should frame first PYQ: %q", g.Example)
	}
	if len(g.KeyPoints) == 0 || len(g.KeyPoints) > MaxKeyPoints {
		t.Fatalf("key points should be 1..%d, got %v", MaxKeyPoints, g.KeyPoints)
	}
	if !strings.Contains(strings.Join(g.KeyPoints, " "), "deadlock") {
		t.Fatalf("key points should surface recurring term deadlock: %v", g.KeyPoints)
	}
	if len(g.PYQRefs) != 3 || g.PYQRefs[0].QuestionID != "q1" {
		t.Fatalf("pyq refs wrong: %+v", g.PYQRefs)
	}
	// Multimodal summary picks up the math expression in q3.
	if !strings.Contains(g.Summary, "x^2") {
		t.Fatalf("summary should note math expression: %q", g.Summary)
	}
	// NextQuiz reuses BuildOriginalQuiz: 3 questions, traceable sources.
	if len(g.NextQuiz.Questions) != 3 {
		t.Fatalf("next quiz should have 3 questions, got %d", len(g.NextQuiz.Questions))
	}
	for i, qq := range g.NextQuiz.Questions {
		if qq.SourceQuestionID == "" || qq.DocumentID == "" || len(qq.Options) != 4 {
			t.Fatalf("next quiz question %d invalid: %+v", i, qq)
		}
	}
}

func TestBuildStudyGuideDeterministic(t *testing.T) {
	a := BuildStudyGuide("Deadlock", "", testPYQs(), false)
	b := BuildStudyGuide("Deadlock", "", testPYQs(), false)
	if a.Explanation != b.Explanation || a.Example != b.Example || a.Summary != b.Summary {
		t.Fatal("guide must be deterministic")
	}
	if len(a.KeyPoints) != len(b.KeyPoints) {
		t.Fatal("key points must be deterministic")
	}
	for i := range a.KeyPoints {
		if a.KeyPoints[i] != b.KeyPoints[i] {
			t.Fatalf("key point %d differs: %q vs %q", i, a.KeyPoints[i], b.KeyPoints[i])
		}
	}
}

func TestBuildStudyGuideEmpty(t *testing.T) {
	g := BuildStudyGuide("Paging", "", nil, false)
	if g.Topic != "Paging" {
		t.Fatalf("topic wrong: %+v", g)
	}
	if len(g.PYQRefs) != 0 || len(g.NextQuiz.Questions) != 0 {
		t.Fatalf("empty PYQs should yield empty refs/quiz: %+v", g)
	}
	if len(g.KeyPoints) == 0 || g.Example == "" || g.Explanation == "" {
		t.Fatalf("empty guide still needs explanation/example/keypoints: %+v", g)
	}
}

func TestBuildStudyGuideTrims(t *testing.T) {
	g := BuildStudyGuide("  Deadlock  ", "  OS  ", testPYQs(), false)
	if g.Topic != "Deadlock" || g.Subject != "OS" {
		t.Fatalf("topic/subject should be trimmed: %+v", g)
	}
}
