package http

import (
	"errors"
	"net/http"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

// handleTopics lists topics.
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

		httpx.WriteJSON(w, http.StatusOK, ts)
	})
}
