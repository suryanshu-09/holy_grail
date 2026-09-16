package search

import (
	"context"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

// Metric defines the pgvector distance operator used for similarity.
type Metric string

const (
	MetricCosine      Metric = "cosine" // <=> cosine distance, similarity = 1 - distance
	MetricL2          Metric = "l2"     // <-> L2 distance
	MetricInnerProduct Metric = "ip"    // <#> inner product
)

// DefaultMetric is the pinned similarity metric for the search index.
// OpenAI text-embedding-3-small vectors are compared with cosine similarity.
// Changing this requires a migration to rebuild the pgvector index with the
// matching operator class (vector_cosine_ops, vector_l2_ops, vector_ip_ops).
const DefaultMetric Metric = MetricCosine

// Filter narrows the vector search results using structured metadata.
// All fields are optional; zero values mean "no filter".
// Filters are combined conjunctively (AND) with the vector similarity search.
type Filter struct {
	Subject      string   `json:"subject"`
	Year         *int     `json:"year"`           // exact year
	YearMin      *int     `json:"year_min"`       // year >= YearMin
	YearMax      *int     `json:"year_max"`       // year <= YearMax
	DocumentID   string   `json:"document_id"`
	Topic        string   `json:"topic"`       // topic name (case-insensitive)
	TopicID      string   `json:"topic_id"`    // topic ID (exact)
	QuestionType string   `json:"question_type"`
	Difficulty   string   `json:"difficulty"`
	Threshold    *float64 `json:"threshold"` // minimum similarity 0..1 (cosine)
	Limit        int      `json:"limit"`
	Offset       int      `json:"offset"`
}

// Result is a single search hit with its similarity score.
type Result struct {
	Question   questions.Question `json:"question"`
	Similarity float64            `json:"similarity"` // 0..1 for cosine (1 = identical)
	Distance   float64            `json:"distance"`   // raw pgvector distance
}

// Response is the top-level JSON envelope returned by the search endpoint.
type Response struct {
	Query   string   `json:"query"`
	Results []Result `json:"results"`
	Count   int      `json:"count"`
	Metric  Metric   `json:"metric"`
	Model   string   `json:"model"`
}

// Repository defines the persistence contract for vector search.
type Repository interface {
	Search(ctx context.Context, vector []float32, filter Filter) ([]Result, error)
}

// HybridFilter extends Filter with hybrid retrieval knobs.
// VectorWeight/KeywordWeight control weighted scoring. When both are zero,
// they default to DefaultVectorWeight/DefaultKeywordWeight. Set one to zero
// to use a single branch. When UseRRF is true, scoring uses reciprocal rank
// fusion (optionally weighted by the same weights, defaulting to 1 each when
// both are zero) with constant RRFK (default DefaultRRFK).
// VectorLimit/KeywordLimit optionally override per-branch candidate set sizes;
// zero means Limit+Offset. EnableRerank applies the service reranker when set.
// KeywordQuery optionally overrides the text used for the keyword branch;
// when empty, the main query is used for both branches.
type HybridFilter struct {
	Filter
	KeywordQuery  string  `json:"keyword_query"`
	VectorWeight  float64 `json:"vector_weight"`
	KeywordWeight float64 `json:"keyword_weight"`
	UseRRF        bool    `json:"use_rrf"`
	RRFK          int     `json:"rrf_k"`
	EnableRerank  bool    `json:"enable_rerank"`
	VectorLimit   int     `json:"vector_limit"`
	KeywordLimit  int     `json:"keyword_limit"`
}

// Hybrid scoring defaults.
const (
	DefaultVectorWeight  = 0.5
	DefaultKeywordWeight = 0.5
	DefaultRRFK          = 60
)

// HybridResult is a single fused hit with per-branch explanation.
type HybridResult struct {
	Question      questions.Question `json:"question"`
	VectorScore   float64            `json:"vector_score"`   // 0 when absent from vector set
	KeywordScore  float64            `json:"keyword_score"`  // 0 when absent from keyword set
	CombinedScore float64            `json:"combined_score"` // weighted sum or (weighted) RRF
	Sources       []string           `json:"sources"`        // subset of {"vector","keyword"}
	RerankBoost   float64            `json:"rerank_boost,omitempty"`
}

// DebugInfo explains how a hybrid response was produced.
type DebugInfo struct {
	Query             string  `json:"query"`
	VectorCandidates  int     `json:"vector_candidates"`
	KeywordCandidates int     `json:"keyword_candidates"`
	MergedCandidates  int     `json:"merged_candidates"`
	VectorWeight      float64 `json:"vector_weight"`
	KeywordWeight     float64 `json:"keyword_weight"`
	Scoring           string  `json:"scoring"` // "weighted" or "rrf"
	RerankEnabled     bool    `json:"rerank_enabled"`
}

// HybridResponse is the top-level envelope for hybrid retrieval.
type HybridResponse struct {
	Query   string         `json:"query"`
	Results []HybridResult `json:"results"`
	Count   int            `json:"count"`
	Metric  Metric         `json:"metric"`
	Model   string         `json:"model"`
	Debug   *DebugInfo     `json:"debug,omitempty"`
}

// QuestionColumns mirrors the questions table columns used in search projections.
const QuestionColumns = `q.id, q.document_id, q.question_number, q.question_text, q.page_number, q.start_page, q.end_page, q.start_offset, q.end_offset, q.confidence, q.question_type, q.options_json, q.extraction_notes_json, q.images_json, q.year, q.subject, q.difficulty, q.created_at, q.updated_at`

// SearchParams bundles the inputs for a search operation.
type SearchParams struct {
	Query  string
	Filter Filter
}

// Validation helpers.
const (
	defaultLimit = 10
	maxLimit     = 100
)

// Clamp normalizes limit/offset for Filter.
func clamp(f *Filter) {
	if f.Limit <= 0 {
		f.Limit = defaultLimit
	}
	if f.Limit > maxLimit {
		f.Limit = maxLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
}
