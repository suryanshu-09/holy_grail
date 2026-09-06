package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/embeddings"
)

// Service orchestrates embedding a query and performing vector search.
type Service struct {
	repo     Repository
	embedder embeddings.Embedder
	metric   Metric
}

// NewService creates a search service. It validates the embedder dimension
// matches the pgvector schema and that the model is pinned.
func NewService(repo Repository, embedder embeddings.Embedder) (*Service, error) {
	if repo == nil {
		return nil, fmt.Errorf("search: repository is required")
	}
	if embedder == nil {
		return nil, fmt.Errorf("search: embedder is required")
	}
	if embedder.Dimensions() != embeddings.DefaultDimensions {
		return nil, fmt.Errorf("search: dimension %d is incompatible with vector(%d)", embedder.Dimensions(), embeddings.DefaultDimensions)
	}
	if strings.TrimSpace(embedder.Model()) == "" {
		return nil, fmt.Errorf("search: embedder model is required")
	}
	return &Service{repo: repo, embedder: embedder, metric: DefaultMetric}, nil
}

// NewServiceWithMetric allows overriding the similarity metric (used in tests).
func NewServiceWithMetric(repo Repository, embedder embeddings.Embedder, metric Metric) (*Service, error) {
	svc, err := NewService(repo, embedder)
	if err != nil {
		return nil, err
	}
	if metric != "" {
		svc.metric = metric
	}
	return svc, nil
}

// Metric returns the configured similarity metric.
func (s *Service) Metric() Metric { return s.metric }

// Model returns the embedder model used for queries.
func (s *Service) Model() string { return s.embedder.Model() }

// Search embeds the query and retrieves top-K similar questions with metadata filtering.
// It validates the query, clamps pagination, validates threshold, and returns
// similarity scores ordered by descending similarity (ascending distance).
func (s *Service) Search(ctx context.Context, query string, filter Filter) (Response, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Response{}, fmt.Errorf("search: query is required")
	}
	clamp(&filter)
	if filter.Threshold != nil {
		if *filter.Threshold < 0 || *filter.Threshold > 1 {
			return Response{}, fmt.Errorf("search: threshold must be between 0 and 1")
		}
	}
	// Embed the query. Use the same model as stored vectors.
	vectors, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return Response{}, fmt.Errorf("search: embed query: %w", err)
	}
	if len(vectors) != 1 {
		return Response{}, fmt.Errorf("search: embedder returned %d vectors, want 1", len(vectors))
	}
	if len(vectors[0]) != embeddings.DefaultDimensions {
		return Response{}, fmt.Errorf("search: vector dimension %d, want %d", len(vectors[0]), embeddings.DefaultDimensions)
	}
	results, err := s.repo.Search(ctx, vectors[0], filter)
	if err != nil {
		return Response{}, err
	}
	if results == nil {
		results = []Result{}
	}
	return Response{
		Query:   query,
		Results: results,
		Count:   len(results),
		Metric:  s.metric,
		Model:   s.embedder.Model(),
	}, nil
}

// SearchWithVector is a lower-level entry point that accepts a precomputed vector.
// It is useful for testing and for callers that already have a query embedding.
func (s *Service) SearchWithVector(ctx context.Context, vector []float32, filter Filter) (Response, error) {
	if len(vector) != embeddings.DefaultDimensions {
		return Response{}, fmt.Errorf("search: vector dimension %d, want %d", len(vector), embeddings.DefaultDimensions)
	}
	clamp(&filter)
	if filter.Threshold != nil {
		if *filter.Threshold < 0 || *filter.Threshold > 1 {
			return Response{}, fmt.Errorf("search: threshold must be between 0 and 1")
		}
	}
	results, err := s.repo.Search(ctx, vector, filter)
	if err != nil {
		return Response{}, err
	}
	if results == nil {
		results = []Result{}
	}
	return Response{
		Results: results,
		Count:   len(results),
		Metric:  s.metric,
		Model:   s.embedder.Model(),
	}, nil
}
