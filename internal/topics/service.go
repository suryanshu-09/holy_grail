package topics

import (
	"context"
	"fmt"
	"strings"

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
	Create(ctx context.Context, name string, subject *string) (Topic, error)
	GetByName(ctx context.Context, name string, subject *string) (Topic, error)
	FindOrCreate(ctx context.Context, name string, subject *string) (Topic, error)
	ListWithCounts(ctx context.Context, f Filter) ([]TopicWithCount, error)
	MergeTopics(ctx context.Context, sourceID, targetID string) error
	AddQuestionTopic(ctx context.Context, questionID, topicID string, confidence *float64) error
	RemoveQuestionTopic(ctx context.Context, questionID, topicID string) error
	ListTopicsForQuestion(ctx context.Context, questionID string) ([]Topic, error)
	ListQuestionTopics(ctx context.Context, questionID string) ([]QuestionTopic, error)
	SetQuestionTopics(ctx context.Context, questionID string, topicIDs []string, confidences map[string]*float64) error
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

// Create creates a new topic.
func (s *Service) Create(ctx context.Context, name string, subject *string) (Topic, error) {
	return s.repo.Create(ctx, name, subject)
}

// GetByName returns a topic by name and optional subject.
func (s *Service) GetByName(ctx context.Context, name string, subject *string) (Topic, error) {
	t, err := s.repo.GetByName(ctx, name, subject)
	if err != nil {
		if err == apperr.ErrNotFound {
			return Topic{}, apperr.ErrNotFound
		}
		return Topic{}, err
	}
	return t, nil
}

// FindOrCreate returns an existing topic or creates one.
func (s *Service) FindOrCreate(ctx context.Context, name string, subject *string) (Topic, error) {
	return s.repo.FindOrCreate(ctx, name, subject)
}

// ListWithCounts returns topics with aggregated question counts.
func (s *Service) ListWithCounts(ctx context.Context, f Filter) ([]TopicWithCount, error) {
	clamp(&f)
	return s.repo.ListWithCounts(ctx, f)
}

// MergeTopics reassigns questions from source to target and deletes source.
func (s *Service) MergeTopics(ctx context.Context, sourceID, targetID string) error {
	return s.repo.MergeTopics(ctx, sourceID, targetID)
}

// AddQuestionTopic associates a question with a topic with optional confidence.
func (s *Service) AddQuestionTopic(ctx context.Context, questionID, topicID string, confidence *float64) error {
	return s.repo.AddQuestionTopic(ctx, questionID, topicID, confidence)
}

// RemoveQuestionTopic removes an association.
func (s *Service) RemoveQuestionTopic(ctx context.Context, questionID, topicID string) error {
	return s.repo.RemoveQuestionTopic(ctx, questionID, topicID)
}

// ListTopicsForQuestion returns topics for a given question.
func (s *Service) ListTopicsForQuestion(ctx context.Context, questionID string) ([]Topic, error) {
	return s.repo.ListTopicsForQuestion(ctx, questionID)
}

// ListQuestionTopics returns join metadata for a question.
func (s *Service) ListQuestionTopics(ctx context.Context, questionID string) ([]QuestionTopic, error) {
	return s.repo.ListQuestionTopics(ctx, questionID)
}

// SetQuestionTopics replaces all topics for a question transactionally.
func (s *Service) SetQuestionTopics(ctx context.Context, questionID string, topicIDs []string, confidences map[string]*float64) error {
	return s.repo.SetQuestionTopics(ctx, questionID, topicIDs, confidences)
}

// CorrectQuestionTopics replaces topics for a question (manual correction).
// It validates inputs, deduplicates topicIDs, verifies each topic exists,
// and performs the replacement transactionally via SetQuestionTopics.
// Returns the updated topic list.
func (s *Service) CorrectQuestionTopics(ctx context.Context, questionID string, topicIDs []string) ([]Topic, error) {
	if strings.TrimSpace(questionID) == "" {
		return nil, fmt.Errorf("question id is required")
	}
	// Deduplicate and clean topicIDs.
	seen := make(map[string]struct{})
	deduped := make([]string, 0, len(topicIDs))
	for _, id := range topicIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	// Validate each topic exists.
	for _, tid := range deduped {
		if _, err := s.repo.GetByID(ctx, tid); err != nil {
			if err == apperr.ErrNotFound {
				return nil, fmt.Errorf("topic %s not found: %w", tid, apperr.ErrNotFound)
			}
			return nil, fmt.Errorf("correct question topics: validate topic %s: %w", tid, err)
		}
	}
	if err := s.repo.SetQuestionTopics(ctx, questionID, deduped, nil); err != nil {
		return nil, err
	}
	return s.repo.ListTopicsForQuestion(ctx, questionID)
}

// MergeDuplicateTopics merges duplicate topics into a target topic.
// sourceIDs are merged into targetID: questions are reassigned, duplicates
// are de-duplicated, confidence is preserved as max, and source topics are deleted.
// It validates inputs, deduplicates sourceIDs, ensures target not in sources,
// verifies existence, and merges each source transactionally.
func (s *Service) MergeDuplicateTopics(ctx context.Context, targetID string, sourceIDs []string) error {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return fmt.Errorf("target id is required")
	}
	if len(sourceIDs) == 0 {
		return fmt.Errorf("source ids are required")
	}
	// Deduplicate sourceIDs and filter empty.
	seen := make(map[string]struct{})
	deduped := make([]string, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	if len(deduped) == 0 {
		return fmt.Errorf("source ids are required")
	}
	// Ensure target not in sources.
	for _, sid := range deduped {
		if sid == targetID {
			return fmt.Errorf("target and source must differ")
		}
	}
	// Verify target exists.
	if _, err := s.repo.GetByID(ctx, targetID); err != nil {
		if err == apperr.ErrNotFound {
			return apperr.ErrNotFound
		}
		return fmt.Errorf("merge duplicate topics: validate target: %w", err)
	}
	// Verify each source exists.
	for _, sid := range deduped {
		if _, err := s.repo.GetByID(ctx, sid); err != nil {
			if err == apperr.ErrNotFound {
				return fmt.Errorf("source topic %s not found: %w", sid, apperr.ErrNotFound)
			}
			return fmt.Errorf("merge duplicate topics: validate source %s: %w", sid, err)
		}
	}
	// Merge each source into target sequentially.
	for _, sid := range deduped {
		if err := s.repo.MergeTopics(ctx, sid, targetID); err != nil {
			return fmt.Errorf("merge source %s: %w", sid, err)
		}
	}
	return nil
}
