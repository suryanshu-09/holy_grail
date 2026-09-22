package search

import (
	"math"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

func approxEq(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestRecallAtK(t *testing.T) {
	tests := []struct {
		name      string
		retrieved []string
		relevant  []string
		k         int
		want      float64
	}{
		{"perfect full", []string{"a", "b", "c"}, []string{"a", "b", "c"}, 0, 1.0},
		{"partial full", []string{"a", "x", "y"}, []string{"a", "b", "c"}, 0, 1.0 / 3.0},
		{"none", []string{"x", "y"}, []string{"a", "b"}, 0, 0.0},
		{"empty relevant", []string{"a"}, []string{}, 0, 0.0},
		{"empty retrieved", []string{}, []string{"a", "b"}, 0, 0.0},
		{"both empty", []string{}, []string{}, 0, 0.0},
		{"top1 hit one of two", []string{"a", "x"}, []string{"a", "b"}, 1, 0.5},
		{"top1 miss", []string{"x", "a"}, []string{"a", "b"}, 1, 0.0},
		{"top2 partial", []string{"a", "x", "b"}, []string{"a", "b"}, 2, 0.5},
		{"k larger than list", []string{"a"}, []string{"a", "b"}, 10, 0.5},
		{"negative k means full", []string{"a", "b"}, []string{"a", "b"}, -1, 1.0},
		{"whitespace normalized", []string{"  a ", "b"}, []string{"a", "b"}, 0, 1.0},
		{"duplicates in relevant counted once", []string{"a"}, []string{"a", "a"}, 0, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RecallAtK(tt.retrieved, tt.relevant, tt.k); !approxEq(got, tt.want) {
				t.Errorf("RecallAtK(%v, %v, %d) = %v want %v", tt.retrieved, tt.relevant, tt.k, got, tt.want)
			}
		})
	}
}

func TestPrecisionAtK(t *testing.T) {
	tests := []struct {
		name      string
		retrieved []string
		relevant  []string
		k         int
		want      float64
	}{
		{"perfect full", []string{"a", "b"}, []string{"a", "b"}, 0, 1.0},
		{"half relevant", []string{"a", "x"}, []string{"a", "b"}, 0, 0.5},
		{"none relevant", []string{"x", "y"}, []string{"a"}, 0, 0.0},
		{"empty retrieved", []string{}, []string{"a"}, 0, 0.0},
		{"empty retrieved no cutoff", []string{}, []string{"a"}, 5, 0.0},
		{"top1 hit", []string{"a", "x"}, []string{"a"}, 1, 1.0},
		{"top1 miss", []string{"x", "a"}, []string{"a"}, 1, 0.0},
		{"top2 one of two", []string{"a", "x", "b"}, []string{"a", "b"}, 2, 0.5},
		{"k larger than list", []string{"a", "x"}, []string{"a"}, 10, 0.5},
		{"whitespace normalized", []string{"  a "}, []string{"a"}, 0, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PrecisionAtK(tt.retrieved, tt.relevant, tt.k); !approxEq(got, tt.want) {
				t.Errorf("PrecisionAtK(%v, %v, %d) = %v want %v", tt.retrieved, tt.relevant, tt.k, got, tt.want)
			}
		})
	}
}

func TestHitAtK(t *testing.T) {
	tests := []struct {
		name      string
		retrieved []string
		relevant  []string
		k         int
		want      bool
	}{
		{"hit top1", []string{"a", "x"}, []string{"a"}, 1, true},
		{"miss top1 hit top2", []string{"x", "a"}, []string{"a"}, 1, false},
		{"hit top2", []string{"x", "a"}, []string{"a"}, 2, true},
		{"no hit", []string{"x", "y"}, []string{"a"}, 2, false},
		{"empty relevant", []string{"a"}, []string{}, 3, false},
		{"empty retrieved", []string{}, []string{"a"}, 3, false},
		{"full list hit", []string{"x", "y", "a"}, []string{"a"}, 0, true},
		{"whitespace normalized", []string{"  a "}, []string{"a"}, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HitAtK(tt.retrieved, tt.relevant, tt.k); got != tt.want {
				t.Errorf("HitAtK(%v, %v, %d) = %v want %v", tt.retrieved, tt.relevant, tt.k, got, tt.want)
			}
		})
	}
}

