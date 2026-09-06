package search

import (
	"context"
	"math"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/embeddings"
	"github.com/suryanshu-09/holy_grail/internal/questions"
)

// fakeEmbedder for tests
type fakeEmbedder struct {
	model      string
	dimensions int
	vectors    map[string][]float32
	calls      int
	fail       bool
}

func newFakeEmbedder() *fakeEmbedder {
	return &fakeEmbedder{
		model:      embeddings.DefaultModel,
		dimensions: embeddings.DefaultDimensions,
		vectors:    make(map[string][]float32),
	}
}

func (f *fakeEmbedder) Model() string   { return f.model }
func (f *fakeEmbedder) Dimensions() int { return f.dimensions }
func (f *fakeEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	f.calls++
	if f.fail {
		return nil, fmtError("embed failed")
	}
	out := make([][]float32, len(inputs))
	for i, input := range inputs {
		if v, ok := f.vectors[input]; ok {
			out[i] = v
		} else {
			// deterministic fake: hash-like vector based on input length
			v := make([]float32, embeddings.DefaultDimensions)
			// encode input into first few dims for predictable similarity
			// Use simple pattern: if input contains "deadlock", high value in dim 0
			// if contains "paging", high value in dim 1
			if contains(input, "deadlock") || contains(input, "Deadlock") {
				v[0] = 1.0
			}
			if contains(input, "paging") || contains(input, "Paging") {
				v[1] = 1.0
			}
			if contains(input, "scheduling") || contains(input, "Scheduling") {
				v[2] = 1.0
			}
			// Normalize to unit length for cosine
			norm := float32(0)
			for _, x := range v {
				norm += x * x
			}
			norm = float32(math.Sqrt(float64(norm)))
			if norm > 0 {
				for idx := range v {
					v[idx] /= norm
				}
			}
			// add small non-zero to avoid zero vector
			if norm == 0 {
				v[0] = 0.1
				v[1] = 0.1
				// normalize
				norm = float32(math.Sqrt(float64(v[0]*v[0] + v[1]*v[1])))
				for idx := range v {
					v[idx] /= norm
				}
			}
			out[i] = v
		}
	}
	return out, nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}()
}

type fakeError string

func (e fakeError) Error() string { return string(e) }
func fmtError(msg string) error   { return fakeError(msg) }

// fakeRepository computes cosine similarity in-memory
type fakeRepository struct {
	questions []questions.Question
	vectors   map[string][]float32 // questionID -> vector
	metric    Metric
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		vectors: make(map[string][]float32),
		metric:  DefaultMetric,
	}
}

func (r *fakeRepository) Search(_ context.Context, vector []float32, filter Filter) ([]Result, error) {
	clamp(&filter)
	if filter.Threshold != nil && (*filter.Threshold < 0 || *filter.Threshold > 1) {
		return nil, fmtError("threshold must be between 0 and 1")
	}
	// compute similarity for each question
	type scored struct {
		res Result
	}
	var scoredList []Result
	for _, q := range r.questions {
		vec, ok := r.vectors[q.ID]
		if !ok {
			continue
		}
		// metadata filtering
		if filter.Subject != "" && (q.Subject == nil || *q.Subject != filter.Subject) {
			continue
		}
		if filter.Year != nil && (q.Year == nil || *q.Year != *filter.Year) {
			continue
		}
		if filter.YearMin != nil && (q.Year == nil || *q.Year < *filter.YearMin) {
			continue
		}
		if filter.YearMax != nil && (q.Year == nil || *q.Year > *filter.YearMax) {
			continue
		}
		if filter.DocumentID != "" && q.DocumentID != filter.DocumentID {
			continue
		}
		if filter.QuestionType != "" && (q.QuestionType == nil || *q.QuestionType != filter.QuestionType) {
			continue
		}
		if filter.Difficulty != "" && (q.Difficulty == nil || *q.Difficulty != filter.Difficulty) {
			continue
		}
		// topic filters not tested in fake (requires join); skip for now
		// compute cosine similarity = dot / (norms) ; vectors are normalized
		similarity := dot(vector, vec)
		// clamp to [-1,1]
		if similarity > 1 {
			similarity = 1
		}
		if similarity < -1 {
			similarity = -1
		}
		distance := 1 - similarity
		if filter.Threshold != nil && similarity < *filter.Threshold {
			continue
		}
		scoredList = append(scoredList, Result{
			Question:   q,
			Similarity: similarity,
			Distance:   distance,
		})
	}
	// sort by similarity desc (distance asc)
	for i := 0; i < len(scoredList); i++ {
		for j := i + 1; j < len(scoredList); j++ {
			if scoredList[j].Similarity > scoredList[i].Similarity {
				scoredList[i], scoredList[j] = scoredList[j], scoredList[i]
			}
		}
	}
	// apply offset/limit
	start := filter.Offset
	if start > len(scoredList) {
		start = len(scoredList)
	}
	end := start + filter.Limit
	if end > len(scoredList) {
		end = len(scoredList)
	}
	return scoredList[start:end], nil
}

