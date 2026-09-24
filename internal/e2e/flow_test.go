package e2e

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func happyScript() *Script {
	return &Script{
		DocID: "doc-e2e-1",
		Extracted: []Question{
			{ID: "q1", DocID: "doc-e2e-1", Text: "Explain the four necessary conditions for deadlock.", Page: 1},
			{ID: "q2", DocID: "doc-e2e-1", Text: "Describe Banker's algorithm with an example.", Page: 2},
			{ID: "q3", DocID: "doc-e2e-1", Text: "What is paging in OS?", Page: 3},
		},
		Classified: []Question{
			{ID: "q1", DocID: "doc-e2e-1", Text: "Explain the four necessary conditions for deadlock.", Page: 1, Subject: "Operating Systems", Topics: []string{"Deadlock"}},
			{ID: "q2", DocID: "doc-e2e-1", Text: "Describe Banker's algorithm with an example.", Page: 2, Subject: "Operating Systems", Topics: []string{"Deadlock"}},
			{ID: "q3", DocID: "doc-e2e-1", Text: "What is paging in OS?", Page: 3, Subject: "Operating Systems", Topics: []string{"Memory Management"}},
		},
	}
}

func happyAnswers() map[string]int {
	return map[string]int{
		// Generated items default to CorrectAnswer = index%4, i.e. 0 and 1.
		"quiz-item-1": 0, // correct
		"quiz-item-2": 3, // wrong
	}
}

// TestUploadToResults_HappyPath scripts the whole PLAN4.md Phase 22 journey:
// Upload PDF → Process/Extract → Classify → Embed → Select topic →
// Retrieve → Generate quiz → Answer → Results.
func TestUploadToResults_HappyPath(t *testing.T) {
	deps, bundle := NewFakeDeps(happyScript())
	flow := &Flow{Deps: deps}

	out, err := flow.Run(context.Background(), FlowRequest{
		Upload:    UploadInput{Filename: "os-pyq.pdf", Content: []byte("%PDF-1.4 fake body")},
		WantTopic: "Deadlock",
		Answers:   happyAnswers(),
	})
	if err != nil {
		t.Fatalf("flow.Run: %v", err)
	}

	// Stage order must match the documented pipeline.
	if !reflect.DeepEqual(out.Trace, Stages) {
		t.Fatalf("trace = %v, want %v", out.Trace, Stages)
	}

	// Upload → document stored.
	if out.Doc.ID != "doc-e2e-1" || out.Doc.Status != "uploaded" {
		t.Fatalf("doc = %+v, want id doc-e2e-1 status uploaded", out.Doc)
	}

	// Retrieve → only Deadlock questions back the quiz.
	if len(out.Quiz.Items) != 2 {
		t.Fatalf("quiz items = %d, want 2 (Deadlock only)", len(out.Quiz.Items))
	}
	for _, item := range out.Quiz.Items {
		if item.Topic != "Deadlock" {
			t.Fatalf("quiz item topic = %q, want Deadlock", item.Topic)
		}
		if len(item.Options) != 4 {
			t.Fatalf("quiz item %s options = %d, want 4", item.ID, len(item.Options))
		}
	}

	// Answer → Results: 1/2 correct = 50%.
	if out.Result.Total != 2 || out.Result.Correct != 1 || out.Result.ScorePct != 50 {
		t.Fatalf("result = %+v, want total 2 correct 1 score 50", out.Result)
	}
	if !reflect.DeepEqual(out.Result.PerItem, []bool{true, false}) {
		t.Fatalf("per-item = %v, want [true false]", out.Result.PerItem)
	}

	// Every fake ran exactly once (embed marks, topics listed+picked).
	if bundle.Upload.Calls != 1 || bundle.Extract.Calls != 1 || bundle.Classify.Calls != 1 ||
		bundle.Embed.Calls != 1 || bundle.Retrieve.Calls != 1 || bundle.Quiz.Calls != 1 ||
		bundle.Answer.Calls != 1 || bundle.Topics.Lists != 1 || bundle.Topics.Picks != 1 {
		t.Fatalf("unexpected call counts: %+v topics=%+v", bundle, bundle.Topics)
	}
}