func TestEvaluateQuery(t *testing.T) {
	qm := EvaluateQuery("deadlock questions", StrategyVector,
		[]string{"q-deadlock-001", "q-deadlock-002", "x-1"},
		[]string{"q-deadlock-001", "q-deadlock-002", "q-deadlock-003"})
	if qm.Query != "deadlock questions" {
		t.Errorf("query = %q", qm.Query)
	}
	if qm.Strategy != StrategyVector {
		t.Errorf("strategy = %q", qm.Strategy)
	}
	if qm.NumRelevant != 3 {
		t.Errorf("num_relevant = %d want 3", qm.NumRelevant)
	}
	if qm.NumRetrieved != 3 {
		t.Errorf("num_retrieved = %d want 3", qm.NumRetrieved)
	}
	if qm.NumRelevantRetrieved != 2 {
		t.Errorf("num_relevant_retrieved = %d want 2", qm.NumRelevantRetrieved)
	}
	if !approxEq(qm.Recall, 2.0/3.0) {
		t.Errorf("recall = %v want %v", qm.Recall, 2.0/3.0)
	}
	if !approxEq(qm.Precision, 2.0/3.0) {
		t.Errorf("precision = %v want %v", qm.Precision, 2.0/3.0)
	}
	// Default cutoffs 3/5/10 must be present.
	for _, k := range DefaultEvalKs {
		if _, ok := qm.Hits[k]; !ok {
			t.Errorf("missing hit for k=%d", k)
		}
	}
	if !qm.Hits[3] || !qm.Hits[5] || !qm.Hits[10] {
		t.Errorf("hits should all be true, got %v", qm.Hits)
	}

	// Custom cutoff: relevant only at rank 3.
	qm2 := EvaluateQuery("paging questions", StrategyKeyword,
		[]string{"x", "y", "q-paging-001"}, []string{"q-paging-001"}, 1, 3)
	if qm2.Hits[1] {
		t.Errorf("top-1 should miss")
	}
	if !qm2.Hits[3] {
		t.Errorf("top-3 should hit")
	}
	if len(qm2.Hits) != 2 {
		t.Errorf("custom ks should record exactly 2 cutoffs, got %v", qm2.Hits)
	}
}

func TestAggregateMetricsFor(t *testing.T) {
	perQuery := []QueryMetrics{
		{Recall: 1.0, Precision: 0.5, Hits: map[int]bool{3: true, 5: true, 10: true}},
		{Recall: 0.5, Precision: 1.0, Hits: map[int]bool{3: false, 5: true, 10: true}},
	}
	agg := AggregateMetricsFor(StrategyHybrid, perQuery)
	if agg.Strategy != StrategyHybrid {
		t.Errorf("strategy = %q", agg.Strategy)
	}
	if agg.NumQueries != 2 {
		t.Errorf("num_queries = %d want 2", agg.NumQueries)
	}
	if !approxEq(agg.AvgRecall, 0.75) {
		t.Errorf("avg_recall = %v want 0.75", agg.AvgRecall)
	}
	if !approxEq(agg.AvgPrecision, 0.75) {
		t.Errorf("avg_precision = %v want 0.75", agg.AvgPrecision)
	}
	if !approxEq(agg.TopKAccuracy[3], 0.5) {
		t.Errorf("top3 = %v want 0.5", agg.TopKAccuracy[3])
	}
	if !approxEq(agg.TopKAccuracy[5], 1.0) {
		t.Errorf("top5 = %v want 1.0", agg.TopKAccuracy[5])
	}
	if !approxEq(agg.TopKAccuracy[10], 1.0) {
		t.Errorf("top10 = %v want 1.0", agg.TopKAccuracy[10])
	}

	// Empty input yields zeros without NaN.
	empty := AggregateMetricsFor(StrategyVector, nil)
	if empty.NumQueries != 0 {
		t.Errorf("empty num_queries = %d want 0", empty.NumQueries)
	}
	if empty.AvgRecall != 0 || empty.AvgPrecision != 0 {
		t.Errorf("empty averages should be 0, got %v/%v", empty.AvgRecall, empty.AvgPrecision)
	}
	for _, k := range DefaultEvalKs {
		if empty.TopKAccuracy[k] != 0 {
			t.Errorf("empty top-%d = %v want 0", k, empty.TopKAccuracy[k])
		}
	}
}

func TestCompareStrategies(t *testing.T) {
	perStrategy := map[RetrievalStrategy][]QueryMetrics{
		StrategyVector: {
			{Recall: 1.0, Precision: 1.0, Hits: map[int]bool{3: true, 5: true, 10: true}},
		},
		StrategyKeyword: {
			{Recall: 0.0, Precision: 0.0, Hits: map[int]bool{3: false, 5: false, 10: false}},
		},
		StrategyHybrid: {},
	}
	got := CompareStrategies(perStrategy)
	if len(got) != 3 {
		t.Fatalf("expected 3 strategies, got %d", len(got))
	}
	if !approxEq(got[StrategyVector].AvgRecall, 1.0) {
		t.Errorf("vector recall = %v want 1.0", got[StrategyVector].AvgRecall)
	}
	if !approxEq(got[StrategyKeyword].AvgRecall, 0.0) {
		t.Errorf("keyword recall = %v want 0.0", got[StrategyKeyword].AvgRecall)
	}
	// Strategy with no queries gets zero-valued aggregates, not NaN.
	if got[StrategyHybrid].NumQueries != 0 || got[StrategyHybrid].AvgRecall != 0 {
		t.Errorf("empty hybrid aggregate = %+v, want zeros", got[StrategyHybrid])
	}
	if got[StrategyHybrid].TopKAccuracy[3] != 0 {
		t.Errorf("empty hybrid top-3 should be 0")
	}
}

