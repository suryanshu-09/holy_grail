package documents

import (
	"context"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// Repository defines the persistence contract for documents.
type Repository interface {
	List(ctx context.Context, f Filter) ([]Document, error)
	GetByID(ctx context.Context, id string) (Document, error)
}

// Service contains the business rules for documents.
type Service struct {
	repo Repository
}

// NewService creates a documents service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
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
