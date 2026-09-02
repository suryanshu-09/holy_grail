package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

// handleTopics lists topics.
// Supports ?include_counts=1 to return topics with question counts via ListWithCounts.
// Accepted truthy values for include_counts are "1" and "true" (case-insensitive).
func handleTopics(svc *topics.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}

		pg, err := httpx.ParsePagination(r)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}

		f := topics.Filter{Limit: pg.Limit, Offset: pg.Offset, Subject: r.URL.Query().Get("subject")}

		includeCounts := r.URL.Query().Get("include_counts")
		if includeCounts == "1" || strings.EqualFold(includeCounts, "true") {
			tcs, cErr := svc.ListWithCounts(r.Context(), f)
			if cErr != nil {
				if errors.Is(cErr, apperr.ErrNotFound) {
					httpx.Error(w, http.StatusNotFound, "topic not found")
					return
				}
				httpx.LogError("topics list with counts failed", cErr)
				httpx.Error(w, http.StatusInternalServerError, "internal server error")
				return
			}
			if tcs == nil {
				tcs = []topics.TopicWithCount{}
			}
			httpx.WriteJSON(w, http.StatusOK, tcs)
			return
		}

		ts, err := svc.List(r.Context(), f)
		if err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				httpx.Error(w, http.StatusNotFound, "topic not found")
				return
			}
			httpx.LogError("topics list failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if ts == nil {
			ts = []topics.Topic{}
		}

		httpx.WriteJSON(w, http.StatusOK, ts)
	})
}

// mergeTopicsRequest is the payload for POST /api/v1/topics/merge.
// Supports both snake_case and camelCase for ergonomics.
type mergeTopicsRequest struct {
	TargetID  string   `json:"target_id"`
	TargetIDAlt string `json:"targetId"`
	SourceIDs []string `json:"source_ids"`
	SourceIDsAlt []string `json:"sourceIds"`
}

func (r *mergeTopicsRequest) effectiveTarget() string {
	if r.TargetID != "" {
		return r.TargetID
	}
	return r.TargetIDAlt
}

func (r *mergeTopicsRequest) effectiveSources() []string {
	if len(r.SourceIDs) > 0 {
		return r.SourceIDs
	}
	return r.SourceIDsAlt
}

// handleMergeTopics merges duplicate topics into a target topic.
func handleMergeTopics(svc *topics.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var req mergeTopicsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
		targetID := req.effectiveTarget()
		sourceIDs := req.effectiveSources()
		if targetID == "" {
			httpx.Error(w, http.StatusBadRequest, "target_id is required")
			return
		}
		if len(sourceIDs) == 0 {
			httpx.Error(w, http.StatusBadRequest, "source_ids is required")
			return
		}
		if err := svc.MergeDuplicateTopics(r.Context(), targetID, sourceIDs); err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				httpx.Error(w, http.StatusNotFound, err.Error())
				return
			}
			// Validation errors (empty, same target/source) are 400.
			if isValidationError(err) {
				httpx.Error(w, http.StatusBadRequest, err.Error())
				return
			}
			httpx.LogError("topics merge failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "merged"})
	})
}

func isValidationError(err error) bool {
	msg := err.Error()
	return contains(msg, "is required") || contains(msg, "must differ") || contains(msg, "source ids")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

// correctQuestionTopicsRequest is payload for PATCH /api/v1/questions/{id}/topics
type correctQuestionTopicsRequest struct {
	TopicIDs    []string `json:"topic_ids"`
	TopicIDsAlt []string `json:"topicIds"`
}

func (r *correctQuestionTopicsRequest) effectiveTopicIDs() []string {
	if len(r.TopicIDs) > 0 {
		return r.TopicIDs
	}
	return r.TopicIDsAlt
}

// handleGetQuestionTopics lists topics for a question.
// GET /api/v1/questions/{id}/topics
func handleGetQuestionTopics(svc *topics.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		questionID := r.PathValue("id")
		if questionID == "" {
			httpx.Error(w, http.StatusBadRequest, "question id is required")
			return
		}
		if svc == nil {
			httpx.Error(w, http.StatusInternalServerError, "topics service not configured")
			return
		}
		ts, err := svc.ListTopicsForQuestion(r.Context(), questionID)
		if err != nil {
			httpx.LogError("list topics for question failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if ts == nil {
			ts = []topics.Topic{}
		}
		httpx.WriteJSON(w, http.StatusOK, ts)
	})
}

// handleCorrectQuestionTopics replaces topics for a question (manual correction).
func handleCorrectQuestionTopics(svc *topics.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			methodNotAllowed(w, http.MethodPatch)
			return
		}
		questionID := r.PathValue("id")
		if questionID == "" {
			httpx.Error(w, http.StatusBadRequest, "question id is required")
			return
		}
		var req correctQuestionTopicsRequest
		// Allow empty body to mean clear topics; but if body present, decode.
		if r.Body != nil {
			dec := json.NewDecoder(r.Body)
			// Allow empty body: if EOF, treat as empty request (clear)
			if err := dec.Decode(&req); err != nil && err.Error() != "EOF" {
				httpx.Error(w, http.StatusBadRequest, "invalid request body")
				return
			}
		}
		topicIDs := req.effectiveTopicIDs()
		// Normalize nil to empty slice (clear all topics) – valid manual correction.
		if topicIDs == nil {
			topicIDs = []string{}
		}
		updated, err := svc.CorrectQuestionTopics(r.Context(), questionID, topicIDs)
		if err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				httpx.Error(w, http.StatusNotFound, err.Error())
				return
			}
			if isValidationError(err) || contains(err.Error(), "not found") {
				// If topic not found, treat as 404 if ErrNotFound else 400
				if contains(err.Error(), "not found") {
					httpx.Error(w, http.StatusNotFound, err.Error())
					return
				}
				httpx.Error(w, http.StatusBadRequest, err.Error())
				return
			}
			httpx.LogError("correct question topics failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, updated)
	})
}
