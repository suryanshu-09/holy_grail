package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
)

// maxMultipartMemory bounds how much of an upload is buffered in RAM before
// spilling to temp files.
const maxMultipartMemory = 8 << 20 // 8 MiB

// handleDocuments lists documents (GET) and accepts multipart uploads (POST).
func handleDocuments(svc *documents.Service, maxUploadBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listDocuments(svc)(w, r)
		case http.MethodPost:
			uploadDocument(svc, maxUploadBytes)(w, r)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	})
}

// listDocuments returns a paginated, filtered list of documents.
func listDocuments(svc *documents.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

// uploadDocument handles multipart PDF uploads and returns the stored document.
func uploadDocument(svc *documents.Service, maxUploadBytes int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
		if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				httpx.Error(w, http.StatusRequestEntityTooLarge, "uploaded file exceeds the size limit")
				return
			}
			httpx.Error(w, http.StatusBadRequest, "invalid multipart form")
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()

		file, header, err := r.FormFile("file")
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "missing file field")
			return
		}
		defer file.Close()

		doc, err := svc.Upload(r.Context(), header.Filename, file)
		if err != nil {
			if errors.Is(err, apperr.ErrInvalidUpload) {
				httpx.Error(w, http.StatusBadRequest, "only PDF files are allowed")
				return
			}
			httpx.LogError("document upload failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}

		httpx.WriteJSON(w, http.StatusCreated, doc)
	}
}
