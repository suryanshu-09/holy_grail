package quiz

import (
	"context"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

func evalAttempts() []QuizAttempt {
	return []QuizAttempt{
		{SessionID: "s1", QuestionID: "quiz-q1", SelectedAnswer: 1, CorrectAnswer: 1, IsCorrect: true, TimeTakenSeconds: 10, Topic: "Deadlock"},
		{SessionID: "s1", QuestionID: "quiz-q2", SelectedAnswer: 0, CorrectAnswer: 2, IsCorrect: false, TimeTakenSeconds: 20, Topic: "Deadlock"},
		{SessionID: "s1", QuestionID: "quiz-q3", SelectedAnswer: 0, CorrectAnswer: 0, IsCorrect: true, TimeTakenSeconds: 30, Topic: "Paging"},
	}
}

func TestComputeSessionMetrics(t *testing.T) {
	m := ComputeSessionMetrics(evalAttempts())
	if m.Attempted != 3 {
		t.Fatalf("attempted = %d want 3", m.Attempted)
	}
	if m.Correct != 2 {
		t.Fatalf("correct = %d want 2", m.Correct)
	}
	if m.Incorrect != 1 {
		t.Fatalf("incorrect = %d want 1", m.Incorrect)
	}
	if m.Score != 2 {
		t.Fatalf("score = %d want 2", m.Score)
	}
	wantAcc := 2.0 / 3.0 * 100
	if m.Accuracy < wantAcc-1e-9 || m.Accuracy > wantAcc+1e-9 {
		t.Fatalf("accuracy = %v want %v", m.Accuracy, wantAcc)
	}
	if m.AverageTimeSeconds != 20 {
		t.Fatalf("avg time = %v want 20", m.AverageTimeSeconds)
	}
}

func TestComputeSessionMetricsEmpty(t *testing.T) {
	m := ComputeSessionMetrics(nil)
	if m.Attempted != 0 || m.Correct != 0 || m.Incorrect != 0 || m.Score != 0 {
		t.Fatalf("empty metrics must be zero, got %+v", m)
	}
	if m.Accuracy != 0 || m.AverageTimeSeconds != 0 {
		t.Fatalf("empty accuracy/avg must be 0, got %+v", m)
	}
}

func TestComputeTopicBreakdown(t *testing.T) {
	rows := ComputeTopicBreakdown(evalAttempts())
	if len(rows) != 2 {
		t.Fatalf("expected 2 topic rows, got %v", rows)
	}
	// Sorted by topic name for determinism.
	if rows[0].Topic != "Deadlock" || rows[1].Topic != "Paging" {
		t.Fatalf("rows not sorted by topic: %v", rows)
	}
	if rows[0].Attempted != 2 || rows[0].Correct != 1 || rows[0].Incorrect != 1 {
		t.Fatalf("deadlock row wrong: %+v", rows[0])
	}
	if rows[0].Accuracy != 50 {
		t.Fatalf("deadlock accuracy = %v want 50", rows[0].Accuracy)
	}
	if rows[1].Accuracy != 100 {
		t.Fatalf("paging accuracy = %v want 100", rows[1].Accuracy)
	}
}

func TestComputeTopicBreakdownSkipsUntagged(t *testing.T) {
	attempts := append(evalAttempts(), QuizAttempt{
		SessionID: "s1", QuestionID: "quiz-q4", SelectedAnswer: 0,
		CorrectAnswer: 0, IsCorrect: true, TimeTakenSeconds: 5,
	})
	rows := ComputeTopicBreakdown(attempts)
	if len(rows) != 2 {
		t.Fatalf("untagged attempts must be skipped, got %v", rows)
	}
}

func TestWeakTopics(t *testing.T) {
	rows := ComputeTopicBreakdown(evalAttempts())
	weak := WeakTopics(rows, DefaultWeakTopicThreshold)
	if len(weak) != 1 || weak[0].Topic != "Deadlock" {
		t.Fatalf("weak topics = %v want [Deadlock]", weak)
	}
	// Threshold boundary: accuracy == threshold is not weak.
	none := WeakTopics([]TopicBreakdown{{Topic: "Edge", Attempted: 1, Correct: 1, Accuracy: 70}}, 70)
	if len(none) != 0 {
		t.Fatalf("accuracy == threshold must not be weak, got %v", none)
	}
	// Weakest first.
	multi := WeakTopics([]TopicBreakdown{
		{Topic: "B", Attempted: 2, Correct: 1, Accuracy: 50},
		{Topic: "A", Attempted: 4, Correct: 1, Accuracy: 25},
	}, 70)
	if len(multi) != 2 || multi[0].Topic != "A" || multi[1].Topic != "B" {
		t.Fatalf("weak topics not sorted weakest-first: %v", multi)
	}
}

func TestNewSessionResult(t *testing.T) {
	session := QuizSession{ID: "s1", Mode: ModeMCQ, Subject: "Operating Systems", TotalQuestions: 3}
	res := NewSessionResult(session, evalAttempts())
	if res.Metrics.Attempted != 3 || res.Metrics.Correct != 2 || res.Metrics.Incorrect != 1 {
		t.Fatalf("metrics wrong: %+v", res.Metrics)
	}
	if len(res.Topics) != 2 {
		t.Fatalf("topics wrong: %+v", res.Topics)
	}
	if len(res.WeakTopic) != 1 || res.WeakTopic[0].Topic != "Deadlock" {
		t.Fatalf("weak topics wrong: %+v", res.WeakTopic)
	}
}

func TestQuizAttemptValidateDerivesIsCorrect(t *testing.T) {
	a := QuizAttempt{SessionID: "s1", QuestionID: "q1", SelectedAnswer: 2, CorrectAnswer: 2, TimeTakenSeconds: 5}
	if err := a.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !a.IsCorrect {
		t.Fatal("is_correct must be derived as selected == correct")
	}
	bad := QuizAttempt{SessionID: "s1", QuestionID: "q1", SelectedAnswer: 9, CorrectAnswer: 0}
	if err := bad.Validate(); err == nil {
		t.Fatal("out-of-bounds selected_answer should fail")
	}
	neg := QuizAttempt{SessionID: "s1", QuestionID: "q1", SelectedAnswer: 0, CorrectAnswer: 0, TimeTakenSeconds: -1}
	if err := neg.Validate(); err == nil {
		t.Fatal("negative time_taken_seconds should fail")
	}
}

// fakeEvalStore is an in-memory EvaluationStore for service tests.
type fakeEvalStore struct {
	sessions map[string]QuizSession
	attempts map[string][]QuizAttempt
}

func newFakeEvalStore() *fakeEvalStore {
	return &fakeEvalStore{sessions: map[string]QuizSession{}, attempts: map[string][]QuizAttempt{}}
}

func (f *fakeEvalStore) CreateSession(_ context.Context, s QuizSession) (QuizSession, error) {
	if err := s.Validate(); err != nil {
		return QuizSession{}, err
	}
	s.ID = "sess-1"
	f.sessions[s.ID] = s
	return s, nil
}

func (f *fakeEvalStore) GetSession(_ context.Context, id string) (QuizSession, error) {
	s, ok := f.sessions[strings.TrimSpace(id)]
	if !ok {
		return QuizSession{}, apperr.ErrNotFound
	}
	return s, nil
}

func (f *fakeEvalStore) RecordAttempt(_ context.Context, a QuizAttempt) (QuizAttempt, error) {
	if err := a.Validate(); err != nil {
		return QuizAttempt{}, err
	}
	if _, ok := f.sessions[a.SessionID]; !ok {
		return QuizAttempt{}, apperr.ErrNotFound
	}
	a.ID = "att-" + a.QuestionID
	f.attempts[a.SessionID] = append(f.attempts[a.SessionID], a)
	return a, nil
}

func (f *fakeEvalStore) ListAttempts(_ context.Context, sessionID string) ([]QuizAttempt, error) {
	return append([]QuizAttempt{}, f.attempts[strings.TrimSpace(sessionID)]...), nil
}

func TestEvaluationServiceSubmitAndResult(t *testing.T) {
	svc := NewEvaluationService(newFakeEvalStore())
	ctx := context.Background()
	session, err := svc.CreateSession(ctx, QuizSession{Mode: ModeMCQ, Subject: "OS", TotalQuestions: 3})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	got, err := svc.SubmitAttempts(ctx, session.ID, []QuizAttempt{
		{QuestionID: "quiz-q1", SelectedAnswer: 1, CorrectAnswer: 1, TimeTakenSeconds: 10, Topic: "Deadlock"},
		{QuestionID: "quiz-q2", SelectedAnswer: 0, CorrectAnswer: 2, TimeTakenSeconds: 20, Topic: "Deadlock"},
		{QuestionID: "quiz-q3", SelectedAnswer: 0, CorrectAnswer: 0, TimeTakenSeconds: 30, Topic: "Paging"},
	})
	if err != nil {
		t.Fatalf("submit attempts: %v", err)
	}
	if len(got) != 3 || !got[0].IsCorrect || got[1].IsCorrect {
		t.Fatalf("is_correct not derived on submit: %+v", got)
	}
	res, err := svc.GetResult(ctx, session.ID)
	if err != nil {
		t.Fatalf("get result: %v", err)
	}
	if res.Metrics.Score != 2 || res.Metrics.Attempted != 3 || res.Metrics.Incorrect != 1 {
		t.Fatalf("metrics wrong: %+v", res.Metrics)
	}
	if res.Metrics.AverageTimeSeconds != 20 {
		t.Fatalf("avg time = %v want 20", res.Metrics.AverageTimeSeconds)
	}
	if len(res.WeakTopic) != 1 || res.WeakTopic[0].Topic != "Deadlock" {
		t.Fatalf("weak topics wrong: %+v", res.WeakTopic)
	}
}

func TestEvaluationServiceBulkValidation(t *testing.T) {
	svc := NewEvaluationService(newFakeEvalStore())
	ctx := context.Background()
	if _, err := svc.SubmitAttempts(ctx, "", []QuizAttempt{{QuestionID: "q1"}}); err == nil {
		t.Fatal("empty session id should fail")
	}
	if _, err := svc.SubmitAttempts(ctx, "sess-1", nil); err == nil {
		t.Fatal("empty attempts should fail")
	}
}
