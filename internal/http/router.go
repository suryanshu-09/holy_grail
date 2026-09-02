package http

import (
	"context"
	"net/http"

	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/extraction"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

// APIVersion is the current versioned API prefix.
const APIVersion = "/api/v1"

// Pinger matches the subset of *sql.DB needed by the readiness endpoint.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// RouterDeps wires the handler layer to the service layer.
type RouterDeps struct {
	DB         Pinger
	Documents  *documents.Service
	Extraction *extraction.ExtractionService
	Questions  *questions.Service
	Topics     *topics.Service
}

// NewRouter builds the versioned route table wrapped in middleware.
func NewRouter(cfg *config.AppConfig, deps RouterDeps) http.Handler {
	mux := http.NewServeMux()

	mux.Handle(APIVersion+"/health", handleHealth())
	mux.Handle(APIVersion+"/ready", handleReady(deps.DB))
	mux.Handle(APIVersion+"/documents", handleDocuments(deps.Documents, cfg.MaxUploadBytes))
	mux.Handle("POST "+APIVersion+"/documents/{id}/extract", handleExtractDocument(deps.Documents, deps.Extraction))
	mux.Handle("POST "+APIVersion+"/documents/{id}/extract-preview", handleExtractPreview(deps.Documents, deps.Extraction))
	mux.Handle("GET "+APIVersion+"/documents/{id}/images/{name}", handleDocumentImages(cfg.DataDir))
	mux.Handle("GET "+APIVersion+"/documents/{id}/images", handleListDocumentImages(cfg.DataDir))
	mux.Handle(APIVersion+"/questions", handleQuestions(deps.Questions))
	mux.Handle(APIVersion+"/topics", handleTopics(deps.Topics))

	handler := httpx.Recover(mux)
	handler = httpx.RequestLogging(handler)
	return httpx.CORS(cfg.CORSAllowedOrigin)(handler)
}
