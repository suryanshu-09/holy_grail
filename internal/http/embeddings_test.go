package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/embeddings"
)

type embeddingDocumentRepo struct {
	documents map[string]documents.Document
}

func (r embeddingDocumentRepo) List(_ context.Context, _ documents.Filter) ([]documents.Document, error) {
	return nil, nil
}
func (r embeddingDocumentRepo) GetByID(_ context.Context, id string) (documents.Document, error) {
	document, ok := r.documents[id]
	if !ok {
		return documents.Document{}, apperr.ErrNotFound
	}
	return document, nil
}
func (r embeddingDocumentRepo) Create(_ context.Context, _ *documents.Document) error { return nil }
func (r embeddingDocumentRepo) UpdateStatus(_ context.Context, _ string, _ string) error {
	return nil
}

type embeddingPipelineStub struct {
	result embeddings.Result
	called bool
}

func (p *embeddingPipelineStub) EmbedDocument(_ context.Context, _ string) (embeddings.Result, error) {
	p.called = true
	return p.result, nil
}

func TestHandleEmbedDocumentReturnsPartialResult(t *testing.T) {
	documentsService := documents.NewService(embeddingDocumentRepo{documents: map[string]documents.Document{
		"document-1": {ID: "document-1"},
	}}, nil)
	pipeline := &embeddingPipelineStub{result: embeddings.Result{DocumentID: "document-1", Embedded: 2, Failed: 1}}
	handler := handleEmbedDocument(documentsService, pipeline)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/documents/document-1/embed", nil)
	request.SetPathValue("id", "document-1")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !pipeline.called {
		t.Fatalf("status = %d body = %s called = %t", response.Code, response.Body.String(), pipeline.called)
	}
	if got := response.Body.String(); !strings.Contains(got, `"status":"partial"`) {
		t.Errorf("body = %s, want partial status", got)
	}
}

func TestHandleEmbedDocumentRejectsUnknownDocument(t *testing.T) {
	documentsService := documents.NewService(embeddingDocumentRepo{documents: map[string]documents.Document{}}, nil)
	pipeline := &embeddingPipelineStub{}
	handler := handleEmbedDocument(documentsService, pipeline)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/documents/missing/embed", nil)
	request.SetPathValue("id", "missing")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || pipeline.called {
		t.Fatalf("status = %d called = %t", response.Code, pipeline.called)
	}
}
