package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/search"
)

// Searcher is the contract for semantic search.
type Searcher interface {
	Search(ctx context.Context, query string, filter search.Filter) (search.Response, error)
}

// HybridSearcher is the contract for hybrid (vector + keyword) retrieval.
type HybridSearcher interface {
	Search(ctx context.Context, query string, filter search.HybridFilter) (search.HybridResponse, error)
}

// searchRequest is the JSON body for POST /api/v1/search.
type searchRequest struct {
	Query        string   `json:"query"`
	Q            string   `json:"q"`
	Subject      string   `json:"subject"`
	Year         *int     `json:"year"`
	YearMin      *int     `json:"year_min"`
	YearMax      *int     `json:"year_max"`
	YearMinAlt   *int     `json:"yearMin"`
	YearMaxAlt   *int     `json:"yearMax"`
	DocumentID   string   `json:"document_id"`
	DocumentIDAlt string  `json:"documentId"`
	Topic        string   `json:"topic"`
	TopicID      string   `json:"topic_id"`
	TopicIDAlt   string   `json:"topicId"`
	QuestionType string   `json:"question_type"`
	QuestionTypeAlt string `json:"questionType"`
	Difficulty   string   `json:"difficulty"`
	Threshold    *float64 `json:"threshold"`
	Limit        *int     `json:"limit"`
	Offset       *int     `json:"offset"`
	// Hybrid retrieval knobs.
	Mode              string   `json:"mode"`
	Keyword           string   `json:"keyword"`
	VectorWeight      *float64 `json:"vector_weight"`
	VectorWeightAlt   *float64 `json:"vectorWeight"`
	KeywordWeight     *float64 `json:"keyword_weight"`
	KeywordWeightAlt  *float64 `json:"keywordWeight"`
	Rerank            *bool    `json:"rerank"`
	Debug             *bool    `json:"debug"`
	IncludeDebug      *bool    `json:"include_debug"`
	IncludeDebugAlt   *bool    `json:"includeDebug"`
	// Phase 27 advanced-retrieval flags (all optional, defaults off for
	// backward compat). They map onto search.AdvancedFilter: rewrite expands
	// synonyms, multi_query fans out + RRF-fuses, parent_child groups hits by
	// document, contextual prefixes filter context into the query.
	Rewrite          *bool `json:"rewrite"`
	MultiQuery       *bool `json:"multi_query"`
	MultiQueryAlt    *bool `json:"multiQuery"`
	ParentChild      *bool `json:"parent_child"`
	ParentChildAlt   *bool `json:"parentChild"`
	Contextual       *bool `json:"contextual"`
}

func (r *searchRequest) effectiveQuery() string {
	if strings.TrimSpace(r.Query) != "" {
		return strings.TrimSpace(r.Query)
	}
	return strings.TrimSpace(r.Q)
}

func (r *searchRequest) effectiveDocumentID() string {
	if strings.TrimSpace(r.DocumentID) != "" {
		return strings.TrimSpace(r.DocumentID)
	}
	return strings.TrimSpace(r.DocumentIDAlt)
}

func (r *searchRequest) effectiveTopicID() string {
	if strings.TrimSpace(r.TopicID) != "" {
		return strings.TrimSpace(r.TopicID)
	}
	return strings.TrimSpace(r.TopicIDAlt)
}

func (r *searchRequest) effectiveQuestionType() string {
	if strings.TrimSpace(r.QuestionType) != "" {
		return strings.TrimSpace(r.QuestionType)
	}
	return strings.TrimSpace(r.QuestionTypeAlt)
}

func (r *searchRequest) effectiveYearMin() *int {
	if r.YearMin != nil {
		return r.YearMin
	}
	return r.YearMinAlt
}

func (r *searchRequest) effectiveYearMax() *int {
	if r.YearMax != nil {
		return r.YearMax
	}
	return r.YearMaxAlt
}

func (r *searchRequest) effectiveVectorWeight() *float64 {
	if r.VectorWeight != nil {
		return r.VectorWeight
	}
	return r.VectorWeightAlt
}

func (r *searchRequest) effectiveKeywordWeight() *float64 {
	if r.KeywordWeight != nil {
		return r.KeywordWeight
	}
	return r.KeywordWeightAlt
}

func (r *searchRequest) effectiveRerank() bool {
	return r.Rerank != nil && *r.Rerank
}

func (r *searchRequest) effectiveDebug() bool {
	if r.Debug != nil && *r.Debug {
		return true
	}
	if r.IncludeDebug != nil && *r.IncludeDebug {
		return true
	}
	if r.IncludeDebugAlt != nil && *r.IncludeDebugAlt {
		return true
	}
	return false
}

