package embeddings

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

// countingBatchTopicReader implements both TopicReader and BatchTopicReader
// and counts calls so tests can prove N+1 avoidance.
type countingBatchTopicReader struct {
	byQuestion map[string][]topics.Topic
	batchCalls int
	singleCalls int
	batchErr   error
}

func (r *countingBatchTopicReader) ListTopicsForQuestion(_ context.Context, questionID string) ([]topics.Topic, error) {
	r.singleCalls++
	return r.byQuestion[questionID], nil
}

func (r *countingBatchTopicReader) ListTopicsForQuestions(_ context.Context, questionIDs []string) (map[string][]topics.Topic, error) {
	r.batchCalls++
	if r.batchErr != nil {
		return nil, r.batchErr
	}
	out := make(map[string][]topics.Topic, len(questionIDs))
	for _, id := range questionIDs {
		out[id] = r.byQuestion[id]
	}
	return out, nil
}

// batchCapableRepo extends the test fake repository with batch methods.
type batchCapableRepo struct {
	*fakeRepository
	batchUpserts int
	upserts      int
	batchErr     error
}

func (r *batchCapableRepo) Upsert(ctx context.Context, questionID string, vector []float32, model, inputHash string) error {
	r.upserts++
	return r.fakeRepository.Upsert(ctx, questionID, vector, model, inputHash)
}

func (r *batchCapableRepo) BatchUpsert(ctx context.Context, items []UpsertItem) error {
	r.batchUpserts++
	if r.batchErr != nil {
		return r.batchErr
	}
	for _, item := range items {
		if err := r.fakeRepository.Upsert(ctx, item.QuestionID, item.Vector, item.Model, item.InputHash); err != nil {
			return err
		}
	}
	return nil
}

func batchTestQuestions(tag string, n int) []questions.Question {
	subj := "Operating Systems"
	qs := make([]questions.Question, n)
	for i := range qs {
		// Unique text per test tag (and per question) so the process-wide
		// embedding cache from another run cannot alias these inputs.
		text := fmt.Sprintf("Unique %s question text number %d about paging?", tag, i)
		qs[i] = questions.Question{
			ID:           fmt.Sprintf("%s-q%d", tag, i),
			DocumentID:   "document-1",
			QuestionText: &text,
			Subject:      &subj,
		}
	}
	return qs
}

func TestEmbedDocumentUsesBatchTopicMap(t *testing.T) {
	qs := batchTestQuestions("batchmap", 5)
	byQ := make(map[string][]topics.Topic, len(qs))
	for _, q := range qs {
		byQ[q.ID] = []topics.Topic{{Name: "Memory"}}
	}
	reader := &countingBatchTopicReader{byQuestion: byQ}
	service, err := NewService(fakeQuestionReader{questions: qs}, reader, newFakeRepository(), &fakeEmbedder{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.RetryBackoff = 0

	result, err := service.EmbedDocument(context.Background(), "document-1")
	if err != nil {
		t.Fatalf("EmbedDocument: %v", err)
	}
	if result.Failed != 0 {
		t.Fatalf("result = %+v, want no failures", result)
	}
	if reader.batchCalls != 1 {
		t.Fatalf("batch calls = %d, want 1 (single query for whole page)", reader.batchCalls)
	}
	if reader.singleCalls != 0 {
		t.Fatalf("single calls = %d, want 0 (N+1 avoided)", reader.singleCalls)
	}
}

func TestEmbedDocumentFallsBackToSingleOnBatchError(t *testing.T) {
	qs := batchTestQuestions("batchfallback", 3)
	byQ := make(map[string][]topics.Topic, len(qs))
	for _, q := range qs {
		byQ[q.ID] = []topics.Topic{{Name: "Memory"}}
	}
	reader := &countingBatchTopicReader{byQuestion: byQ, batchErr: errors.New("batch unavailable")}
	service, err := NewService(fakeQuestionReader{questions: qs}, reader, newFakeRepository(), &fakeEmbedder{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.RetryBackoff = 0

	result, err := service.EmbedDocument(context.Background(), "document-1")
	if err != nil {
		t.Fatalf("EmbedDocument: %v", err)
	}
	if result.Failed != 0 {
		t.Fatalf("result = %+v, want no failures via fallback", result)
	}
	if reader.batchCalls != 1 {
		t.Fatalf("batch calls = %d, want 1 attempted", reader.batchCalls)
	}
	if reader.singleCalls != len(qs) {
		t.Fatalf("single calls = %d, want %d (fallback per question)", reader.singleCalls, len(qs))
	}
}

func TestEmbedDocumentUsesBatchUpsert(t *testing.T) {
	ClearGlobalEmbeddingCache() // isolate from process-wide vector cache
	textA := "Unique batch-upsert text about paging?"
	textB := "Unique batch-upsert text about segmentation?"
	subj := "Operating Systems"
	qs := []questions.Question{
		{ID: "qa", DocumentID: "document-1", QuestionText: &textA, Subject: &subj},
		{ID: "qb", DocumentID: "document-1", QuestionText: &textB, Subject: &subj},
	}
	repo := &batchCapableRepo{fakeRepository: newFakeRepository()}
	service, err := NewService(
		fakeQuestionReader{questions: qs},
		fakeTopicReader{byQuestion: map[string][]topics.Topic{}},
		repo,
		&fakeEmbedder{},
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.RetryBackoff = 0

	result, err := service.EmbedDocument(context.Background(), "document-1")
	if err != nil {
		t.Fatalf("EmbedDocument: %v", err)
	}
	if result.Embedded != 2 || result.Failed != 0 {
		t.Fatalf("result = %+v, want 2 embedded", result)
	}
	if repo.batchUpserts != 1 {
		t.Fatalf("batch upserts = %d, want 1", repo.batchUpserts)
	}
	if repo.upserts != 0 {
		t.Fatalf("single upserts = %d, want 0 (batch path)", repo.upserts)
	}
}

func TestEmbedDocumentFallsBackToSingleUpsert(t *testing.T) {
	ClearGlobalEmbeddingCache() // isolate from process-wide vector cache
	textA := "Unique fallback-upsert text about paging?"
	subj := "Operating Systems"
	qs := []questions.Question{
		{ID: "qa", DocumentID: "document-1", QuestionText: &textA, Subject: &subj},
	}
	repo := &batchCapableRepo{fakeRepository: newFakeRepository(), batchErr: errors.New("batch unavailable")}
	service, err := NewService(
		fakeQuestionReader{questions: qs},
		fakeTopicReader{byQuestion: map[string][]topics.Topic{}},
		repo,
		&fakeEmbedder{},
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.RetryBackoff = 0

	result, err := service.EmbedDocument(context.Background(), "document-1")
	if err != nil {
		t.Fatalf("EmbedDocument: %v", err)
	}
	if result.Embedded != 1 || result.Failed != 0 {
		t.Fatalf("result = %+v, want 1 embedded via fallback", result)
	}
	if repo.upserts != 1 {
		t.Fatalf("single upserts = %d, want 1 (fallback)", repo.upserts)
	}
}
