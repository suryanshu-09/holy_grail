package embeddings

import (
	"context"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

// TestGlobalEmbeddingCacheHit verifies that a vector embedded once is reused
// from the process-wide cache on a later run without calling the provider,
// even when the repository is fresh (no DB reuse possible).
func TestGlobalEmbeddingCacheHit(t *testing.T) {
	ClearGlobalEmbeddingCache()
	questionText := "Explain how deadlock prevention works."
	questionSubject := "Operating Systems"

	embedder1 := &fakeEmbedder{}
	svc1, err := NewService(
		fakeQuestionReader{questions: []questions.Question{
			{ID: "question-1", DocumentID: "document-1", QuestionText: &questionText, Subject: &questionSubject},
		}},
		fakeTopicReader{byQuestion: map[string][]topics.Topic{}},
		newFakeRepository(),
		embedder1,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc1.RetryBackoff = 0
	res1, err := svc1.EmbedDocument(context.Background(), "document-1")
	if err != nil {
		t.Fatalf("first EmbedDocument: %v", err)
	}
	if res1.Embedded != 1 || embedder1.calls != 1 {
		t.Fatalf("first result = %+v calls = %d, want 1 embedded via provider", res1, embedder1.calls)
	}

	// Fresh repository + fresh embedder: the DB cannot reuse anything, so a
	// provider-free Reused proves the process-wide cache hit.
	embedder2 := &fakeEmbedder{}
	svc2, err := NewService(
		fakeQuestionReader{questions: []questions.Question{
			{ID: "question-1", DocumentID: "document-1", QuestionText: &questionText, Subject: &questionSubject},
		}},
		fakeTopicReader{byQuestion: map[string][]topics.Topic{}},
		newFakeRepository(),
		embedder2,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc2.RetryBackoff = 0
	res2, err := svc2.EmbedDocument(context.Background(), "document-1")
	if err != nil {
		t.Fatalf("second EmbedDocument: %v", err)
	}
	if res2.Reused != 1 {
		t.Errorf("second result = %+v, want 1 reused from process cache", res2)
	}
	if embedder2.calls != 0 {
		t.Errorf("provider calls on cache hit = %d, want 0", embedder2.calls)
	}
	ClearGlobalEmbeddingCache()
}