// effectiveAdvanced folds the Phase 27 body flags into a search.AdvancedFilter.
// All flags default off (nil = false) so legacy requests are unaffected.
func (r *searchRequest) effectiveAdvanced() search.AdvancedFilter {
	return search.AdvancedFilter{
		Rewrite:     r.Rewrite != nil && *r.Rewrite,
		MultiQuery:  (r.MultiQuery != nil && *r.MultiQuery) || (r.MultiQueryAlt != nil && *r.MultiQueryAlt),
		ParentChild: (r.ParentChild != nil && *r.ParentChild) || (r.ParentChildAlt != nil && *r.ParentChildAlt),
		Contextual:  r.Contextual != nil && *r.Contextual,
	}
}

// normalizeMode validates the retrieval mode. Empty defaults to "vector"
// to preserve the Phase 11 vector search API.
func normalizeMode(raw string) (string, error) {
	m := strings.ToLower(strings.TrimSpace(raw))
	if m == "" {
		return "vector", nil
	}
	switch m {
	case "vector", "keyword", "hybrid":
		return m, nil
	default:
		return "", errInvalidMode()
	}
}

func errInvalidMode() error { return errModeInvalid }

var errModeInvalid = errString("mode must be one of vector|keyword|hybrid")

type errString string

func (e errString) Error() string { return string(e) }

// parseOptionalFloat parses an optional float query param (snake_case + camelCase).
func parseOptionalFloat(q map[string][]string, snake, camel string, field string, w http.ResponseWriter) (*float64, bool) {
	raw := ""
	if vals, ok := q[snake]; ok && len(vals) > 0 && vals[0] != "" {
		raw = vals[0]
	} else if vals, ok := q[camel]; ok && len(vals) > 0 && vals[0] != "" {
		raw = vals[0]
	}
	if raw == "" {
		return nil, true
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, field+" must be a number")
		return nil, false
	}
	return &f, true
}

// parseOptionalBool parses an optional bool query param. Absent means nil/false.
func parseOptionalBool(q map[string][]string, keys []string, field string, w http.ResponseWriter) (*bool, bool) {
	raw := ""
	for _, k := range keys {
		if vals, ok := q[k]; ok && len(vals) > 0 && vals[0] != "" {
			raw = vals[0]
			break
		}
	}
	if raw == "" {
		return nil, true
	}
	b, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, field+" must be a boolean")
		return nil, false
	}
	return &b, true
}

