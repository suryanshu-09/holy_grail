package extraction

import (
	"testing"
)

func TestValidateQuestionBoundaries(t *testing.T) {
	valid := PreviewQuestion{
		Text:       "What is paging?",
		StartPage:  1,
		EndPage:    2,
		Confidence: 0.8,
		Type:       "MCQ",
	}
	tests := []struct {
		name    string
		mutate  func(*PreviewQuestion)
		wantErr bool
	}{
		{"valid", func(*PreviewQuestion) {}, false},
		{"empty text", func(q *PreviewQuestion) { q.Text = "   " }, true},
		{"zero start page", func(q *PreviewQuestion) { q.StartPage = 0 }, true},
		{"negative start page", func(q *PreviewQuestion) { q.StartPage = -1 }, true},
		{"end before start", func(q *PreviewQuestion) { q.EndPage = 0 }, true},
		{"single page span", func(q *PreviewQuestion) { q.StartPage, q.EndPage = 3, 3 }, false},
		{"confidence below zero", func(q *PreviewQuestion) { q.Confidence = -0.1 }, true},
		{"confidence above one", func(q *PreviewQuestion) { q.Confidence = 1.1 }, true},
		{"confidence zero edge", func(q *PreviewQuestion) { q.Confidence = 0 }, false},
		{"confidence one edge", func(q *PreviewQuestion) { q.Confidence = 1 }, false},
		{"unknown type", func(q *PreviewQuestion) { q.Type = "essay" }, true},
		{"empty type allowed", func(q *PreviewQuestion) { q.Type = "" }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := valid
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

func TestValidateQuestionAllowedTypes(t *testing.T) {
	for _, typ := range []string{"MCQ", "MSQ", "numerical", "descriptive", "true_false", "unknown"} {
		q := PreviewQuestion{Text: "Q?", StartPage: 1, EndPage: 1, Confidence: 0.5, Type: typ}
		if err := ValidateQuestion(q); err != nil {
			t.Fatalf("type %q rejected: %v", typ, err)
		}
	}
}