func TestStrategyConstants(t *testing.T) {
	if len(AllStrategies) != 5 {
		t.Fatalf("AllStrategies has %d entries, want 5", len(AllStrategies))
	}
	want := map[RetrievalStrategy]bool{
		StrategyVector: false, StrategyMetadata: false, StrategyKeyword: false,
		StrategyHybrid: false, StrategyHybridRerank: false,
	}
	for _, s := range AllStrategies {
		if _, ok := want[s]; !ok {
			t.Errorf("unexpected strategy %q", s)
		}
		want[s] = true
	}
	for s, seen := range want {
		if !seen {
			t.Errorf("missing strategy %q", s)
		}
	}
	if string(StrategyHybridRerank) != "hybrid+reranker" {
		t.Errorf("hybrid+reranker id = %q", StrategyHybridRerank)
	}
	if len(DefaultEvalKs) != 3 || DefaultEvalKs[0] != 3 || DefaultEvalKs[1] != 5 || DefaultEvalKs[2] != 10 {
		t.Errorf("DefaultEvalKs = %v want [3 5 10]", DefaultEvalKs)
	}
}

func TestEvaluationDataset(t *testing.T) {
	if len(EvaluationDataset) != 60 {
		t.Fatalf("dataset has %d queries, want 60", len(EvaluationDataset))
	}
	cats := EvaluationCategories()
	if len(cats) != 12 {
		t.Fatalf("dataset has %d categories, want 12", len(cats))
	}
	perCat := map[string]int{}
	seenQueries := map[string]bool{}
	for i, q := range EvaluationDataset {
		if strings.TrimSpace(q.Query) == "" {
			t.Errorf("query %d has empty text", i)
		}
		if seenQueries[q.Query] {
			t.Errorf("duplicate query %q", q.Query)
		}
		seenQueries[q.Query] = true
		if len(q.ExpectedIDs) == 0 {
			t.Errorf("query %q has no expected IDs", q.Query)
		}
		for _, id := range q.ExpectedIDs {
			if strings.TrimSpace(id) == "" {
				t.Errorf("query %q has blank expected ID", q.Query)
			}
		}
		if strings.TrimSpace(q.Category) == "" {
			t.Errorf("query %q has empty category", q.Query)
		}
		perCat[q.Category]++
	}
	for _, c := range cats {
		if perCat[c] != 5 {
			t.Errorf("category %q has %d queries, want 5", c, perCat[c])
		}
	}
	// PLAN4.md Phase 17 example queries must be covered.
	examples := []string{
		"deadlock questions",
		"questions about circular wait",
		"paging questions",
		"virtual memory",
		"Banker's algorithm",
		"CPU scheduling algorithms",
	}
	texts := map[string]bool{}
	for _, q := range EvaluationDataset {
		texts[q.Query] = true
	}
	for _, ex := range examples {
		if !texts[ex] {
			t.Errorf("dataset missing PLAN4 example query %q", ex)
		}
	}
}

// mockRetriever simulates a retrieval strategy over the evaluation dataset:
// perfect returns all expected IDs first, partial buries only the first
// expected ID at rank 4 (after three distractors), empty returns only
// distractors.
func mockRetriever(mode string, q EvalQuery) []string {
	switch mode {
	case "perfect":
		out := make([]string, 0, len(q.ExpectedIDs)+1)
		out = append(out, q.ExpectedIDs...)
		out = append(out, "q-distractor-001")
		return out
	case "partial":
		if len(q.ExpectedIDs) == 0 {
			return []string{"q-distractor-001"}
		}
		return []string{"q-distractor-001", "q-distractor-002", "q-distractor-003", q.ExpectedIDs[0]}
	default: // "empty"
		return []string{"q-distractor-001", "q-distractor-002"}
	}
}