func TestUploadToResults_RejectsBadUploads(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   UploadInput
		want string
	}{
		{"empty filename", UploadInput{Filename: "", Content: []byte("%PDF-1.4 x")}, "filename"},
		{"non-pdf extension", UploadInput{Filename: "notes.txt", Content: []byte("%PDF-1.4 x")}, ".pdf"},
		{"empty file", UploadInput{Filename: "a.pdf", Content: nil}, "empty"},
		{"non-pdf magic", UploadInput{Filename: "a.pdf", Content: []byte("hello")}, "not a PDF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps, _ := NewFakeDeps(happyScript())
			_, err := (&Flow{Deps: deps}).Run(context.Background(), FlowRequest{
				Upload: tc.in, WantTopic: "Deadlock", Answers: happyAnswers(),
			})
			if err == nil || !strings.Contains(err.Error(), "upload") {
				t.Fatalf("err = %v, want upload-stage error", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

// TestUploadToResults_StageFailures ensures a failure at any stage aborts the
// flow, names the stage, and never reaches the results.
func TestUploadToResults_StageFailures(t *testing.T) {
	for _, stage := range []string{"upload", "extract", "classify", "embed", "list-topics", "select-topic", "retrieve", "generate-quiz", "answer"} {
		t.Run(stage, func(t *testing.T) {
			script := happyScript()
			script.FailAt = stage
			script.FailErr = fmt.Errorf("boom at %s", stage)
			deps, _ := NewFakeDeps(script)
			_, err := (&Flow{Deps: deps}).Run(context.Background(), FlowRequest{
				Upload:    UploadInput{Filename: "os-pyq.pdf", Content: []byte("%PDF-1.4 fake")},
				WantTopic: "Deadlock",
				Answers:   happyAnswers(),
			})
			if err == nil {
				t.Fatalf("expected error at stage %s", stage)
			}
			if !strings.Contains(err.Error(), stage) {
				t.Fatalf("err = %v, want stage name %q", err, stage)
			}
		})
	}
}

func TestUploadToResults_UnknownTopicAndEmptyRetrieval(t *testing.T) {
	deps, _ := NewFakeDeps(happyScript())
	flow := &Flow{Deps: deps}
	base := FlowRequest{
		Upload:  UploadInput{Filename: "os-pyq.pdf", Content: []byte("%PDF-1.4 fake")},
		Answers: happyAnswers(),
	}

	base.WantTopic = "No Such Topic"
	if _, err := flow.Run(context.Background(), base); err == nil ||
		!strings.Contains(err.Error(), "select-topic") {
		t.Fatalf("unknown topic err = %v, want select-topic error", err)
	}

	// Topic exists in the listing but has no embedded questions: craft via
	// classification that tags nothing with the wanted topic.
	script := happyScript()
	for i := range script.Classified {
		script.Classified[i].Topics = []string{"Memory Management"}
	}
	deps2, _ := NewFakeDeps(script)
	_, err := (&Flow{Deps: deps2}).Run(context.Background(), FlowRequest{
		Upload:    UploadInput{Filename: "os-pyq.pdf", Content: []byte("%PDF-1.4 fake")},
		WantTopic: "Deadlock",
		Answers:   happyAnswers(),
	})
	if err == nil {
		t.Fatalf("expected retrieval error for topic with no questions")
	}
	if !strings.Contains(err.Error(), "select-topic") && !strings.Contains(err.Error(), "retrieve") {
		t.Fatalf("err = %v, want select-topic or retrieve stage", err)
	}
}

func TestUploadToResults_MissingAnswer(t *testing.T) {
	deps, _ := NewFakeDeps(happyScript())
	_, err := (&Flow{Deps: deps}).Run(context.Background(), FlowRequest{
		Upload:    UploadInput{Filename: "os-pyq.pdf", Content: []byte("%PDF-1.4 fake")},
		WantTopic: "Deadlock",
		Answers:   map[string]int{"quiz-item-1": 0}, // quiz-item-2 unanswered
	})
	if err == nil || !strings.Contains(err.Error(), "answer") {
		t.Fatalf("err = %v, want answer-stage error", err)
	}
}
