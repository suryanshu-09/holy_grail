package documents

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

const (
	defaultLimit = 20
	maxLimit     = 100

	statusUploaded = "uploaded"
	pdfMagic       = "%PDF-"
	fallbackName   = "document.pdf"
)

// Repository defines the persistence contract for documents.
type Repository interface {
	List(ctx context.Context, f Filter) ([]Document, error)
	GetByID(ctx context.Context, id string) (Document, error)
	Create(ctx context.Context, d *Document) error
	UpdateStatus(ctx context.Context, id string, status string) error
}

// Document lifecycle statuses stored in documents.status.
const (
	StatusUploaded  = "uploaded"
	StatusExtracted = "extracted"
	StatusFailed    = "failed"
)

// Storage persists uploaded document files and returns their storage-relative
// path.
type Storage interface {
	SaveDocument(ctx context.Context, id string, r io.Reader) (string, error)
	RemoveDocument(ctx context.Context, id string) error
}

// Service contains the business rules for documents.
type Service struct {
	repo  Repository
	store Storage
}

// NewService creates a documents service.
func NewService(repo Repository, store Storage) *Service {
	return &Service{repo: repo, store: store}
}

func clamp(f *Filter) {
	if f.Limit <= 0 {
		f.Limit = defaultLimit
	}
	if f.Limit > maxLimit {
		f.Limit = maxLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
}

// List returns documents matching the filter.
func (s *Service) List(ctx context.Context, f Filter) ([]Document, error) {
	clamp(&f)
	return s.repo.List(ctx, f)
}

// Get returns a single document or apperr.ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (Document, error) {
	doc, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if err == apperr.ErrNotFound {
			return Document{}, apperr.ErrNotFound
		}
		return Document{}, err
	}
	return doc, nil
}

// Upload validates and stores an uploaded PDF, persists its metadata and
// returns the created document. It returns apperr.ErrInvalidUpload when the
// upload fails validation (non-PDF content or unusable filename).
func (s *Service) Upload(ctx context.Context, originalName string, src io.Reader) (Document, error) {
	if src == nil {
		return Document{}, fmt.Errorf("%w: missing file", apperr.ErrInvalidUpload)
	}

	id, err := NewID()
	if err != nil {
		return Document{}, err
	}

	filename := SanitizeFilename(originalName)
	if filename == "" {
		filename = fallbackName
	}

	// Validate the actual content by sniffing the PDF magic bytes instead of
	// trusting the extension or Content-Type header.
	head := make([]byte, len(pdfMagic))
	n, err := io.ReadFull(src, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return Document{}, fmt.Errorf("documents: read file head: %w", err)
	}
	if !bytes.Equal(head[:n], []byte(pdfMagic)) {
		return Document{}, fmt.Errorf("%w: only PDF files are allowed", apperr.ErrInvalidUpload)
	}
	body := io.MultiReader(bytes.NewReader(head[:n]), src)

	relPath, err := s.store.SaveDocument(ctx, id, body)
	if err != nil {
		return Document{}, fmt.Errorf("documents: save file: %w", err)
	}

	doc := Document{
		ID:               id,
		Filename:         filename,
		OriginalFilename: originalName,
		StoragePath:      &relPath,
		Status:           statusUploaded,
	}
	if err := s.repo.Create(ctx, &doc); err != nil {
		// Do not leave orphaned files behind when metadata persistence fails.
		_ = s.store.RemoveDocument(context.WithoutCancel(ctx), id)
		return Document{}, fmt.Errorf("documents: create record: %w", err)
	}
	return doc, nil
}
