package topics

import (
	"context"
	"errors"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

// mockRepo implements topics.Repository for unit tests.
type mockRepo struct {
	topics           map[string]Topic
	setCalls         [][]string
	mergeCalls       [][2]string
	listForQuestion  []Topic
	getByIDErr       error
	setErr           error
	mergeErr         error
	listErr          error
}

func newMockRepo() *mockRepo {
	return &mockRepo{topics: make(map[string]Topic)}
}

func (m *mockRepo) List(_ context.Context, _ Filter) ([]Topic, error) { return nil, nil }
func (m *mockRepo) GetByID(_ context.Context, id string) (Topic, error) {
	if m.getByIDErr != nil {
		return Topic{}, m.getByIDErr
	}
	if t, ok := m.topics[id]; ok {
		return t, nil
	}
	return Topic{}, apperr.ErrNotFound
}
func (m *mockRepo) Create(_ context.Context, _ string, _ *string) (Topic, error) { return Topic{}, nil }
func (m *mockRepo) GetByName(_ context.Context, _ string, _ *string) (Topic, error) {
	return Topic{}, apperr.ErrNotFound
}
func (m *mockRepo) FindOrCreate(_ context.Context, _ string, _ *string) (Topic, error) {
	return Topic{}, nil
}
func (m *mockRepo) ListWithCounts(_ context.Context, _ Filter) ([]TopicWithCount, error) {
	return nil, nil
}
func (m *mockRepo) MergeTopics(_ context.Context, sourceID, targetID string) error {
	m.mergeCalls = append(m.mergeCalls, [2]string{sourceID, targetID})
	if m.mergeErr != nil {
		return m.mergeErr
	}
	// simulate delete source
	delete(m.topics, sourceID)
	return nil
}
func (m *mockRepo) AddQuestionTopic(_ context.Context, _, _ string, _ *float64) error { return nil }
func (m *mockRepo) RemoveQuestionTopic(_ context.Context, _, _ string) error        { return nil }
func (m *mockRepo) ListTopicsForQuestion(_ context.Context, _ string) ([]Topic, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.listForQuestion != nil {
		return m.listForQuestion, nil
	}
	// default: return topics that were set
	if len(m.setCalls) > 0 {
		last := m.setCalls[len(m.setCalls)-1]
		out := []Topic{}
		for _, id := range last {
			if t, ok := m.topics[id]; ok {
				out = append(out, t)
			}
		}
		return out, nil
	}
	return []Topic{}, nil
}
func (m *mockRepo) ListQuestionTopics(_ context.Context, _ string) ([]QuestionTopic, error) {
	return nil, nil
}
func (m *mockRepo) SetQuestionTopics(_ context.Context, _ string, topicIDs []string, _ map[string]*float64) error {
	if m.setErr != nil {
		return m.setErr
	}
	cp := make([]string, len(topicIDs))
	copy(cp, topicIDs)
	m.setCalls = append(m.setCalls, cp)
	return nil
}

func TestCorrectQuestionTopics_Success(t *testing.T) {
	repo := newMockRepo()
	repo.topics["t1"] = Topic{ID: "t1", Name: "Deadlock"}
	repo.topics["t2"] = Topic{ID: "t2", Name: "Paging"}
	svc := NewService(repo)

	updated, err := svc.CorrectQuestionTopics(context.Background(), "q1", []string{"t1", "t2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.setCalls) != 1 {
		t.Fatalf("expected 1 SetQuestionTopics call, got %d", len(repo.setCalls))
	}
	if len(updated) != 2 {
		t.Fatalf("expected 2 topics returned, got %d", len(updated))
	}
}

func TestCorrectQuestionTopics_Dedup(t *testing.T) {
	repo := newMockRepo()
	repo.topics["t1"] = Topic{ID: "t1", Name: "Deadlock"}
	svc := NewService(repo)
	_, err := svc.CorrectQuestionTopics(context.Background(), "q1", []string{"t1", " t1 ", "t1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.setCalls[0]) != 1 || repo.setCalls[0][0] != "t1" {
		t.Errorf("expected deduped single t1, got %v", repo.setCalls[0])
	}
}

func TestCorrectQuestionTopics_EmptyQuestionID(t *testing.T) {
	repo := newMockRepo()
	svc := NewService(repo)
	_, err := svc.CorrectQuestionTopics(context.Background(), "", []string{"t1"})
	if err == nil || err.Error() != "question id is required" {
		t.Errorf("expected question id required error, got %v", err)
	}
}

func TestCorrectQuestionTopics_EmptyClears(t *testing.T) {
	repo := newMockRepo()
	repo.topics["t1"] = Topic{ID: "t1", Name: "Deadlock"}
	svc := NewService(repo)
	// clearing topics by passing empty slice should succeed and call Set with empty
	_, err := svc.CorrectQuestionTopics(context.Background(), "q1", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.setCalls[0]) != 0 {
		t.Errorf("expected empty set, got %v", repo.setCalls[0])
	}
	// also test nil slice
	repo2 := newMockRepo()
	repo2.topics["t1"] = Topic{ID: "t1", Name: "Deadlock"}
	svc2 := NewService(repo2)
	_, err = svc2.CorrectQuestionTopics(context.Background(), "q1", nil)
	if err != nil {
		t.Fatalf("unexpected error for nil: %v", err)
	}
	if len(repo2.setCalls[0]) != 0 {
		t.Errorf("expected empty set for nil, got %v", repo2.setCalls[0])
	}
}

func TestCorrectQuestionTopics_TopicNotFound(t *testing.T) {
	repo := newMockRepo()
	repo.topics["t1"] = Topic{ID: "t1", Name: "Deadlock"}
	svc := NewService(repo)
	_, err := svc.CorrectQuestionTopics(context.Background(), "q1", []string{"t1", "missing"})
	if !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMergeDuplicateTopics_Success(t *testing.T) {
	repo := newMockRepo()
	repo.topics["target"] = Topic{ID: "target", Name: "Deadlock"}
	repo.topics["s1"] = Topic{ID: "s1", Name: "Deadlocks"}
	repo.topics["s2"] = Topic{ID: "s2", Name: "deadlock problem"}
	svc := NewService(repo)
	err := svc.MergeDuplicateTopics(context.Background(), "target", []string{"s1", "s2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.mergeCalls) != 2 {
		t.Fatalf("expected 2 merges, got %d", len(repo.mergeCalls))
	}
	if repo.mergeCalls[0][0] != "s1" || repo.mergeCalls[0][1] != "target" {
		t.Errorf("first merge mismatch: %v", repo.mergeCalls[0])
	}
}

func TestMergeDuplicateTopics_DedupSources(t *testing.T) {
	repo := newMockRepo()
	repo.topics["target"] = Topic{ID: "target", Name: "Deadlock"}
	repo.topics["s1"] = Topic{ID: "s1", Name: "Deadlocks"}
	svc := NewService(repo)
	err := svc.MergeDuplicateTopics(context.Background(), "target", []string{"s1", "s1", " s1 "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.mergeCalls) != 1 {
		t.Errorf("expected 1 merge after dedup, got %d", len(repo.mergeCalls))
	}
}

func TestMergeDuplicateTopics_TargetInSources(t *testing.T) {
	repo := newMockRepo()
	repo.topics["target"] = Topic{ID: "target", Name: "Deadlock"}
	repo.topics["s1"] = Topic{ID: "s1", Name: "Deadlocks"}
	svc := NewService(repo)
	err := svc.MergeDuplicateTopics(context.Background(), "target", []string{"s1", "target"})
	if err == nil || err.Error() != "target and source must differ" {
		t.Errorf("expected target and source must differ, got %v", err)
	}
}

func TestMergeDuplicateTopics_EmptyTarget(t *testing.T) {
	repo := newMockRepo()
	svc := NewService(repo)
	err := svc.MergeDuplicateTopics(context.Background(), "", []string{"s1"})
	if err == nil || err.Error() != "target id is required" {
		t.Errorf("expected target id is required, got %v", err)
	}
}

func TestMergeDuplicateTopics_EmptySources(t *testing.T) {
	repo := newMockRepo()
	repo.topics["target"] = Topic{ID: "target", Name: "Deadlock"}
	svc := NewService(repo)
	err := svc.MergeDuplicateTopics(context.Background(), "target", []string{})
	if err == nil || err.Error() != "source ids are required" {
		t.Errorf("expected source ids are required, got %v", err)
	}
	// also whitespace-only sources filtered to empty
	err = svc.MergeDuplicateTopics(context.Background(), "target", []string{"   "})
	if err == nil || err.Error() != "source ids are required" {
		t.Errorf("expected source ids are required for whitespace, got %v", err)
	}
}

func TestMergeDuplicateTopics_TargetNotFound(t *testing.T) {
	repo := newMockRepo()
	repo.topics["s1"] = Topic{ID: "s1", Name: "Deadlocks"}
	svc := NewService(repo)
	err := svc.MergeDuplicateTopics(context.Background(), "missing", []string{"s1"})
	if !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMergeDuplicateTopics_SourceNotFound(t *testing.T) {
	repo := newMockRepo()
	repo.topics["target"] = Topic{ID: "target", Name: "Deadlock"}
	svc := NewService(repo)
	err := svc.MergeDuplicateTopics(context.Background(), "target", []string{"missing"})
	if !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMergeDuplicateTopics_MergeErrorPropagates(t *testing.T) {
	repo := newMockRepo()
	repo.topics["target"] = Topic{ID: "target", Name: "Deadlock"}
	repo.topics["s1"] = Topic{ID: "s1", Name: "Deadlocks"}
	repo.mergeErr = errors.New("db failure")
	svc := NewService(repo)
	err := svc.MergeDuplicateTopics(context.Background(), "target", []string{"s1"})
	if err == nil || err.Error() == "" {
		t.Error("expected error")
	}
}
