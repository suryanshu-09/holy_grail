package quiz

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/auth"
)

func itoa(n int) string { return strconv.Itoa(n) }

// historyFakeStore extends the in-memory fake with session listing.
type historyFakeStore struct {
	*fakeEvalStore
	next int
}

func (f *historyFakeStore) CreateSession(_ context.Context, s QuizSession) (QuizSession, error) {
	if err := s.Validate(); err != nil {
		return QuizSession{}, err
	}
	f.next++
	s.ID = "sess-" + itoa(f.next)
	f.sessions[s.ID] = s
	return s, nil
}

func (f *historyFakeStore) ListSessionsByUser(_ context.Context, userID string, limit, offset int) ([]QuizSession, error) {
	out := []QuizSession{}
	for _, s := range f.sessions {
		if s.UserID == userID {
			out = append(out, s)
		}
	}
	if offset > len(out) {
		return []QuizSession{}, nil
	}
	out = out[offset:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []QuizSession{}
	}
	return out, nil
}

func TestSessionOwnership(t *testing.T) {
	svc := NewEvaluationService(&historyFakeStore{fakeEvalStore: newFakeEvalStore()})
	ctx := context.Background()
	alice := auth.User{ID: "user-alice", Email: "alice@example.com"}
	bob := auth.User{ID: "user-bob", Email: "bob@example.com"}
	aliceCtx := auth.WithUser(ctx, alice)
	bobCtx := auth.WithUser(ctx, bob)

	created, err := svc.CreateSession(aliceCtx, QuizSession{Mode: ModeMCQ, TotalQuestions: 2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.UserID != alice.ID {
		t.Fatalf("session not attributed to creator: %+v", created)
	}

	// Anonymous sessions stay unowned.
	anon, err := svc.CreateSession(ctx, QuizSession{TotalQuestions: 1})
	if err != nil {
		t.Fatalf("anon create: %v", err)
	}
	if anon.UserID != "" {
		t.Fatalf("anonymous session attributed: %+v", anon)
	}

	// Cross-owner reads are masked as not found.
	if _, err := svc.GetResult(bobCtx, created.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("bob read = %v, want not found", err)
	}
	if _, err := svc.GetResult(ctx, created.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("anonymous read = %v, want not found", err)
	}
	// Owner reads work.
	if _, err := svc.GetResult(aliceCtx, created.ID); err != nil {
		t.Fatalf("alice read: %v", err)
	}
	// Legacy unowned sessions stay visible to everyone.
	if _, err := svc.GetResult(bobCtx, anon.ID); err != nil {
		t.Fatalf("legacy read: %v", err)
	}

	// Cross-owner writes are rejected too.
	attempt := QuizAttempt{SessionID: created.ID, QuestionID: "q1", SelectedAnswer: 0, CorrectAnswer: 0}
	if _, err := svc.SubmitAttempt(bobCtx, attempt); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("bob submit = %v, want not found", err)
	}
	if _, err := svc.SubmitAttempts(bobCtx, created.ID, []QuizAttempt{attempt}); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("bob bulk submit = %v, want not found", err)
	}
	if _, err := svc.SubmitAttempt(aliceCtx, attempt); err != nil {
		t.Fatalf("alice submit: %v", err)
	}
}

func TestListSessionsHistory(t *testing.T) {
	svc := NewEvaluationService(&historyFakeStore{fakeEvalStore: newFakeEvalStore()})
	ctx := context.Background()
	alice := auth.User{ID: "user-alice", Email: "alice@example.com"}
	aliceCtx := auth.WithUser(ctx, alice)

	if _, err := svc.CreateSession(aliceCtx, QuizSession{TotalQuestions: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSession(ctx, QuizSession{TotalQuestions: 1}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ListSessions(aliceCtx, 20, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].UserID != alice.ID {
		t.Fatalf("history leaks or misses sessions: %+v", got)
	}

	// Anonymous callers get an empty history.
	anon, err := svc.ListSessions(ctx, 20, 0)
	if err != nil {
		t.Fatalf("anon list: %v", err)
	}
	if len(anon) != 0 {
		t.Fatalf("anonymous history = %+v, want empty", anon)
	}

	// Stores without listing support report an error.
	plain := NewEvaluationService(newFakeEvalStore())
	if _, err := plain.ListSessions(aliceCtx, 20, 0); err == nil {
		t.Fatalf("unsupported store listed history")
	}
}
