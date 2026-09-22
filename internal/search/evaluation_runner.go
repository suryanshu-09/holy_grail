package search

import (
	"context"
	"fmt"
	"strings"
)

// RunnerConfig tunes the strategy comparison runner.
// Limit is the Top-N retrieved per query per strategy (defaults to 10).
// Ks are the Top-K cutoffs recorded per query (defaults to DefaultEvalKs).
type RunnerConfig struct {
	Limit int   `json:"limit"`
	Ks    []int `json:"ks"`
}

// normalize fills defaults in place.
func (c *RunnerConfig) normalize() {
	if c.Limit <= 0 {
		c.Limit = defaultLimit
	}
	if c.Limit > maxLimit {
		c.Limit = maxLimit
	}
	if len(c.Ks) == 0 {
		c.Ks = append([]int(nil), DefaultEvalKs...)
	}
}

// StrategyRunner executes every Phase 17 retrieval strategy over a shared
// evaluation dataset so vector / metadata / keyword / hybrid /
// hybrid+reranker can be compared side by side.
//
// Strategy mapping (all branches share the same Limit):
//   - vector:          Service.Search with no metadata filter (pure semantic).
//   - metadata:        Service.Search constrained to Filter.Topic = query Category.
//   - keyword:         KeywordRepository.Search (Postgres FTS, no metadata filter).
//   - hybrid:          HybridService.Search with reranking disabled.
//   - hybrid+reranker: HybridService.Search with HybridFilter.EnableRerank = true.
//
// Metrics reuse EvaluateQuery / CompareStrategies from evaluation.go.
type StrategyRunner struct {
	vector  *Service
	keyword KeywordRepository
	hybrid  *HybridService
	cfg     RunnerConfig
}

// NewStrategyRunner creates a comparison runner. All three backends are
// required; cfg defaults (Limit 10, Ks 3/5/10) apply when unset.
func NewStrategyRunner(vector *Service, keyword KeywordRepository, hybrid *HybridService, cfg RunnerConfig) (*StrategyRunner, error) {
	if vector == nil {
		return nil, fmt.Errorf("evaluation runner: vector service is required")
	}
	if keyword == nil {
		return nil, fmt.Errorf("evaluation runner: keyword repository is required")
	}
	if hybrid == nil {
		return nil, fmt.Errorf("evaluation runner: hybrid service is required")
	}
	cfg.normalize()
	return &StrategyRunner{vector: vector, keyword: keyword, hybrid: hybrid, cfg: cfg}, nil
}

// Config returns the runner's normalized configuration.
func (r *StrategyRunner) Config() RunnerConfig { return r.cfg }

// RetrieveIDs runs a single strategy for one eval query and returns
// rank-ordered question IDs.
func (r *StrategyRunner) RetrieveIDs(ctx context.Context, strategy RetrievalStrategy, q EvalQuery) ([]string, error) {
	if strings.TrimSpace(q.Query) == "" {
		return nil, fmt.Errorf("evaluation runner: query is required")
	}
	limit := r.cfg.Limit
	switch strategy {
	case StrategyVector:
		resp, err := r.vector.Search(ctx, q.Query, Filter{Limit: limit})
		if err != nil {
			return nil, fmt.Errorf("evaluation runner: vector: %w", err)
		}
		return ResultIDs(resp.Results), nil
	case StrategyMetadata:
		resp, err := r.vector.Search(ctx, q.Query, Filter{Topic: q.Category, Limit: limit})
		if err != nil {
			return nil, fmt.Errorf("evaluation runner: metadata: %w", err)
		}
		return ResultIDs(resp.Results), nil
	case StrategyKeyword:
		results, err := r.keyword.Search(ctx, q.Query, Filter{Limit: limit})
		if err != nil {
			return nil, fmt.Errorf("evaluation runner: keyword: %w", err)
		}
		return ResultIDs(results), nil
	case StrategyHybrid:
		resp, err := r.hybrid.Search(ctx, q.Query, HybridFilter{Filter: Filter{Limit: limit}})
		if err != nil {
			return nil, fmt.Errorf("evaluation runner: hybrid: %w", err)
		}
		return HybridResultIDs(resp.Results), nil
	case StrategyHybridRerank:
		resp, err := r.hybrid.Search(ctx, q.Query, HybridFilter{Filter: Filter{Limit: limit}, EnableRerank: true})
		if err != nil {
			return nil, fmt.Errorf("evaluation runner: hybrid+reranker: %w", err)
		}
		return HybridResultIDs(resp.Results), nil
	default:
		return nil, fmt.Errorf("evaluation runner: unknown strategy %q", string(strategy))
	}
}

// ComparisonReport holds per-query metrics and aggregated results per strategy.
type ComparisonReport struct {
	Config   RunnerConfig                           `json:"config"`
	PerQuery map[RetrievalStrategy][]QueryMetrics   `json:"per_query"`
	Results  map[RetrievalStrategy]AggregateMetrics `json:"results"`
}

// BestByRecall returns the strategy with the highest average recall.
// Ties break on Top-K accuracy at k=5, then k=10. Returns "" when empty.
func (rep *ComparisonReport) BestByRecall() RetrievalStrategy {
	best := RetrievalStrategy("")
	bestRecall := -1.0
	bestTop5 := -1.0
	bestTop10 := -1.0
	for strategy, agg := range rep.Results {
		top5 := agg.TopKAccuracy[5]
		top10 := agg.TopKAccuracy[10]
		if agg.AvgRecall > bestRecall ||
			(agg.AvgRecall == bestRecall && top5 > bestTop5) ||
			(agg.AvgRecall == bestRecall && top5 == bestTop5 && top10 > bestTop10) {
			best = strategy
			bestRecall = agg.AvgRecall
			bestTop5 = top5
			bestTop10 = top10
		}
	}
	return best
}

// Run executes every strategy in AllStrategies over the dataset, scores each
// query with EvaluateQuery, and aggregates with CompareStrategies.
// It aborts on the first retrieval error so failures are never silently
// recorded as zeros.
func (r *StrategyRunner) Run(ctx context.Context, dataset []EvalQuery) (*ComparisonReport, error) {
	if len(dataset) == 0 {
		return nil, fmt.Errorf("evaluation runner: dataset is required")
	}
	perStrategy := make(map[RetrievalStrategy][]QueryMetrics, len(AllStrategies))
	for _, strategy := range AllStrategies {
		perQuery := make([]QueryMetrics, 0, len(dataset))
		for _, q := range dataset {
			ids, err := r.RetrieveIDs(ctx, strategy, q)
			if err != nil {
				return nil, err
			}
			perQuery = append(perQuery, EvaluateQuery(q.Query, strategy, ids, q.ExpectedIDs, r.cfg.Ks...))
		}
		perStrategy[strategy] = perQuery
	}
	return &ComparisonReport{
		Config:   r.cfg,
		PerQuery: perStrategy,
		Results:  CompareStrategies(perStrategy, r.cfg.Ks...),
	}, nil
}
