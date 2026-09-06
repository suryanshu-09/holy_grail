package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/embeddings"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
)

// EmbeddingPipeline generates embeddings for all questions in one document.
type EmbeddingPipeline interface {
	EmbedDocument(ctx context.Context, documentID string) (embeddings.Result, error)
}

func handleEmbedDocument(documentsService *documents.Service, pipeline EmbeddingPipeline) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if pipeline == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "embedding pipeline not configured")
			return
		}
		id := r.PathValue("id")
		if id == "" {
			httpx.Error(w, http.StatusBadRequest, "missing document id")
			return
		}
		if documentsService == nil {
			httpx.Error(w, http.StatusInternalServerError, "documents service not configured")
			return
		}
		if _, err := documentsService.Get(r.Context(), id); err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				httpx.Error(w, http.StatusNotFound, "document not found")
				return
			}
			httpx.LogError("document lookup failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		result, err := pipeline.EmbedDocument(r.Context(), id)
		if err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				httpx.Error(w, http.StatusNotFound, "document not found")
				return
			}
			httpx.LogError("embedding failed", err)
			httpx.Error(w, http.StatusInternalServerError, "embedding failed")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, struct {
			embeddings.Result
			Status string `json:"status"`
		}{Result: result, Status: result.Status()})
	})
}
