package http

import (
	"context"
	"net/http"

	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/search"
)

// EvalRunner is the contract for Phase 17 retrieval strategy comparison.
// *search.StrategyRunner satisfies it; the interface keeps handlers testable
// with fakes and mirrors the Searcher/HybridSearcher pattern in search.go.
type EvalRunner interface {
	Run(ctx context.Context, dataset []search.EvalQuery) (*search.ComparisonReport, error)
}

// debugEvalResponse flattens the comparison report with the winning strategy
// so debug clients get metrics plus a one-field verdict in a single payload.
type debugEvalResponse struct {
	Config       search.RunnerConfig                                  `json:"config"`
	PerQuery     map[search.RetrievalStrategy][]search.QueryMetrics   `json:"per_query"`
	Results      map[search.RetrievalStrategy]search.AggregateMetrics `json:"results"`
	BestByRecall search.RetrievalStrategy                             `json:"best_by_recall"`
	NumQueries   int                                                  `json:"num_queries"`
}

// handleDebugEval triggers a full Phase 17 retrieval evaluation over the
// bundled dataset (search.EvaluationDataset) and returns metrics JSON.
//
//	GET  /api/v1/debug/eval  (also accepts POST with an empty/ignored body)
//
// Responses:
//   - 200 with debugEvalResponse on success.
//   - 503 when no evaluation runner is configured (e.g. search disabled).
//   - 405 for non-GET/POST methods, 500 when the run itself fails.
//
// The runner carries its own RunnerConfig (Limit/Ks baked at construction in
// cmd/api/main.go), so the endpoint takes no parameters: every strategy runs
// over the same dataset and limit for a fair side-by-side comparison.
func handleDebugEval(runner EvalRunner) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
			return
		}
		if runner == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "evaluation not configured (search pipeline unavailable)")
			return
		}
		report, err := runner.Run(r.Context(), search.EvaluationDataset)
		if err != nil {
			httpx.LogError("debug eval failed", err)
			httpx.Error(w, http.StatusInternalServerError, "evaluation failed")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, debugEvalResponse{
			Config:       report.Config,
			PerQuery:     report.PerQuery,
			Results:      report.Results,
			BestByRecall: report.BestByRecall(),
			NumQueries:   len(search.EvaluationDataset),
		})
	})
}
