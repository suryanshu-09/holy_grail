package search

import (
	"context"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

// stubVectorRepo returns preset vector results.
type stubVectorRepo struct {
	results []Result
}

func (s *stubVectorRepo) Search(_ context.Context, _ []float32, filter Filter) ([]Result, error) {
	out := s.results
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

// stubKeywordRepo returns preset keyword results and records the query used.
type stubKeywordRepo struct {
	results []Result
	lastQuery string
}

func (s *stubKeywordRepo) Search(_ context.Context, query string, filter Filter) ([]Result, error) {
	s.lastQuery = query
	out := s.results
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func hybridTestQuestion(id, text string) questions.Question {
	t := text
	return questions.Question{ID: id, QuestionText: &t}
}

func TestHybrid_Dedupe(t *testing.T) {
	embedder := newFakeEmbedder()
	vec := &stubVectorRepo{results: []Result{
		{Question: hybridTestQuestion("q1", "deadlock prevention"), Similarity: 0.9, Distance: 0.1},
		{Question: hybridTestQuestion("q2", "bankers algorithm"), Similarity: 0.8, Distance: 0.2},
	}}
	kw := &stubKeywordRepo{results: []Result{
		{Question: hybridTestQuestion("q2", "bankers algorithm"), Similarity: 0.7, Distance: 0.3},
		{Question: hybridTestQuestion("q3", "paging"), Similarity: 0.6, Distance: 0.4},
	}}
	hs, err := NewHybridService(vec, kw, embedder)
	if err != nil {
		t.Fatalf("NewHybridService: %v", err)
	}
	resp, err := hs.Search(context.Background(), "bankers", HybridFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("expected 3 deduped results, got %d", len(resp.Results))
	}
	seen := map[string]int{}
	for _, r := range resp.Results {
		seen[r.Question.ID]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("question %s appears %d times, want 1", id, n)
		}
	}
	// q2 must list both sources.
	for _, r := range resp.Results {
		if r.Question.ID == "q2" {
			if len(r.Sources) != 2 {
				t.Errorf("q2 sources = %v want [vector keyword]", r.Sources)
			}
		}
	}
}

func TestHybrid_WeightedScoring(t *testing.T) {
	embedder := newFakeEmbedder()
	vec := &stubVectorRepo{results: []Result{
		{Question: hybridTestQuestion("q1", "vector strong"), Similarity: 0.9},
		{Question: hybridTestQuestion("q2", "keyword strong"), Similarity: 0.1},
	}}
	kw := &stubKeywordRepo{results: []Result{
		{Question: hybridTestQuestion("q1", "vector strong"), Similarity: 0.1},
		{Question: hybridTestQuestion("q2", "keyword strong"), Similarity: 0.9},
	}}
	hs, _ := NewHybridService(vec, kw, embedder)
	// Vector-heavy weights: q1 should rank first.
	resp, err := hs.Search(context.Background(), "test", HybridFilter{VectorWeight: 0.8, KeywordWeight: 0.2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.Results[0].Question.ID != "q1" {
		t.Errorf("vector-heavy: expected q1 first, got %s", resp.Results[0].Question.ID)
	}
	wantQ1 := 0.8*0.9 + 0.2*0.1
	if diff := resp.Results[0].CombinedScore - wantQ1; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("q1 combined = %v want %v", resp.Results[0].CombinedScore, wantQ1)
	}
	// Keyword-heavy weights: q2 should rank first.
	resp2, err := hs.Search(context.Background(), "test", HybridFilter{VectorWeight: 0.2, KeywordWeight: 0.8})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp2.Results[0].Question.ID != "q2" {
		t.Errorf("keyword-heavy: expected q2 first, got %s", resp2.Results[0].Question.ID)
	}
}

func TestHybrid_KeywordOnlySkipsEmbedder(t *testing.T) {
	embedder := newFakeEmbedder()
	vec := &stubVectorRepo{results: []Result{}}
	kw := &stubKeywordRepo{results: []Result{
		{Question: hybridTestQuestion("q1", "bankers algorithm"), Similarity: 0.9},
	}}
	hs, _ := NewHybridService(vec, kw, embedder)
	callsBefore := embedder.calls
	_, err := hs.Search(context.Background(), "bankers", HybridFilter{VectorWeight: 0, KeywordWeight: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if embedder.calls != callsBefore {
		t.Errorf("keyword-only should not call embedder (calls %d -> %d)", callsBefore, embedder.calls)
	}
}

func TestHybrid_DebugInfo(t *testing.T) {
	embedder := newFakeEmbedder()
	vec := &stubVectorRepo{results: []Result{
		{Question: hybridTestQuestion("q1", "a"), Similarity: 0.9},
		{Question: hybridTestQuestion("q2", "b"), Similarity: 0.8},
	}}
	kw := &stubKeywordRepo{results: []Result{
		{Question: hybridTestQuestion("q2", "b"), Similarity: 0.5},
		{Question: hybridTestQuestion("q3", "c"), Similarity: 0.4},
	}}
	hs, _ := NewHybridService(vec, kw, embedder)
	resp, err := hs.Search(context.Background(), "debug query", HybridFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Debug == nil {
		t.Fatalf("expected debug info, got nil")
	}
	d := resp.Debug
	if d.Query != "debug query" {
		t.Errorf("debug query = %q", d.Query)
	}
	if d.VectorCandidates != 2 {
		t.Errorf("vector candidates = %d want 2", d.VectorCandidates)
	}
	if d.KeywordCandidates != 2 {
		t.Errorf("keyword candidates = %d want 2", d.KeywordCandidates)
	}
	if d.MergedCandidates != 3 {
		t.Errorf("merged candidates = %d want 3", d.MergedCandidates)
	}
	if d.Scoring != "weighted" {
		t.Errorf("scoring = %q want weighted", d.Scoring)
	}
	if d.RerankEnabled {
		t.Errorf("rerank should be disabled by default")
	}
}

func TestHybrid_Rerank(t *testing.T) {
	embedder := newFakeEmbedder()
	// q1 scores higher on fusion but does not contain the query; q2 contains it.
	vec := &stubVectorRepo{results: []Result{
		{Question: hybridTestQuestion("q1", "unrelated scheduling topic"), Similarity: 0.9},
		{Question: hybridTestQuestion("q2", "bankers algorithm explained"), Similarity: 0.5},
	}}
	kw := &stubKeywordRepo{results: []Result{
		{Question: hybridTestQuestion("q1", "unrelated scheduling topic"), Similarity: 0.5},
		{Question: hybridTestQuestion("q2", "bankers algorithm explained"), Similarity: 0.5},
	}}
	hs, _ := NewHybridService(vec, kw, embedder)
	hs.SetReranker(NewExactMatchReranker(1.0))

	// Without rerank, q1 leads.
	plain, err := hs.Search(context.Background(), "bankers", HybridFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if plain.Results[0].Question.ID != "q1" {
		t.Fatalf("without rerank expected q1 first, got %s", plain.Results[0].Question.ID)
	}
	if plain.Debug.RerankEnabled {
		t.Errorf("plain debug rerank_enabled should be false")
	}

	// With rerank, q2 (exact substring) should jump to top with recorded boost.
	ranked, err := hs.Search(context.Background(), "bankers", HybridFilter{EnableRerank: true})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if ranked.Results[0].Question.ID != "q2" {
		t.Fatalf("with rerank expected q2 first, got %s", ranked.Results[0].Question.ID)
	}
	if ranked.Results[0].RerankBoost != 1.0 {
		t.Errorf("rerank boost = %v want 1.0", ranked.Results[0].RerankBoost)
	}
	if !ranked.Debug.RerankEnabled {
		t.Errorf("ranked debug rerank_enabled should be true")
	}
}

func TestHybrid_KeywordQueryOverride(t *testing.T) {
	embedder := newFakeEmbedder()
	vec := &stubVectorRepo{results: []Result{}}
	kw := &stubKeywordRepo{results: []Result{}}
	hs, _ := NewHybridService(vec, kw, embedder)
	_, err := hs.Search(context.Background(), "circular wait", HybridFilter{KeywordQuery: "Banker's algorithm"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if kw.lastQuery != "Banker's algorithm" {
		t.Errorf("keyword branch query = %q want Banker's algorithm", kw.lastQuery)
	}
	// Empty KeywordQuery falls back to main query.
	_, err = hs.Search(context.Background(), "circular wait", HybridFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.Contains(kw.lastQuery, "circular wait") {
		t.Errorf("fallback keyword query = %q want main query", kw.lastQuery)
	}
}
