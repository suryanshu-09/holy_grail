package search

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/embeddings"
	"github.com/suryanshu-09/holy_grail/internal/questions"
)

// Reranker optionally re-scores fused hybrid results.
// It is disabled by default (HybridService runs with a nil reranker unless
// SetReranker is called and HybridFilter.EnableRerank is true).
type Reranker interface {
	Rerank(query string, results []HybridResult) []HybridResult
}

// ExactMatchReranker is a heuristic reranker that adds a fixed Boost to the
// CombinedScore of any result whose question text contains the raw query as a
// case-insensitive substring, then re-sorts by CombinedScore descending.
// The per-result boost is recorded in HybridResult.RerankBoost for debugging.
type ExactMatchReranker struct {
	Boost float64
}

// NewExactMatchReranker creates the heuristic exact-match reranker.
func NewExactMatchReranker(boost float64) *ExactMatchReranker {
	return &ExactMatchReranker{Boost: boost}
}

// Rerank implements Reranker.
func (r *ExactMatchReranker) Rerank(query string, results []HybridResult) []HybridResult {
	if r == nil || r.Boost == 0 {
		return results
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return results
	}
	out := make([]HybridResult, len(results))
	copy(out, results)
	for i := range out {
		text := ""
		if out[i].Question.QuestionText != nil {
			text = strings.ToLower(*out[i].Question.QuestionText)
		}
		if strings.Contains(text, needle) {
			out[i].CombinedScore += r.Boost
			out[i].RerankBoost += r.Boost
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		return out[a].CombinedScore > out[b].CombinedScore
	})
	return out
}

// HybridService fuses vector (semantic) and keyword (FTS) candidate sets.
// Metadata filtering (HybridFilter.Filter) is pushed down to both branches;
// fusion deduplicates by question ID and scores with either a weighted sum
// or reciprocal rank fusion (RRF). An optional Reranker applies a heuristic
// exact-match boost when HybridFilter.EnableRerank is set.
type HybridService struct {
	vectorRepo  Repository
	keywordRepo KeywordRepository
	embedder    embeddings.Embedder
	metric      Metric
	reranker    Reranker // nil = disabled by default
}

// NewHybridService creates a hybrid retrieval service. All three dependencies
// are required; the reranker stays disabled until SetReranker is called.
func NewHybridService(vectorRepo Repository, keywordRepo KeywordRepository, embedder embeddings.Embedder) (*HybridService, error) {
	return NewHybridServiceWithMetric(vectorRepo, keywordRepo, embedder, DefaultMetric)
}

// NewHybridServiceWithMetric allows overriding the similarity metric (tests).
func NewHybridServiceWithMetric(vectorRepo Repository, keywordRepo KeywordRepository, embedder embeddings.Embedder, metric Metric) (*HybridService, error) {
	if vectorRepo == nil {
		return nil, fmt.Errorf("hybrid search: vector repository is required")
	}
	if keywordRepo == nil {
		return nil, fmt.Errorf("hybrid search: keyword repository is required")
	}
	if embedder == nil {
		return nil, fmt.Errorf("hybrid search: embedder is required")
	}
	if embedder.Dimensions() != embeddings.DefaultDimensions {
		return nil, fmt.Errorf("hybrid search: dimension %d is incompatible with vector(%d)", embedder.Dimensions(), embeddings.DefaultDimensions)
	}
	if strings.TrimSpace(embedder.Model()) == "" {
		return nil, fmt.Errorf("hybrid search: embedder model is required")
	}
	if metric == "" {
		metric = DefaultMetric
	}
	return &HybridService{vectorRepo: vectorRepo, keywordRepo: keywordRepo, embedder: embedder, metric: metric}, nil
}

// SetReranker installs the optional reranker (nil disables it).
func (h *HybridService) SetReranker(r Reranker) { h.reranker = r }

// Metric returns the configured vector similarity metric.
func (h *HybridService) Metric() Metric { return h.metric }

// Model returns the embedder model used for the vector branch.
func (h *HybridService) Model() string { return h.embedder.Model() }

