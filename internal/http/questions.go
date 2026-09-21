package http

import (
	"errors"
	"net/http"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/questions"
)

// handleQuestionByID returns a single question by ID.
// GET /api/v1/questions/{id}
// Returns the full Question record including start_page, end_page,
// question_text, images_json, and document_id for source traceability.
func handleQuestionByID(svc *questions.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}

		id := r.PathValue("id")
		if id == "" {
			httpx.Error(w, http.StatusBadRequest, "question id is required")
			return
		}
		if svc == nil {
			httpx.Error(w, http.StatusInternalServerError, "questions service not configured")
			return
		}

		q, err := svc.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				httpx.Error(w, http.StatusNotFound, "question not found")
				return
			}
			httpx.LogError("question get by id failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}

		httpx.WriteJSON(w, http.StatusOK, q)
	})
}

// handleQuestions lists questions.
func handleQuestions(svc *questions.Service) http.Handler {
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

		f := questions.Filter{Limit: pg.Limit, Offset: pg.Offset}
		q := r.URL.Query()
		f.DocumentID = q.Get("document_id")
		f.Subject = q.Get("subject")
		y, convErr := questions.ParseYear(q.Get("year"))
		if convErr != nil {
			httpx.Error(w, http.StatusBadRequest, "year must be an integer")
			return
		}
		f.Year = y

		qs, err := svc.List(r.Context(), f)
		if err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				httpx.Error(w, http.StatusNotFound, "question not found")
				return
			}
			httpx.LogError("questions list failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}

		httpx.WriteJSON(w, http.StatusOK, qs)
	})
}
