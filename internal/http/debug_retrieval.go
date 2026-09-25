package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/search"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

// RetrievalTopicLister resolves the topic name(s) for a debug retrieval hit.
// *topics.Service satisfies it; the interface keeps the handler testable.
type RetrievalTopicLister interface {
	ListTopicsForQuestion(ctx context.Context, questionID string) ([]topics.Topic, error)
}

// DebugRetrievalItem is the slim per-hit payload for the retrieval debugger:
// question id, relevance score, and primary topic name.
type DebugRetrievalItem struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
	Topic string  `json:"topic"`
}

// DebugRetrievalResponse is the JSON envelope for GET/POST /api/v1/debug/retrieval.
type DebugRetrievalResponse struct {
	Query   string               `json:"query"`
	Mode    string               `json:"mode"`
	Limit   int                  `json:"limit"`
	Count   int                  `json:"count"`
	Results []DebugRetrievalItem `json:"results"`
}

// debugRetrievalRequest is the JSON body for POST /api/v1/debug/retrieval.
type debugRetrievalRequest struct {
	Query string `json:"query"`
	Q     string `json:"q"`
	Limit *int   `json:"limit"`
	Mode  string `json:"mode"`
}

func (r *debugRetrievalRequest) effectiveQuery() string {
	if strings.TrimSpace(r.Query) != "" {
		return strings.TrimSpace(r.Query)
	}
	return strings.TrimSpace(r.Q)
}

// handleDebugRetrieval serves the Phase 23 single-query retrieval debugger.
//
//	GET  /api/v1/debug/retrieval?q=...&limit=...&mode=...
//	POST /api/v1/debug/retrieval  { "query": "...", "limit": ..., "mode": "..." }
//
// Query params accept both q and query; POST accepts both query and q.
// limit defaults to 10 and is clamped to 1..100. mode defaults to "vector"
// and must be one of vector|keyword|hybrid (validated by normalizeMode).
// Each hit returns {id, score, topic} where score is cosine similarity in
// vector mode or the fused combined score in keyword/hybrid modes, and topic
// is the first topic name for the question ("" when unknown/unconfigured).
//
// Responses:
//   - 200 with DebugRetrievalResponse on success.
//   - 400 for missing query, invalid limit, or invalid mode.
//   - 405 for non-GET/POST methods.
//   - 503 when no searcher is configured for the requested mode.
func handleDebugRetrieval(searcher Searcher, hybrid HybridSearcher, topicLister RetrievalTopicLister) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
			return
		}
		var query, modeRaw string
		limit := 10
		if r.Method == http.MethodGet {
			q := r.URL.Query()
			query = strings.TrimSpace(q.Get("q"))
			if query == "" {
				query = strings.TrimSpace(q.Get("query"))
			}
			modeRaw = q.Get("mode")
			if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
				n, err := strconv.Atoi(raw)
				if err != nil || n < 1 {
					httpx.Error(w, http.StatusBadRequest, "limit must be a positive integer")
					return
				}
				limit = n
			}
		} else {
			var req debugRetrievalRequest
			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil {
				httpx.Error(w, http.StatusBadRequest, "invalid request body")
				return
			}
			query = req.effectiveQuery()
			modeRaw = req.Mode
			if req.Limit != nil {
				if *req.Limit < 1 {
					httpx.Error(w, http.StatusBadRequest, "limit must be a positive integer")
					return
				}
				limit = *req.Limit
			}
		}
		if strings.TrimSpace(query) == "" {
			httpx.Error(w, http.StatusBadRequest, "query is required (q or query)")
			return
		}
		mode, err := normalizeMode(modeRaw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if limit > 100 {
			limit = 100
		}

		ctx := r.Context()
		items := make([]DebugRetrievalItem, 0, limit)
		if mode == "hybrid" || mode == "keyword" {
			if hybrid == nil {
				if searcher == nil {
					httpx.Error(w, http.StatusServiceUnavailable, "search not configured (missing embedding pipeline)")
					return
				}
				// Fall back to vector-only when hybrid is unavailable.
				mode = "vector"
			} else {
				hf := search.HybridFilter{}
				hf.Limit = limit
				if mode == "keyword" {
					hf.VectorWeight = 0
					hf.KeywordWeight = 1
				}
				resp, err := hybrid.Search(ctx, query, hf)
				if err != nil {
					msg := err.Error()
					if strings.Contains(msg, "query is required") || strings.Contains(msg, "dimension") || strings.Contains(msg, "weight") || strings.Contains(msg, "threshold") {
						httpx.Error(w, http.StatusBadRequest, msg)
						return
					}
					httpx.LogError("debug retrieval hybrid search failed", err)
					httpx.Error(w, http.StatusInternalServerError, "search failed")
					return
				}
				for _, hit := range resp.Results {
					items = append(items, DebugRetrievalItem{
						ID:    hit.Question.ID,
						Score: hit.CombinedScore,
						Topic: primaryTopicName(ctx, topicLister, hit.Question.ID),
					})
				}
				httpx.WriteJSON(w, http.StatusOK, DebugRetrievalResponse{
					Query:   query,
					Mode:    mode,
					Limit:   limit,
					Count:   len(items),
					Results: items,
				})
				return
			}
		}
		if searcher == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "search not configured (missing embedding pipeline)")
			return
		}
		resp, err := searcher.Search(ctx, query, search.Filter{Limit: limit})
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "query is required") || strings.Contains(msg, "dimension") || strings.Contains(msg, "threshold") {
				httpx.Error(w, http.StatusBadRequest, msg)
				return
			}
			httpx.LogError("debug retrieval search failed", err)
			httpx.Error(w, http.StatusInternalServerError, "search failed")
			return
		}
		for _, hit := range resp.Results {
			items = append(items, DebugRetrievalItem{
				ID:    hit.Question.ID,
				Score: hit.Similarity,
				Topic: primaryTopicName(ctx, topicLister, hit.Question.ID),
			})
		}
		httpx.WriteJSON(w, http.StatusOK, DebugRetrievalResponse{
			Query:   query,
			Mode:    mode,
			Limit:   limit,
			Count:   len(items),
			Results: items,
		})
	})
}

// primaryTopicName returns the first topic name for a question, or "" when
// the lister is nil, the question has no topics, or the lookup fails.
func primaryTopicName(ctx context.Context, lister RetrievalTopicLister, questionID string) string {
	if lister == nil || strings.TrimSpace(questionID) == "" {
		return ""
	}
	ts, err := lister.ListTopicsForQuestion(ctx, questionID)
	if err != nil || len(ts) == 0 {
		return ""
	}
	return ts[0].Name
}
