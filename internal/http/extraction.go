package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/extraction"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
)

// handleExtractDocument triggers the extraction pipeline for one uploaded
// document and returns its summary.
func handleExtractDocument(docs *documents.Service, ext *extraction.ExtractionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			httpx.Error(w, http.StatusBadRequest, "missing document id")
			return
		}

		doc, err := docs.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				httpx.Error(w, http.StatusNotFound, "document not found")
				return
			}
			httpx.LogError("document lookup failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}

		var storagePath string
		if doc.StoragePath != nil {
			storagePath = *doc.StoragePath
		}
		result, err := ext.Extract(r.Context(), doc.ID, storagePath)
		if err != nil {
			httpx.LogError("document extraction failed", err)
			httpx.Error(w, http.StatusInternalServerError, "extraction failed")
			return
		}

		httpx.WriteJSON(w, http.StatusOK, result.Summary())
	}
}

// handleExtractPreview returns a preview of parsed questions for the given
// document without persisting them. Request body may include { "pages": [1,2] }
// to limit the preview to a subset of pages.
func handleExtractPreview(docs *documents.Service, ext *extraction.ExtractionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			httpx.Error(w, http.StatusBadRequest, "missing document id")
			return
		}
		doc, err := docs.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				httpx.Error(w, http.StatusNotFound, "document not found")
				return
			}
			httpx.LogError("document lookup failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}

		var storagePath string
		if doc.StoragePath != nil {
			storagePath = *doc.StoragePath
		}

		// parse optional pages list from body
		var body struct {
			Pages []int `json:"pages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		parsed, err := ext.ExtractPreview(r.Context(), doc.ID, storagePath, body.Pages)
		if err != nil {
			httpx.LogError("extract preview failed", err)
			httpx.Error(w, http.StatusInternalServerError, "preview failed")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, parsed)
	}
}
