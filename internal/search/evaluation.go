package search

// Retrieval evaluation metrics for Phase 17 (PLAN4.md).
//
// Recall: did we retrieve the questions that should have been retrieved?
// Precision: how many retrieved questions were actually relevant?
// Top-K accuracy: was a relevant question within Top 3 / 5 / 10?
//
// All functions are pure (no DB access) so they are directly unit-testable.
// Retrieved lists are assumed to be rank-ordered (best first); passing k > 0
// truncates to the first k entries before scoring.

import (
	"strings"
)

// RetrievalStrategy identifies a retrieval branch under evaluation.
type RetrievalStrategy string

const (
	StrategyVector       RetrievalStrategy = "vector"
	StrategyMetadata     RetrievalStrategy = "metadata"
	StrategyKeyword      RetrievalStrategy = "keyword"
	StrategyHybrid       RetrievalStrategy = "hybrid"
	StrategyHybridRerank RetrievalStrategy = "hybrid+reranker"
)

// AllStrategies lists every strategy compared in Phase 17 evaluation.
var AllStrategies = []RetrievalStrategy{
	StrategyVector,
	StrategyMetadata,
	StrategyKeyword,
	StrategyHybrid,
	StrategyHybridRerank,
}

// DefaultEvalKs are the Top-K cutoffs from PLAN4.md Phase 17.
var DefaultEvalKs = []int{3, 5, 10}

// QueryMetrics holds per-query retrieval scores.
type QueryMetrics struct {
	Query                string            `json:"query"`
	Strategy             RetrievalStrategy `json:"strategy"`
	NumRelevant          int               `json:"num_relevant"`
	NumRetrieved         int               `json:"num_retrieved"`
	NumRelevantRetrieved int               `json:"num_relevant_retrieved"`
	Recall               float64           `json:"recall"`
	Precision            float64           `json:"precision"`
	Hits                 map[int]bool      `json:"hits"` // k -> a relevant doc is within top-k
}

// AggregateMetrics holds mean scores across an evaluation dataset.
type AggregateMetrics struct {
	Strategy     RetrievalStrategy `json:"strategy"`
	NumQueries   int               `json:"num_queries"`
	AvgRecall    float64           `json:"avg_recall"`
	AvgPrecision float64           `json:"avg_precision"`
	TopKAccuracy map[int]float64   `json:"top_k_accuracy"` // k -> fraction of queries with a hit
}

// truncate returns the first k IDs when k > 0, otherwise the full list.
func truncate(ids []string, k int) []string {
	if k <= 0 || k >= len(ids) {
		return ids
	}
	return ids[:k]
}

// toSet builds a normalized (trimmed) ID set for O(1) relevance lookups.
func toSet(ids []string) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		set[id] = struct{}{}
	}
	return set
}

// countHits counts how many retrieved IDs (optionally truncated to top-k)
// appear in the relevant set.
func countHits(retrieved []string, relevantSet map[string]struct{}, k int) int {
	hits := 0
	for _, id := range truncate(retrieved, k) {
		if _, ok := relevantSet[strings.TrimSpace(id)]; ok {
			hits++
		}
	}
	return hits
}

// RecallAtK is the fraction of relevant docs retrieved within the top-k.
// Returns 0 when there are no relevant docs (nothing to recall).
func RecallAtK(retrievedIDs, relevantIDs []string, k int) float64 {
	relevant := toSet(relevantIDs)
	if len(relevant) == 0 {
		return 0
	}
	return float64(countHits(retrievedIDs, relevant, k)) / float64(len(relevant))
}

// PrecisionAtK is the fraction of top-k retrieved docs that are relevant.
// Returns 0 when nothing was retrieved within the cutoff.
func PrecisionAtK(retrievedIDs, relevantIDs []string, k int) float64 {
	cut := truncate(retrievedIDs, k)
	if len(cut) == 0 {
		return 0
	}
	return float64(countHits(cut, toSet(relevantIDs), 0)) / float64(len(cut))
}

// HitAtK reports whether at least one relevant doc appears within the top-k.
func HitAtK(retrievedIDs, relevantIDs []string, k int) bool {
	relevant := toSet(relevantIDs)
	if len(relevant) == 0 {
		return false
	}
	return countHits(retrievedIDs, relevant, k) > 0
}

// EvaluateQuery scores a single query against its known relevant IDs.
// ks defaults to DefaultEvalKs (3, 5, 10) when empty. Recall and Precision
// are computed over the full retrieved list; Hits records Top-K accuracy
// per cutoff.
func EvaluateQuery(query string, strategy RetrievalStrategy, retrievedIDs, relevantIDs []string, ks ...int) QueryMetrics {
	if len(ks) == 0 {
		ks = DefaultEvalKs
	}
	relevant := toSet(relevantIDs)
	hits := make(map[int]bool, len(ks))
	for _, k := range ks {
		hits[k] = HitAtK(retrievedIDs, relevantIDs, k)
	}
	matched := countHits(retrievedIDs, relevant, 0)
	return QueryMetrics{
		Query:                query,
		Strategy:             strategy,
		NumRelevant:          len(relevant),
		NumRetrieved:         len(retrievedIDs),
		NumRelevantRetrieved: matched,
		Recall:               RecallAtK(retrievedIDs, relevantIDs, 0),
		Precision:            PrecisionAtK(retrievedIDs, relevantIDs, 0),
		Hits:                 hits,
	}
}

// AggregateMetricsFor averages per-query scores into dataset-level metrics.
// TopKAccuracy[k] is the fraction of queries with a hit within top-k.
func AggregateMetricsFor(strategy RetrievalStrategy, perQuery []QueryMetrics, ks ...int) AggregateMetrics {
	if len(ks) == 0 {
		ks = DefaultEvalKs
	}
	agg := AggregateMetrics{
		Strategy:     strategy,
		NumQueries:   len(perQuery),
		TopKAccuracy: make(map[int]float64, len(ks)),
	}
	if len(perQuery) == 0 {
		for _, k := range ks {
			agg.TopKAccuracy[k] = 0
		}
		return agg
	}
	for _, qm := range perQuery {
		agg.AvgRecall += qm.Recall
		agg.AvgPrecision += qm.Precision
	}
	agg.AvgRecall /= float64(len(perQuery))
	agg.AvgPrecision /= float64(len(perQuery))
	for _, k := range ks {
		hits := 0
		for _, qm := range perQuery {
			if qm.Hits[k] {
				hits++
			}
		}
		agg.TopKAccuracy[k] = float64(hits) / float64(len(perQuery))
	}
	return agg
}

// CompareStrategies aggregates per-query metrics for each strategy so
// vector / metadata / keyword / hybrid / hybrid+reranker can be compared
// side by side. Strategies with no queries get zero-valued aggregates.
func CompareStrategies(perStrategy map[RetrievalStrategy][]QueryMetrics, ks ...int) map[RetrievalStrategy]AggregateMetrics {
	if len(ks) == 0 {
		ks = DefaultEvalKs
	}
	out := make(map[RetrievalStrategy]AggregateMetrics, len(perStrategy))
	for strategy, perQuery := range perStrategy {
		out[strategy] = AggregateMetricsFor(strategy, perQuery, ks...)
	}
	return out
}

// ResultIDs extracts question IDs from rank-ordered vector/keyword results.
func ResultIDs(results []Result) []string {
	ids := make([]string, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.Question.ID)
	}
	return ids
}

// HybridResultIDs extracts question IDs from rank-ordered hybrid results.
func HybridResultIDs(results []HybridResult) []string {
	ids := make([]string, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.Question.ID)
	}
	return ids
}
