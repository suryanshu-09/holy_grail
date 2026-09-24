package questions

import (
	"context"
	"testing"
)

func TestNewRepositoryNilSafe(t *testing.T) {
	if NewRepository(nil) == nil {
		t.Fatalf("NewRepository(nil) returned nil")
	}
}

func TestAddTopicValidation(t *testing.T) {
	// Empty IDs are rejected before any database access, so a nil *sql.DB
	// is safe here and no external database is required.
	repo := NewRepository(nil)
	tests := []struct {
		name       string
		questionID string
		topicID    string
	}{
		{"both empty", "", ""},
		{"empty question", "", "t1"},
		{"empty topic", "q1", ""},
	}
	for _, tc := range tests {
		t.Run("AddTopic "+tc.name, func(t *testing.T) {
			if err := repo.(*repository).AddTopic(context.Background(), tc.questionID, tc.topicID, nil); err == nil {
				t.Fatalf("AddTopic(%q, %q) succeeded, want error", tc.questionID, tc.topicID)
			}
		})
		t.Run("AddQuestionTopic alias "+tc.name, func(t *testing.T) {
			if err := repo.(*repository).AddQuestionTopic(context.Background(), tc.questionID, tc.topicID, nil); err == nil {
				t.Fatalf("AddQuestionTopic(%q, %q) succeeded, want error", tc.questionID, tc.topicID)
			}
		})
		t.Run("RemoveTopic "+tc.name, func(t *testing.T) {
			if err := repo.(*repository).RemoveTopic(context.Background(), tc.questionID, tc.topicID); err == nil {
				t.Fatalf("RemoveTopic(%q, %q) succeeded, want error", tc.questionID, tc.topicID)
			}
		})
		t.Run("RemoveQuestionTopic alias "+tc.name, func(t *testing.T) {
			if err := repo.(*repository).RemoveQuestionTopic(context.Background(), tc.questionID, tc.topicID); err == nil {
				t.Fatalf("RemoveQuestionTopic(%q, %q) succeeded, want error", tc.questionID, tc.topicID)
			}
		})
	}
}

func TestListTopicsValidation(t *testing.T) {
	repo := NewRepository(nil).(*repository)
	if _, err := repo.ListTopics(context.Background(), ""); err == nil {
		t.Fatalf("ListTopics(\"\") succeeded, want error")
	}
	if _, err := repo.ListTopicsForQuestion(context.Background(), ""); err == nil {
		t.Fatalf("ListTopicsForQuestion(\"\") succeeded, want error")
	}
	if _, err := repo.ListQuestionTopics(context.Background(), ""); err == nil {
		t.Fatalf("ListQuestionTopics(\"\") succeeded, want error")
	}
	if _, err := repo.GetQuestionTopicsForTopic(context.Background(), ""); err == nil {
		t.Fatalf("GetQuestionTopicsForTopic(\"\") succeeded, want error")
	}
}

func TestSetQuestionTopicsValidation(t *testing.T) {
	repo := NewRepository(nil).(*repository)
	if err := repo.SetQuestionTopics(context.Background(), "", []string{"t1"}, nil); err == nil {
		t.Fatalf("SetQuestionTopics(\"\", ...) succeeded, want error")
	}
}
