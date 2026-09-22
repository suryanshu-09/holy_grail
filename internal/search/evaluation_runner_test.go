package search

import (
	"context"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

func runnerTestSetup() (*Service, *stubKeywordRepo, *HybridService) {
	repo := newFakeRepository()
	embedder := newFakeEmbedder()
	q1Text := "deadlock prevention and avoidance"
	q2Text := "paging and virtual memory"
	repo.questions = []questions.Question{
		{ID: "q-deadlock-001", DocumentID: "d1", QuestionText: &q1Text},
		{ID: "q-paging-001", DocumentID: "d1", QuestionText: &q2Text},
	}
	repo.vectors["q-deadlock-001"] = makeVector(map[int]float32{0: 1})
	repo.vectors["q-paging-001"] = makeVector(map[int]float32{1: 1})

	vectorSvc, err := NewService(repo, embedder)
	if err != nil {
		panic(err)
	}
	kw := &stubKeywordRepo{results: []Result{
		{Question: repo.questions[0], Similarity: 0.9},
		{Question: repo.questions[1], Similarity: 0.1},
	}}
	vecStub := &stubVectorRepo{results: []Result{
		{Question: repo.questions[0], Similarity: 0.9},
		{Question: repo.questions[1], Similarity: 0.1},
	}}
	hybridSvc, err := NewHybridService(vecStub, kw, embedder)
	if err != nil {
		panic(err)
	}
	hybridSvc.SetReranker(NewExactMatchReranker(0.5))
	return vectorSvc, kw, hybridSvc
}

func TestStrategyRunner_RunAllStrategies(t *testing.T) {
	vectorSvc, kw, hybridSvc := runnerTestSetup()
	runner, err := NewStrategyRunner(vectorSvc, kw, hybridSvc, RunnerConfig{Limit: 10})
	if err != nil {
		t.Fatalf("NewStrategyRunner: %v", err)
	}
	dataset := []EvalQuery{
		{Query: "deadlock questions", ExpectedIDs: []string{"q-deadlock-001"}, Category: "Deadlock"},
		{Query: "paging questions", ExpectedIDs: []string{"q-paging-001"}, Category: "Paging"},
	}
	report, err := runner.Run(context.Background(), dataset)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Results) != len(AllStrategies) {
		t.Fatalf("results = %d strategies, want %d", len(report.Results), len(AllStrategies))
	}
	for _, s := range AllStrategies {
		perQuery, ok := report.PerQuery[s]
		if !ok || len(perQuery) != len(dataset) {
			t.Errorf("strategy %s per-query = %d, want %d", s, len(perQuery), len(dataset))
		}
		if _, ok := report.Results[s]; !ok {
			t.Errorf("strategy %s missing aggregate", s)
		}
	}
}

func TestStrategyRunner_Validation(t *testing.T) {
	vectorSvc, kw, hybridSvc := runnerTestSetup()
	if _, err := NewStrategyRunner(nil, kw, hybridSvc, RunnerConfig{}); err == nil {
		t.Errorf("expected error for nil vector service")
	}
	if _, err := NewStrategyRunner(vectorSvc, nil, hybridSvc, RunnerConfig{}); err == nil {
		t.Errorf("expected error for nil keyword repo")
	}
	if _, err := NewStrategyRunner(vectorSvc, kw, nil, RunnerConfig{}); err == nil {
		t.Errorf("expected error for nil hybrid service")
	}
	runner, _ := NewStrategyRunner(vectorSvc, kw, hybridSvc, RunnerConfig{})
	if _, err := runner.Run(context.Background(), nil); err == nil {
		t.Errorf("expected error for empty dataset")
	}
	if _, err := runner.RetrieveIDs(context.Background(), RetrievalStrategy("bogus"), EvalQuery{Query: "x"}); err == nil {
		t.Errorf("expected error for unknown strategy")
	}
}
