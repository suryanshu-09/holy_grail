package embeddings

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

type fakeQuestionReader struct {
	questions []questions.Question
}

func (r fakeQuestionReader) List(_ context.Context, filter questions.Filter) ([]questions.Question, error) {
	if filter.Offset >= len(r.questions) {
		return []questions.Question{}, nil
	}
	end := filter.Offset + filter.Limit
	if end > len(r.questions) {
		end = len(r.questions)
	}
	return r.questions[filter.Offset:end], nil
}

type fakeTopicReader struct {
	byQuestion map[string][]topics.Topic
}

func (r fakeTopicReader) ListTopicsForQuestion(_ context.Context, questionID string) ([]topics.Topic, error) {
	return r.byQuestion[questionID], nil
}

type fakeRepository struct {
	records map[string]Record
	vectors map[string][]float32
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{records: make(map[string]Record), vectors: make(map[string][]float32)}
}

func (r *fakeRepository) Get(_ context.Context, questionID string) (Record, bool, error) {
	record, ok := r.records[questionID]
	return record, ok, nil
}

func (r *fakeRepository) FindReusable(_ context.Context, model, inputHash string) (Record, bool, error) {
	for _, record := range r.records {
		if record.Model == model && record.InputHash == inputHash {
			return record, true, nil
		}
	}
	return Record{}, false, nil
}

func (r *fakeRepository) Copy(_ context.Context, questionID, sourceQuestionID, model, inputHash string) error {
	vector, ok := r.vectors[sourceQuestionID]
	if !ok {
		return errors.New("source vector not found")
	}
	r.vectors[questionID] = vector
	r.records[questionID] = Record{QuestionID: questionID, Model: model, InputHash: inputHash}
	return nil
}

func (r *fakeRepository) Upsert(_ context.Context, questionID string, vector []float32, model, inputHash string) error {
	r.vectors[questionID] = vector
	r.records[questionID] = Record{QuestionID: questionID, Model: model, InputHash: inputHash}
	return nil
}

type fakeEmbedder struct {
	calls  int
	inputs [][]string
	fail   int
}

func (e *fakeEmbedder) Model() string   { return DefaultModel }
func (e *fakeEmbedder) Dimensions() int { return DefaultDimensions }
func (e *fakeEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	e.calls++
	e.inputs = append(e.inputs, append([]string(nil), inputs...))
	if e.calls <= e.fail {
		return nil, errors.New("temporary provider failure")
	}
	vectors := make([][]float32, len(inputs))
	for index := range inputs {
		vectors[index] = make([]float32, DefaultDimensions)
		vectors[index][0] = float32(index + 1)
	}
	return vectors, nil
}

func stringPointer(value string) *string { return &value }

func TestEmbedDocumentBatchesRichInputAndReusesDuplicate(t *testing.T) {
	questionText := "Explain how deadlock prevention works."
	service, err := NewService(
		fakeQuestionReader{questions: []questions.Question{
			{ID: "question-1", DocumentID: "document-1", QuestionText: &questionText, Subject: stringPointer("Operating Systems")},
			{ID: "question-2", DocumentID: "document-1", QuestionText: &questionText, Subject: stringPointer("Operating Systems")},
		}},
		fakeTopicReader{byQuestion: map[string][]topics.Topic{
			"question-1": {{Name: "Deadlock"}},
			"question-2": {{Name: "Deadlock"}},
		}},
		newFakeRepository(),
		&fakeEmbedder{},
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	embedder := service.embedder.(*fakeEmbedder)
	service.RetryBackoff = 0

	result, err := service.EmbedDocument(context.Background(), "document-1")
	if err != nil {
		t.Fatalf("EmbedDocument: %v", err)
	}
	if result.Embedded != 1 || result.Reused != 1 || result.Failed != 0 {
		t.Fatalf("result = %+v, want one embedded and one reused", result)
	}
	if embedder.calls != 1 || len(embedder.inputs[0]) != 1 {
		t.Fatalf("calls = %d inputs = %#v, want one deduplicated request", embedder.calls, embedder.inputs)
	}
	input := embedder.inputs[0][0]
	for _, expected := range []string{"Subject: Operating Systems", "- Deadlock", "Question:\nExplain how deadlock prevention works."} {
		if !strings.Contains(input, expected) {
			t.Errorf("input %q does not contain %q", input, expected)
		}
	}

	result, err = service.EmbedDocument(context.Background(), "document-1")
	if err != nil {
		t.Fatalf("second EmbedDocument: %v", err)
	}
	if result.Skipped != 2 || embedder.calls != 1 {
		t.Errorf("second result = %+v calls = %d, want two skipped and no new call", result, embedder.calls)
	}
}

func TestEmbedDocumentRetriesTemporaryFailure(t *testing.T) {
	questionText := "What is paging?"
	embedder := &fakeEmbedder{fail: 2}
	service, err := NewService(
		fakeQuestionReader{questions: []questions.Question{{ID: "question-1", DocumentID: "document-1", QuestionText: &questionText}}},
		fakeTopicReader{}, newFakeRepository(), embedder,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.RetryBackoff = 0

	result, err := service.EmbedDocument(context.Background(), "document-1")
	if err != nil {
		t.Fatalf("EmbedDocument: %v", err)
	}
	if result.Embedded != 1 || result.Failed != 0 || embedder.calls != 3 {
		t.Errorf("result = %+v calls = %d, want one successful third attempt", result, embedder.calls)
	}
}

func TestEmbedDocumentReportsEmptyQuestion(t *testing.T) {
	empty := "  "
	service, err := NewService(
		fakeQuestionReader{questions: []questions.Question{{ID: "question-1", DocumentID: "document-1", QuestionText: &empty}}},
		fakeTopicReader{}, newFakeRepository(), &fakeEmbedder{},
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	result, err := service.EmbedDocument(context.Background(), "document-1")
	if err != nil {
		t.Fatalf("EmbedDocument: %v", err)
	}
	if result.Status() != "partial" || result.Failed != 1 || len(result.Failures) != 1 {
		t.Errorf("result = %+v, want one reported failure", result)
	}
}
