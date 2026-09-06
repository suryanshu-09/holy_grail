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

// handleSearch handles both GET and POST for vector search.
// GET  /api/v1/search?q=...&subject=...&topic=...&year_min=...&threshold=...&limit=...
// POST /api/v1/search  { "query": "...", "topic": "...", "threshold": 0.7, "limit": 10 }
func handleSearch(searcher Searcher) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
			return
		}
		if searcher == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "search not configured (missing embedding pipeline)")
			return
		}

		var query string
		var filter search.Filter

		if r.Method == http.MethodGet {
			q := r.URL.Query()
			query = strings.TrimSpace(q.Get("q"))
			if query == "" {
				query = strings.TrimSpace(q.Get("query"))
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
		}

		if strings.TrimSpace(query) == "" {
			httpx.Error(w, http.StatusBadRequest, "query is required (q or query)")
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
		httpx.WriteJSON(w, http.StatusOK, resp)
	})
}