type mergedCandidate struct {
	question     questions.Question
	vectorScore  float64
	keywordScore float64
	vectorRank   int // 1-indexed, 0 = absent
	keywordRank  int // 1-indexed, 0 = absent
	inVector     bool
	inKeyword    bool
}

// Search runs the hybrid retrieval pipeline: fetch vector + keyword candidate
// sets (with metadata filtering), dedupe by question ID, fuse with weighted or
// RRF scoring, optionally rerank, then paginate. Existing Service.Search is
// left intact; this is a separate entry point.
func (h *HybridService) Search(ctx context.Context, query string, filter HybridFilter) (HybridResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return HybridResponse{}, fmt.Errorf("hybrid search: query is required")
	}
	if err := normalizeHybridFilter(&filter); err != nil {
		return HybridResponse{}, err
	}

	needVector := filter.UseRRF || filter.VectorWeight > 0
	needKeyword := filter.UseRRF || filter.KeywordWeight > 0
	if !needVector && !needKeyword {
		return HybridResponse{}, fmt.Errorf("hybrid search: at least one of vector_weight or keyword_weight must be positive")
	}

	// Per-branch candidate sizes default to Limit+Offset so the fused,
// paginated window is fully populated after dedupe.
	candidateSize := filter.Limit + filter.Offset
	vectorLimit := filter.VectorLimit
	if vectorLimit <= 0 {
		vectorLimit = candidateSize
	}
	keywordLimit := filter.KeywordLimit
	if keywordLimit <= 0 {
		keywordLimit = candidateSize
	}

	branchFilter := filter.Filter // metadata + threshold shared by both branches

	var vectorResults []Result
	var keywordResults []Result

	if needVector {
		vectors, err := h.embedder.Embed(ctx, []string{query})
		if err != nil {
			return HybridResponse{}, fmt.Errorf("hybrid search: embed query: %w", err)
		}
		if len(vectors) != 1 {
			return HybridResponse{}, fmt.Errorf("hybrid search: embedder returned %d vectors, want 1", len(vectors))
		}
		if len(vectors[0]) != embeddings.DefaultDimensions {
			return HybridResponse{}, fmt.Errorf("hybrid search: vector dimension %d, want %d", len(vectors[0]), embeddings.DefaultDimensions)
		}
		vf := branchFilter
		vf.Limit = vectorLimit
		vf.Offset = 0
		vectorResults, err = h.vectorRepo.Search(ctx, vectors[0], vf)
		if err != nil {
			return HybridResponse{}, err
		}
	}

	if needKeyword {
		kf := branchFilter
		kf.Limit = keywordLimit
		kf.Offset = 0
		keywordQuery := query
		if strings.TrimSpace(filter.KeywordQuery) != "" {
			keywordQuery = strings.TrimSpace(filter.KeywordQuery)
		}
		var err error
		keywordResults, err = h.keywordRepo.Search(ctx, keywordQuery, kf)
		if err != nil {
			return HybridResponse{}, err
		}
	}

	// Merge + dedupe by question ID.
	merged := make(map[string]*mergedCandidate)
	order := make([]string, 0)
	for i, r := range vectorResults {
		id := r.Question.ID
		m, ok := merged[id]
		if !ok {
			m = &mergedCandidate{question: r.Question}
			merged[id] = m
			order = append(order, id)
		}
		m.vectorScore = r.Similarity
		m.vectorRank = i + 1
		m.inVector = true
	}
	for i, r := range keywordResults {
		id := r.Question.ID
		m, ok := merged[id]
		if !ok {
			m = &mergedCandidate{question: r.Question}
			merged[id] = m
			order = append(order, id)
		}
		m.keywordScore = r.Similarity
		m.keywordRank = i + 1
		m.inKeyword = true
	}

	scoring := "weighted"
	if filter.UseRRF {
		scoring = "rrf"
	}
	k := filter.RRFK

	results := make([]HybridResult, 0, len(merged))
	for _, id := range order {
		m := merged[id]
		var combined float64
		if filter.UseRRF {
			if m.inVector {
				combined += filter.VectorWeight * (1.0 / float64(k+m.vectorRank))
			}
			if m.inKeyword {
				combined += filter.KeywordWeight * (1.0 / float64(k+m.keywordRank))
			}
		} else {
			combined = filter.VectorWeight*m.vectorScore + filter.KeywordWeight*m.keywordScore
		}
		// Threshold on the fused score applies only to weighted mode, where
		// scores stay in similarity units. RRF scores are rank-based (tiny
		// magnitudes), so branch-level thresholds already apply there.
		if filter.Threshold != nil && !filter.UseRRF {
			if combined < *filter.Threshold {
				continue
			}
		}
		sources := make([]string, 0, 2)
		if m.inVector {
			sources = append(sources, "vector")
		}
		if m.inKeyword {
			sources = append(sources, "keyword")
		}
		results = append(results, HybridResult{
			Question:      m.question,
			VectorScore:   m.vectorScore,
			KeywordScore:  m.keywordScore,
			CombinedScore: combined,
			Sources:       sources,
		})
	}

	sort.SliceStable(results, func(a, b int) bool {
		return results[a].CombinedScore > results[b].CombinedScore
	})

	rerankEnabled := filter.EnableRerank && h.reranker != nil
	if rerankEnabled {
		results = h.reranker.Rerank(query, results)
	}

	mergedCount := len(results)
	// Paginate the fused ranking.
	start := filter.Offset
	if start > len(results) {
		start = len(results)
	}
	end := start + filter.Limit
	if end > len(results) {
		end = len(results)
	}
	results = results[start:end]
	if results == nil {
		results = []HybridResult{}
	}

	return HybridResponse{
		Query:   query,
		Results: results,
		Count:   len(results),
		Metric:  h.metric,
		Model:   h.embedder.Model(),
		Debug: &DebugInfo{
			Query:             query,
			VectorCandidates:  len(vectorResults),
			KeywordCandidates: len(keywordResults),
			MergedCandidates:  mergedCount,
			VectorWeight:      filter.VectorWeight,
			KeywordWeight:     filter.KeywordWeight,
			Scoring:           scoring,
			RerankEnabled:     rerankEnabled,
		},
	}, nil
}

