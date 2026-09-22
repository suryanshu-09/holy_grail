package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/search"
)

type stubEvalRunner struct {
	report *search.ComparisonReport
	err    error
	calls  int
	gotN   int
}

func (s *stubEvalRunner) Run(_ context.Context, dataset []search.EvalQuery) (*search.ComparisonReport, error) {
	s.calls++
	s.gotN = len(dataset)
	if s.err != nil {
		return nil, s.err
	}
	return s.report, nil
}

func stubEvalReport() *search.ComparisonReport {
	perQuery := map[search.RetrievalStrategy][]search.QueryMetrics{
		search.StrategyVector: {
			{Query: "deadlock questions", Strategy: search.StrategyVector, Recall: 1, Precision: 0.5, Hits: map[int]bool{3: true, 5: true, 10: true}},
		},
		search.StrategyHybrid: {
			{Query: "deadlock questions", Strategy: search.StrategyHybrid, Recall: 1, Precision: 1, Hits: map[int]bool{3: true, 5: true, 10: true}},
		},
	}
	return &search.ComparisonReport{
		Config:   search.RunnerConfig{Limit: 10, Ks: []int{3, 5, 10}},
		PerQuery: perQuery,
		Results:  search.CompareStrategies(perQuery),
	}
}

func TestHandleDebugEvalGET_Success(t *testing.T) {
	runner := &stubEvalRunner{report: stubEvalReport()}
	handler := handleDebugEval(runner)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/debug/eval", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body %s", w.Code, w.Body.String())
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d want 1", runner.calls)
	}
	if runner.gotN != len(search.EvaluationDataset) {
		t.Errorf("dataset size = %d want %d (full bundled dataset)", runner.gotN, len(search.EvaluationDataset))
	}
	var resp debugEvalResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.NumQueries != len(search.EvaluationDataset) {
		t.Errorf("num_queries = %d want %d", resp.NumQueries, len(search.EvaluationDataset))
	}
	if len(resp.Results) == 0 {
		t.Errorf("results should not be empty")
	}
	if resp.BestByRecall == "" {
		t.Errorf("best_by_recall should be set")
	}
	if resp.Config.Limit != 10 {
		t.Errorf("config.limit = %d want 10", resp.Config.Limit)
	}
}

func TestHandleDebugEvalPOST_Success(t *testing.T) {
	runner := &stubEvalRunner{report: stubEvalReport()}
	handler := handleDebugEval(runner)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/debug/eval", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST expected 200 got %d body %s", w.Code, w.Body.String())
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d want 1", runner.calls)
	}
}

func TestHandleDebugEval_NilRunner(t *testing.T) {
	handler := handleDebugEval(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/debug/eval", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when runner nil, got %d", w.Code)
	}
}

func TestHandleDebugEval_RunnerError(t *testing.T) {
	runner := &stubEvalRunner{err: errors.New("boom")}
	handler := handleDebugEval(runner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/debug/eval", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on runner error, got %d", w.Code)
	}
}

func TestHandleDebugEval_MethodNotAllowed(t *testing.T) {
	runner := &stubEvalRunner{report: stubEvalReport()}
	handler := handleDebugEval(runner)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/debug/eval", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
	if runner.calls != 0 {
		t.Errorf("runner should not be called on 405")
	}
}

func TestRouter_DebugEvalRoute(t *testing.T) {
	runner := &stubEvalRunner{report: stubEvalReport()}
	cfg := &config.AppConfig{CORSAllowedOrigin: "*"}
	deps := RouterDeps{EvalRunner: runner}
	router := NewRouter(cfg, deps)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/debug/eval", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("router debug eval expected 200 got %d %s", w.Code, w.Body.String())
	}
}