func TestStrategyComparisonUsingDataset(t *testing.T) {
	modes := map[RetrievalStrategy]string{
		StrategyVector:       "perfect",
		StrategyMetadata:     "partial",
		StrategyKeyword:      "partial",
		StrategyHybrid:       "perfect",
		StrategyHybridRerank: "perfect",
	}
	perStrategy := make(map[RetrievalStrategy][]QueryMetrics, len(modes))
	for strategy, mode := range modes {
		for _, q := range EvaluationDataset {
			retrieved := mockRetriever(mode, q)
			perStrategy[strategy] = append(perStrategy[strategy],
				EvaluateQuery(q.Query, strategy, retrieved, q.ExpectedIDs))
		}
	}
	compared := CompareStrategies(perStrategy)

	// Every strategy is evaluated over the full 60-query dataset.
	for _, s := range AllStrategies {
		agg, ok := compared[s]
		if !ok {
			t.Fatalf("missing aggregate for strategy %q", s)
		}
		if agg.NumQueries != len(EvaluationDataset) {
			t.Errorf("%s num_queries = %d want %d", s, agg.NumQueries, len(EvaluationDataset))
		}
	}

	perfect := compared[StrategyVector]
	partial := compared[StrategyMetadata]
	if !(perfect.AvgRecall > partial.AvgRecall) {
		t.Errorf("perfect recall %v should exceed partial %v", perfect.AvgRecall, partial.AvgRecall)
	}
	if !(perfect.AvgPrecision > partial.AvgPrecision) {
		t.Errorf("perfect precision %v should exceed partial %v", perfect.AvgPrecision, partial.AvgPrecision)
	}
	if !approxEq(perfect.AvgRecall, 1.0) {
		t.Errorf("perfect recall = %v want 1.0", perfect.AvgRecall)
	}
	if !(perfect.TopKAccuracy[3] > partial.TopKAccuracy[3]) {
		t.Errorf("perfect top-3 %v should exceed partial %v",
			perfect.TopKAccuracy[3], partial.TopKAccuracy[3])
	}
	// Partial mock buries the single hit at rank 4: top-3 misses everything,
	// top-5 hits everything.
	if !approxEq(partial.TopKAccuracy[3], 0.0) {
		t.Errorf("partial top-3 = %v want 0.0", partial.TopKAccuracy[3])
	}
	if !approxEq(partial.TopKAccuracy[5], 1.0) {
		t.Errorf("partial top-5 = %v want 1.0", partial.TopKAccuracy[5])
	}
	// Rank check with explicit cutoffs: the buried hit misses top-3.
	sample := EvaluationDataset[0]
	if HitAtK(mockRetriever("partial", sample), sample.ExpectedIDs, 3) {
		t.Errorf("partial retriever should miss top-3 for %q", sample.Query)
	}
	if !HitAtK(mockRetriever("partial", sample), sample.ExpectedIDs, 5) {
		t.Errorf("partial retriever should hit top-5 for %q", sample.Query)
	}
	// Scores stay in [0,1].
	for s, agg := range compared {
		if agg.AvgRecall < 0 || agg.AvgRecall > 1 {
			t.Errorf("%s avg_recall %v out of range", s, agg.AvgRecall)
		}
		if agg.AvgPrecision < 0 || agg.AvgPrecision > 1 {
			t.Errorf("%s avg_precision %v out of range", s, agg.AvgPrecision)
		}
		for k, acc := range agg.TopKAccuracy {
			if acc < 0 || acc > 1 {
				t.Errorf("%s top-%d accuracy %v out of range", s, k, acc)
			}
		}
	}
}

func TestResultIDsHelpers(t *testing.T) {
	text := "deadlock"
	results := []Result{
		{Question: questions.Question{ID: "q1", QuestionText: &text}},
		{Question: questions.Question{ID: "q2", QuestionText: &text}},
	}
	ids := ResultIDs(results)
	if len(ids) != 2 || ids[0] != "q1" || ids[1] != "q2" {
		t.Errorf("ResultIDs = %v want [q1 q2]", ids)
	}
	if got := ResultIDs(nil); len(got) != 0 {
		t.Errorf("ResultIDs(nil) = %v want empty", got)
	}

	hybrid := []HybridResult{
		{Question: questions.Question{ID: "h1"}},
		{Question: questions.Question{ID: "h2"}},
	}
	hids := HybridResultIDs(hybrid)
	if len(hids) != 2 || hids[0] != "h1" || hids[1] != "h2" {
		t.Errorf("HybridResultIDs = %v want [h1 h2]", hids)
	}
	if got := HybridResultIDs(nil); len(got) != 0 {
		t.Errorf("HybridResultIDs(nil) = %v want empty", got)
	}

	// IDs feed directly into metrics without reordering.
	if !HitAtK(ids, []string{"q2"}, 2) {
		t.Errorf("expected hit for q2 within top-2 of %v", ids)
	}
	if HitAtK(ids, []string{"q2"}, 1) {
		t.Errorf("q2 should miss top-1 of %v", ids)
	}
}
