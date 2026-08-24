package topics

import (
	"context"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// Repository defines the persistence contract for topics.
type Repository interface {
	List(ctx context.Context, f Filter) ([]Topic, error)
	GetByID(ctx context.Context, id string) (Topic, error)
}

// Service contains the business rules for topics.
type Service struct {
	repo Repository
}

// NewService creates a topics service.
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

// List returns topics matching the filter.
func (s *Service) List(ctx context.Context, f Filter) ([]Topic, error) {
	clamp(&f)
	return s.repo.List(ctx, f)
}

// Get returns a single topic or apperr.ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (Topic, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if err == apperr.ErrNotFound {
			return Topic{}, apperr.ErrNotFound
		}
		return Topic{}, err
	}
	return t, nil
}
