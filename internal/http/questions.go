package http

import (
	"errors"
	"net/http"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/questions"
)

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