// normalizeHybridFilter clamps pagination, validates thresholds/weights, and
// fills scoring defaults in place.
func normalizeHybridFilter(f *HybridFilter) error {
	clamp(&f.Filter)
	if f.Threshold != nil {
		if *f.Threshold < 0 || *f.Threshold > 1 {
			return fmt.Errorf("hybrid search: threshold must be between 0 and 1")
		}
	}
	if f.VectorWeight < 0 || f.KeywordWeight < 0 {
		return fmt.Errorf("hybrid search: vector_weight and keyword_weight must be >= 0")
	}
	if f.UseRRF {
		if f.VectorWeight == 0 && f.KeywordWeight == 0 {
			f.VectorWeight = 1
			f.KeywordWeight = 1
		}
		if f.RRFK <= 0 {
			f.RRFK = DefaultRRFK
		}
	} else {
		if f.VectorWeight == 0 && f.KeywordWeight == 0 {
			f.VectorWeight = DefaultVectorWeight
			f.KeywordWeight = DefaultKeywordWeight
		}
		// Normalize to unit sum so combined scores stay in similarity units.
		total := f.VectorWeight + f.KeywordWeight
		if total != 1 && total > 0 {
			f.VectorWeight /= total
			f.KeywordWeight /= total
		}
	}
	if f.VectorLimit < 0 || f.KeywordLimit < 0 {
		return fmt.Errorf("hybrid search: vector_limit and keyword_limit must be >= 0")
	}
	if f.VectorLimit > maxLimit*5 {
		f.VectorLimit = maxLimit * 5
	}
	if f.KeywordLimit > maxLimit*5 {
		f.KeywordLimit = maxLimit * 5
	}
	return nil
}
