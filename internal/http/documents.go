package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
)

// handleDocuments lists documents.
func handleDocuments(svc *documents.Service) http.Handler {
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

		f := documents.Filter{Limit: pg.Limit, Offset: pg.Offset}
		q := r.URL.Query()
		f.Subject = q.Get("subject")
		f.Status = q.Get("status")
		if raw := q.Get("year"); raw != "" {
			y, convErr := strconv.Atoi(raw)
			if convErr != nil {
				httpx.Error(w, http.StatusBadRequest, "year must be an integer")
				return
			}
			f.Year = &y
		}

		docs, err := svc.List(r.Context(), f)
		if err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				httpx.Error(w, http.StatusNotFound, "document not found")
				return
			}
			httpx.LogError("documents list failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}

		httpx.WriteJSON(w, http.StatusOK, docs)
	})
}
