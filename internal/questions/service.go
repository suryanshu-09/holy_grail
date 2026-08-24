package questions

import (
	"context"
	"strconv"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// Repository defines the persistence contract for questions.
type Repository interface {
	List(ctx context.Context, f Filter) ([]Question, error)
	GetByID(ctx context.Context, id string) (Question, error)
}

// Service contains the business rules for questions.
type Service struct {
	repo Repository
}

// NewService creates a questions service.
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

// ParseYear converts a raw year string into a pointer usable in filters.
// An empty string yields a nil (unset) year.
func ParseYear(raw string) (*int, error) {
	if raw == "" {
		return nil, nil
	}
	y, err := strconv.Atoi(raw)
	if err != nil || y < 1900 || y > 2100 {
		return nil, strconv.ErrSyntax
	}
	return &y, nil
}

// List returns questions matching the filter.
func (s *Service) List(ctx context.Context, f Filter) ([]Question, error) {
	clamp(&f)
	return s.repo.List(ctx, f)
}

// Get returns a single question or apperr.ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (Question, error) {
	q, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if err == apperr.ErrNotFound {
			return Question{}, apperr.ErrNotFound
		}
		return Question{}, err
	}
	return q, nil
}