// handleSearch handles both GET and POST for vector/keyword/hybrid search.
// GET  /api/v1/search?q=...&mode=hybrid&keyword=...&vector_weight=...&rerank=...&debug=...
// POST /api/v1/search  { "query": "...", "mode": "hybrid", ... }
// The hybrid searcher is optional: when nil (or when no keyword/embedder is
// available), hybrid/keyword modes fall back to vector-only search to preserve
// the Phase 11 API. Variadic to keep single-arg callers compiling.
func handleSearch(searcher Searcher, hybridOpt ...HybridSearcher) http.Handler {
	var hybrid HybridSearcher
	if len(hybridOpt) > 0 {
		hybrid = hybridOpt[0]
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
			return
		}

		var query string
		var filter search.Filter
		var mode string
		var keyword string
		var vectorWeight *float64
		var keywordWeight *float64
		var rerank bool
		var includeDebug bool
		// Phase 27 advanced retrieval (defaults off for backward compat).
		var adv search.AdvancedFilter

		if r.Method == http.MethodGet {
			q := r.URL.Query()
			query = strings.TrimSpace(q.Get("q"))
			if query == "" {
				query = strings.TrimSpace(q.Get("query"))
			}
			modeRaw := q.Get("mode")
			m, err := normalizeMode(modeRaw)
			if err != nil {
				httpx.Error(w, http.StatusBadRequest, err.Error())
				return
			}
			mode = m
			keyword = strings.TrimSpace(q.Get("keyword"))
			vals := map[string][]string(q)
			vw, ok := parseOptionalFloat(vals, "vector_weight", "vectorWeight", "vector_weight", w)
			if !ok {
				return
			}
			vectorWeight = vw
			kw, ok := parseOptionalFloat(vals, "keyword_weight", "keywordWeight", "keyword_weight", w)
			if !ok {
				return
			}
			keywordWeight = kw
			rb, ok := parseOptionalBool(vals, []string{"rerank"}, "rerank", w)
			if !ok {
				return
			}
			if rb != nil {
				rerank = *rb
			}
			db, ok := parseOptionalBool(vals, []string{"debug", "include_debug", "includeDebug"}, "debug", w)
			if !ok {
				return
			}
			if db != nil {
				includeDebug = *db
			}
			// Phase 27 advanced-retrieval flags (all optional, default false).
			rwb, ok := parseOptionalBool(vals, []string{"rewrite"}, "rewrite", w)
			if !ok {
				return
			}
			if rwb != nil {
				adv.Rewrite = *rwb
			}
			mqb, ok := parseOptionalBool(vals, []string{"multi_query", "multiQuery"}, "multi_query", w)
			if !ok {
				return
			}
			if mqb != nil {
				adv.MultiQuery = *mqb
			}
			pcb, ok := parseOptionalBool(vals, []string{"parent_child", "parentChild"}, "parent_child", w)
			if !ok {
				return
			}
			if pcb != nil {
				adv.ParentChild = *pcb
			}
			cxb, ok := parseOptionalBool(vals, []string{"contextual"}, "contextual", w)
			if !ok {
				return
			}
			if cxb != nil {
				adv.Contextual = *cxb
			}
			filter.Subject = q.Get("subject")
			filter.Topic = q.Get("topic")
			filter.TopicID = q.Get("topic_id")
			if filter.TopicID == "" {
				filter.TopicID = q.Get("topicId")
			}
			filter.DocumentID = q.Get("document_id")
			if filter.DocumentID == "" {
				filter.DocumentID = q.Get("documentId")
			}
			filter.QuestionType = q.Get("question_type")
			if filter.QuestionType == "" {
				filter.QuestionType = q.Get("questionType")
			}
			filter.Difficulty = q.Get("difficulty")

			if raw := q.Get("year"); raw != "" {
				y, err := strconv.Atoi(raw)
				if err != nil {
					httpx.Error(w, http.StatusBadRequest, "year must be an integer")
					return
				}
				filter.Year = &y
			}
			if raw := q.Get("year_min"); raw != "" {
				y, err := strconv.Atoi(raw)
				if err != nil {
					httpx.Error(w, http.StatusBadRequest, "year_min must be an integer")
					return
				}
				filter.YearMin = &y
			} else if raw := q.Get("yearMin"); raw != "" {
				y, err := strconv.Atoi(raw)
				if err != nil {
					httpx.Error(w, http.StatusBadRequest, "yearMin must be an integer")
					return
				}
				filter.YearMin = &y
			}
			if raw := q.Get("year_max"); raw != "" {
				y, err := strconv.Atoi(raw)
				if err != nil {
					httpx.Error(w, http.StatusBadRequest, "year_max must be an integer")
					return
				}
				filter.YearMax = &y
			} else if raw := q.Get("yearMax"); raw != "" {
				y, err := strconv.Atoi(raw)
				if err != nil {
					httpx.Error(w, http.StatusBadRequest, "yearMax must be an integer")
					return
				}
				filter.YearMax = &y
			}
			if raw := q.Get("threshold"); raw != "" {
				t, err := strconv.ParseFloat(raw, 64)
				if err != nil {
					httpx.Error(w, http.StatusBadRequest, "threshold must be a number between 0 and 1")
					return
				}
				if t < 0 || t > 1 {
					httpx.Error(w, http.StatusBadRequest, "threshold must be between 0 and 1")
					return
				}
				filter.Threshold = &t
			}
			if raw := q.Get("limit"); raw != "" {
				n, err := strconv.Atoi(raw)
				if err != nil || n < 1 {
					httpx.Error(w, http.StatusBadRequest, "limit must be a positive integer")
					return
				}
				filter.Limit = n
			} else {
				filter.Limit = 10
			}
			if raw := q.Get("offset"); raw != "" {
				n, err := strconv.Atoi(raw)
				if err != nil || n < 0 {
					httpx.Error(w, http.StatusBadRequest, "offset must be a non-negative integer")
					return
				}
				filter.Offset = n
			}
		} else {
			// POST: parse JSON body. Allow empty body? No, query required.
			var req searchRequest
			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil {
				httpx.Error(w, http.StatusBadRequest, "invalid request body")
				return
			}
			query = req.effectiveQuery()
			m, err := normalizeMode(req.Mode)
			if err != nil {
				httpx.Error(w, http.StatusBadRequest, err.Error())
				return
			}
			mode = m
			keyword = strings.TrimSpace(req.Keyword)
			vectorWeight = req.effectiveVectorWeight()
			keywordWeight = req.effectiveKeywordWeight()
			rerank = req.effectiveRerank()
			includeDebug = req.effectiveDebug()
			adv = req.effectiveAdvanced()
			filter.Subject = req.Subject
			filter.Topic = req.Topic
			filter.TopicID = req.effectiveTopicID()
			filter.DocumentID = req.effectiveDocumentID()
			filter.QuestionType = req.effectiveQuestionType()
			filter.Difficulty = req.Difficulty
			filter.Year = req.Year
			filter.YearMin = req.effectiveYearMin()
			filter.YearMax = req.effectiveYearMax()
			filter.Threshold = req.Threshold
			if req.Limit != nil {
				filter.Limit = *req.Limit
			} else {
				filter.Limit = 10
			}
			if req.Offset != nil {
				filter.Offset = *req.Offset
			}
			// Validate limit/offset if provided
			if req.Limit != nil && *req.Limit < 1 {
				httpx.Error(w, http.StatusBadRequest, "limit must be a positive integer")
				return
			}
			if req.Offset != nil && *req.Offset < 0 {
				httpx.Error(w, http.StatusBadRequest, "offset must be a non-negative integer")
				return
			}
			if req.Threshold != nil && (*req.Threshold < 0 || *req.Threshold > 1) {
				httpx.Error(w, http.StatusBadRequest, "threshold must be between 0 and 1")
				return
			}
			if vectorWeight != nil && *vectorWeight < 0 {
				httpx.Error(w, http.StatusBadRequest, "vector_weight must be >= 0")
				return
			}
			if keywordWeight != nil && *keywordWeight < 0 {
				httpx.Error(w, http.StatusBadRequest, "keyword_weight must be >= 0")
				return
			}
		}

		// Validate weights for GET as well (POST validated above).
		if r.Method == http.MethodGet {
			if vectorWeight != nil && *vectorWeight < 0 {
				httpx.Error(w, http.StatusBadRequest, "vector_weight must be >= 0")
				return
			}
			if keywordWeight != nil && *keywordWeight < 0 {
				httpx.Error(w, http.StatusBadRequest, "keyword_weight must be >= 0")
				return
			}
		}

		if strings.TrimSpace(query) == "" {
			httpx.Error(w, http.StatusBadRequest, "query is required (q or query)")
			return
		}

		// Phase 27 advanced retrieval preprocessing (defaults off).
		// Rewrite expands synonyms deterministically; contextual prefixes the
		// active metadata filters into the query. Both preserve the legacy
		// query when disabled.
		if adv.Rewrite {
			if rw := search.RewriteQuery(query); strings.TrimSpace(rw.Expanded) != "" {
				query = rw.Expanded
			}
		}
		if adv.Contextual {
			query = search.BuildContextualQuery(query, filter.Subject, filter.Topic, filter.DocumentID)
		}

		// Dispatch: vector is the default (Phase 11 preserved). Hybrid/keyword
		// modes require the hybrid searcher; fall back to vector-only when it
		// is unavailable (no keyword/embedder configured).
		useHybrid := (mode == "hybrid" || mode == "keyword") && hybrid != nil
		if useHybrid {
			hf := search.HybridFilter{Filter: filter}
			if vectorWeight != nil {
				hf.VectorWeight = *vectorWeight
			}
			if keywordWeight != nil {
				hf.KeywordWeight = *keywordWeight
			}
			if mode == "keyword" && vectorWeight == nil && keywordWeight == nil {
				// Keyword-only: disable the vector branch.
				hf.VectorWeight = 0
				hf.KeywordWeight = 1
			}
			hf.EnableRerank = rerank
			if keyword != "" {
				hf.KeywordQuery = keyword
			}
			// Multi-query fans out over deterministic variants + RRF-fuses.
			if adv.MultiQuery {
				variants := search.BuildQueryVariants(query, []string{filter.Topic, filter.Subject})
				if len(variants) == 0 {
					variants = []string{query}
				}
				lists := make([][]search.HybridResult, 0, len(variants))
				for _, v := range variants {
					hresp, err := hybrid.Search(r.Context(), v, hf)
					if err != nil {
						msg := err.Error()
						if strings.Contains(msg, "threshold") || strings.Contains(msg, "query is required") || strings.Contains(msg, "dimension") || strings.Contains(msg, "weight") {
							httpx.Error(w, http.StatusBadRequest, msg)
							return
						}
						httpx.LogError("hybrid search failed", err)
						httpx.Error(w, http.StatusInternalServerError, "search failed")
						return
					}
					lists = append(lists, hresp.Results)
				}
				fused := search.FuseMultiQuery(lists, search.DefaultRRFK)
				if adv.ParentChild {
					httpx.WriteJSON(w, http.StatusOK, search.GroupByDocument(query, fused))
					return
				}
				// Respect the requested limit on the fused list.
				limit := filter.Limit
				if limit <= 0 {
					limit = 10
				}
				if len(fused) > limit {
					fused = fused[:limit]
				}
				httpx.WriteJSON(w, http.StatusOK, search.HybridResponse{
					Query:   query,
					Results: fused,
					Count:   len(fused),
				})
				return
			}
			resp, err := hybrid.Search(r.Context(), query, hf)
			if err != nil {
				msg := err.Error()
				if strings.Contains(msg, "threshold") || strings.Contains(msg, "query is required") || strings.Contains(msg, "dimension") || strings.Contains(msg, "weight") {
					httpx.Error(w, http.StatusBadRequest, msg)
					return
				}
				httpx.LogError("hybrid search failed", err)
				httpx.Error(w, http.StatusInternalServerError, "search failed")
				return
			}
			if !includeDebug {
				resp.Debug = nil
			}
			if adv.ParentChild {
				httpx.WriteJSON(w, http.StatusOK, search.GroupByDocument(query, resp.Results))
				return
			}
			httpx.WriteJSON(w, http.StatusOK, resp)
			return
		}

		if searcher == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "search not configured (missing embedding pipeline)")
			return
		}

		// Multi-query over the vector searcher: fan out, merge, dedupe by
		// question ID (first-seen wins), cap at the requested limit.
		if adv.MultiQuery {
			variants := search.BuildQueryVariants(query, []string{filter.Topic, filter.Subject})
			if len(variants) == 0 {
				variants = []string{query}
			}
			seen := make(map[string]struct{})
			merged := make([]search.Result, 0)
			for _, v := range variants {
				sresp, err := searcher.Search(r.Context(), v, filter)
				if err != nil {
					msg := err.Error()
					if strings.Contains(msg, "threshold") || strings.Contains(msg, "query is required") || strings.Contains(msg, "dimension") {
						httpx.Error(w, http.StatusBadRequest, msg)
						return
					}
					httpx.LogError("search failed", err)
					httpx.Error(w, http.StatusInternalServerError, "search failed")
					return
				}
				for _, hit := range sresp.Results {
					if _, dup := seen[hit.Question.ID]; dup {
						continue
					}
					seen[hit.Question.ID] = struct{}{}
					merged = append(merged, hit)
				}
			}
			limit := filter.Limit
			if limit <= 0 {
				limit = 10
			}
			if len(merged) > limit {
				merged = merged[:limit]
			}
			if adv.ParentChild {
				hyb := make([]search.HybridResult, 0, len(merged))
				for _, hit := range merged {
					hyb = append(hyb, search.HybridResult{
						Question:      hit.Question,
						VectorScore:   hit.Similarity,
						CombinedScore: hit.Similarity,
						Sources:       []string{"vector"},
					})
				}
				httpx.WriteJSON(w, http.StatusOK, search.GroupByDocument(query, hyb))
				return
			}
			httpx.WriteJSON(w, http.StatusOK, search.Response{
				Query:   query,
				Results: merged,
				Count:   len(merged),
			})
			return
		}

		resp, err := searcher.Search(r.Context(), query, filter)
		if err != nil {
			// Map validation errors to 400
			msg := err.Error()
			if strings.Contains(msg, "threshold") || strings.Contains(msg, "query is required") || strings.Contains(msg, "dimension") {
				httpx.Error(w, http.StatusBadRequest, msg)
				return
			}
			httpx.LogError("search failed", err)
			httpx.Error(w, http.StatusInternalServerError, "search failed")
			return
		}
		if adv.ParentChild {
			hyb := make([]search.HybridResult, 0, len(resp.Results))
			for _, hit := range resp.Results {
				hyb = append(hyb, search.HybridResult{
					Question:      hit.Question,
					VectorScore:   hit.Similarity,
					CombinedScore: hit.Similarity,
					Sources:       []string{"vector"},
				})
			}
			httpx.WriteJSON(w, http.StatusOK, search.GroupByDocument(query, hyb))
			return
		}
		httpx.WriteJSON(w, http.StatusOK, resp)
	})
}

// handleHybrid is a thin alias for the hybrid mode of handleSearch, kept for
// callers that prefer an explicit hybrid entry point.
func handleHybrid(searcher Searcher, hybrid HybridSearcher) http.Handler {
	return handleSearch(searcher, hybrid)
}