func dot(a, b []float32) float64 {
	sum := 0.0
	for i := range a {
		if i >= len(b) {
			break
		}
		sum += float64(a[i] * b[i])
	}
	return sum
}

func ptrString(s string) *string { return &s }
func ptrInt(i int) *int          { return &i }

func makeVector(dims map[int]float32) []float32 {
	v := make([]float32, embeddings.DefaultDimensions)
	for idx, val := range dims {
		v[idx] = val
	}
	// normalize
	norm := 0.0
	for _, x := range v {
		norm += float64(x * x)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range v {
			v[i] = float32(float64(v[i]) / norm)
		}
	}
	return v
}

func TestSearch_TopKRetrieval(t *testing.T) {
	repo := newFakeRepository()
	embedder := newFakeEmbedder()

	// Create 3 questions with known vectors
	q1Text := "Explain deadlock prevention"
	q2Text := "Explain paging"
	q3Text := "Explain CPU scheduling"
	repo.questions = []questions.Question{
		{ID: "q1", DocumentID: "d1", QuestionText: &q1Text, Subject: ptrString("Operating Systems"), Year: ptrInt(2022)},
		{ID: "q2", DocumentID: "d1", QuestionText: &q2Text, Subject: ptrString("Operating Systems"), Year: ptrInt(2021)},
		{ID: "q3", DocumentID: "d1", QuestionText: &q3Text, Subject: ptrString("Operating Systems"), Year: ptrInt(2020)},
	}
	repo.vectors["q1"] = makeVector(map[int]float32{0: 1})
	repo.vectors["q2"] = makeVector(map[int]float32{1: 1})
	repo.vectors["q3"] = makeVector(map[int]float32{2: 1})

	svc, err := NewService(repo, embedder)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	// Search for deadlock -> should return q1 top
	resp, err := svc.Search(context.Background(), "Questions about avoiding deadlocks", Filter{Limit: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.Results[0].Question.ID != "q1" {
		t.Errorf("expected q1 top, got %s", resp.Results[0].Question.ID)
	}
	if resp.Results[0].Similarity <= resp.Results[1].Similarity {
		t.Errorf("results not sorted by similarity desc: %v <= %v", resp.Results[0].Similarity, resp.Results[1].Similarity)
	}
	if resp.Metric != DefaultMetric {
		t.Errorf("metric = %q want %q", resp.Metric, DefaultMetric)
	}
	if resp.Model != embeddings.DefaultModel {
		t.Errorf("model = %q want %q", resp.Model, embeddings.DefaultModel)
	}
}

func TestSearch_MetadataFiltering(t *testing.T) {
	repo := newFakeRepository()
	embedder := newFakeEmbedder()
	q1Text := "deadlock"
	q2Text := "deadlock"
	q3Text := "deadlock"
	repo.questions = []questions.Question{
		{ID: "q1", DocumentID: "d1", QuestionText: &q1Text, Subject: ptrString("Operating Systems"), Year: ptrInt(2022), Difficulty: ptrString("hard")},
		{ID: "q2", DocumentID: "d1", QuestionText: &q2Text, Subject: ptrString("DBMS"), Year: ptrInt(2021), Difficulty: ptrString("easy")},
		{ID: "q3", DocumentID: "d2", QuestionText: &q3Text, Subject: ptrString("Operating Systems"), Year: ptrInt(2020), Difficulty: ptrString("hard")},
	}
	// All same vector for similar query
	repo.vectors["q1"] = makeVector(map[int]float32{0: 1})
	repo.vectors["q2"] = makeVector(map[int]float32{0: 1})
	repo.vectors["q3"] = makeVector(map[int]float32{0: 1})

	svc, _ := NewService(repo, embedder)

	// Filter by subject
	resp, _ := svc.Search(context.Background(), "deadlock", Filter{Subject: "Operating Systems", Limit: 10})
	if len(resp.Results) != 2 {
		t.Fatalf("subject filter expected 2, got %d", len(resp.Results))
	}
	// Filter by year_min
	resp, _ = svc.Search(context.Background(), "deadlock", Filter{YearMin: ptrInt(2022), Limit: 10})
	if len(resp.Results) != 1 || resp.Results[0].Question.ID != "q1" {
		t.Fatalf("year_min filter failed: %v", resp.Results)
	}
	resp, _ = svc.Search(context.Background(), "deadlock", Filter{YearMax: ptrInt(2021), Limit: 10})
	if len(resp.Results) != 2 {
		t.Fatalf("year_max filter expected 2, got %d", len(resp.Results))
	}
	// Exact year
	resp, _ = svc.Search(context.Background(), "deadlock", Filter{Year: ptrInt(2022), Limit: 10})
	if len(resp.Results) != 1 || resp.Results[0].Question.ID != "q1" {
		t.Fatalf("year exact filter failed")
	}
	// Document filter
	resp, _ = svc.Search(context.Background(), "deadlock", Filter{DocumentID: "d2", Limit: 10})
	if len(resp.Results) != 1 || resp.Results[0].Question.ID != "q3" {
		t.Fatalf("document filter failed")
	}
	// Difficulty filter
	resp, _ = svc.Search(context.Background(), "deadlock", Filter{Difficulty: "hard", Limit: 10})
	if len(resp.Results) != 2 {
		t.Fatalf("difficulty filter expected 2, got %d", len(resp.Results))
	}
	// Question type filter
	qt := "mcq"
	repo.questions[0].QuestionType = &qt
	resp, _ = svc.Search(context.Background(), "deadlock", Filter{QuestionType: "mcq", Limit: 10})
	if len(resp.Results) != 1 || resp.Results[0].Question.ID != "q1" {
		t.Fatalf("question_type filter failed")
	}
}

func TestSearch_ThresholdAndSimilarityScores(t *testing.T) {
	repo := newFakeRepository()
	embedder := newFakeEmbedder()
	q1Text := "deadlock"
	q2Text := "paging"
	q3Text := "scheduling"
	repo.questions = []questions.Question{
		{ID: "q1", QuestionText: &q1Text},
		{ID: "q2", QuestionText: &q2Text},
		{ID: "q3", QuestionText: &q3Text},
	}
	repo.vectors["q1"] = makeVector(map[int]float32{0: 1})
	repo.vectors["q2"] = makeVector(map[int]float32{1: 1})
	repo.vectors["q3"] = makeVector(map[int]float32{2: 1})

	svc, _ := NewService(repo, embedder)

	// Query deadlock, threshold 0.9 should only return q1 (perfect match ~1)
	thresh := 0.9
	resp, _ := svc.Search(context.Background(), "deadlock question", Filter{Threshold: &thresh, Limit: 10})
	if len(resp.Results) != 1 || resp.Results[0].Question.ID != "q1" {
		t.Fatalf("threshold 0.9 failed: got %v", resp.Results)
	}
	if resp.Results[0].Similarity < 0.9 {
		t.Errorf("similarity %v < threshold 0.9", resp.Results[0].Similarity)
	}
	if resp.Results[0].Distance != 1-resp.Results[0].Similarity {
		t.Errorf("distance %v != 1 - similarity %v", resp.Results[0].Distance, resp.Results[0].Similarity)
	}
	// Threshold 0.0 should return all
	threshZero := 0.0
	resp, _ = svc.Search(context.Background(), "deadlock", Filter{Threshold: &threshZero, Limit: 10})
	if len(resp.Results) != 3 {
		t.Fatalf("threshold 0 expected 3, got %d", len(resp.Results))
	}
	// Check ordering and scores are in [0,1] and descending
	prev := 2.0
	for _, r := range resp.Results {
		if r.Similarity < 0 || r.Similarity > 1 {
			t.Errorf("similarity out of range: %v", r.Similarity)
		}
		if r.Similarity > prev {
			t.Errorf("not sorted desc: %v > %v", r.Similarity, prev)
		}
		prev = r.Similarity
	}
}

func TestSearch_Validation(t *testing.T) {
	repo := newFakeRepository()
	embedder := newFakeEmbedder()
	svc, _ := NewService(repo, embedder)

	_, err := svc.Search(context.Background(), "   ", Filter{})
	if err == nil {
		t.Errorf("expected error for empty query")
	}
	threshInvalid := 1.5
	_, err = svc.Search(context.Background(), "query", Filter{Threshold: &threshInvalid})
	if err == nil {
		t.Errorf("expected error for invalid threshold")
	}
	// Embedder dimension mismatch
	badEmbedder := &fakeEmbedder{model: embeddings.DefaultModel, dimensions: 512}
	_, err = NewService(repo, badEmbedder)
	if err == nil {
		t.Errorf("expected error for dimension mismatch")
	}
	_, err = NewService(nil, embedder)
	if err == nil {
		t.Errorf("expected error for nil repo")
	}
	_, err = NewService(repo, nil)
	if err == nil {
		t.Errorf("expected error for nil embedder")
	}
}

func TestSearch_Pagination(t *testing.T) {
	repo := newFakeRepository()
	embedder := newFakeEmbedder()
	// 5 questions all same vector
	for i := 0; i < 5; i++ {
		text := "deadlock"
		q := questions.Question{ID: string(rune('a' + i)), QuestionText: &text}
		repo.questions = append(repo.questions, q)
		repo.vectors[q.ID] = makeVector(map[int]float32{0: 1})
	}
	svc, _ := NewService(repo, embedder)
	resp, _ := svc.Search(context.Background(), "deadlock", Filter{Limit: 2, Offset: 0})
	if len(resp.Results) != 2 {
		t.Fatalf("limit 2 expected 2, got %d", len(resp.Results))
	}
	resp2, _ := svc.Search(context.Background(), "deadlock", Filter{Limit: 2, Offset: 2})
	if len(resp2.Results) != 2 {
		t.Fatalf("offset 2 limit 2 expected 2, got %d", len(resp2.Results))
	}
	if resp.Results[0].Question.ID == resp2.Results[0].Question.ID {
		t.Errorf("pagination not working, same first result")
	}
}

func TestRepository_OperatorAndMetric(t *testing.T) {
	// Validate metric configuration maps to correct operator
	repoCosine := NewRepository(nil, MetricCosine, embeddings.DefaultModel).(*repository)
	if repoCosine.operator() != "<=>" {
		t.Errorf("cosine operator = %q want <=>", repoCosine.operator())
	}
	repoL2 := NewRepository(nil, MetricL2, embeddings.DefaultModel).(*repository)
	if repoL2.operator() != "<->" {
		t.Errorf("l2 operator = %q want <->", repoL2.operator())
	}
	repoIP := NewRepository(nil, MetricInnerProduct, embeddings.DefaultModel).(*repository)
	if repoIP.operator() != "<#>" {
		t.Errorf("ip operator = %q want <#>", repoIP.operator())
	}
	if diff := repoCosine.thresholdToDistance(0.7) - 0.3; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cosine thresholdToDistance 0.7 = %v want 0.3", repoCosine.thresholdToDistance(0.7))
	}
	if diff := repoL2.thresholdToDistance(0.5) - 1.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("l2 threshold 0.5 distance = %v want 1.0", repoL2.thresholdToDistance(0.5))
	}
}
