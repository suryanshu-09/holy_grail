package quiz

import (
	"context"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

// Accuracy boundary: all correct must yield 100% and score == attempted.
func TestComputeSessionMetrics_AllCorrect(t *testing.T) {
	attempts := []QuizAttempt{
		{SessionID: "s1", QuestionID: "q1", IsCorrect: true, TimeTakenSeconds: 5, Topic: "Deadlock"},
		{SessionID: "s1", QuestionID: "q2", IsCorrect: true, TimeTakenSeconds: 15, Topic: "Paging"},
	}
	m := ComputeSessionMetrics(attempts)
	if m.Attempted != 2 || m.Correct != 2 || m.Incorrect != 0 || m.Score != 2 {
		t.Fatalf("all-correct counts wrong: %+v", m)
	}
	if m.Accuracy != 100 {
		t.Fatalf("all-correct accuracy = %v want 100", m.Accuracy)
	}
	if m.AverageTimeSeconds != 10 {
		t.Fatalf("avg time = %v want 10", m.AverageTimeSeconds)
	}
}

// Accuracy boundary: all incorrect must yield 0% and score 0.
func TestComputeSessionMetrics_AllIncorrect(t *testing.T) {
	attempts := []QuizAttempt{
		{SessionID: "s1", QuestionID: "q1", IsCorrect: false, TimeTakenSeconds: 7, Topic: "Deadlock"},
		{SessionID: "s1", QuestionID: "q2", IsCorrect: false, TimeTakenSeconds: 9, Topic: "Deadlock"},
	}
	m := ComputeSessionMetrics(attempts)
	if m.Correct != 0 || m.Incorrect != 2 || m.Score != 0 {
		t.Fatalf("all-incorrect counts wrong: %+v", m)
	}
	if m.Accuracy != 0 {
		t.Fatalf("all-incorrect accuracy = %v want 0", m.Accuracy)
	}
	if m.AverageTimeSeconds != 8 {
		t.Fatalf("avg time = %v want 8", m.AverageTimeSeconds)
	}
}

// Single attempt: average time equals that attempt's time.
func TestComputeSessionMetrics_SingleAttempt(t *testing.T) {
	m := ComputeSessionMetrics([]QuizAttempt{
		{SessionID: "s1", QuestionID: "q1", IsCorrect: true, TimeTakenSeconds: 42, Topic: "Paging"},
	})
	if m.Attempted != 1 || m.Correct != 1 || m.Score != 1 || m.Accuracy != 100 {
		t.Fatalf("single-attempt metrics wrong: %+v", m)
	}
	if m.AverageTimeSeconds != 42 {
		t.Fatalf("single avg time = %v want 42", m.AverageTimeSeconds)
	}
}

// Topics with surrounding whitespace must group with the trimmed name.
func TestComputeTopicBreakdown_TrimsWhitespace(t *testing.T) {
	attempts := []QuizAttempt{
		{SessionID: "s1", QuestionID: "q1", IsCorrect: true, TimeTakenSeconds: 5, Topic: "  Deadlock"},
		{SessionID: "s1", QuestionID: "q2", IsCorrect: false, TimeTakenSeconds: 5, Topic: "Deadlock  "},
		{SessionID: "s1", QuestionID: "q3", IsCorrect: true, TimeTakenSeconds: 5, Topic: "   "},
	}
	rows := ComputeTopicBreakdown(attempts)
	if len(rows) != 1 {
		t.Fatalf("expected 1 grouped row, got %v", rows)
	}
	if rows[0].Topic != "Deadlock" || rows[0].Attempted != 2 || rows[0].Correct != 1 {
		t.Fatalf("whitespace grouping wrong: %+v", rows[0])
	}
	if rows[0].Accuracy != 50 {
		t.Fatalf("accuracy = %v want 50", rows[0].Accuracy)
	}
}

// WeakTopics with empty input must return empty (not nil-panic), and a
// custom threshold must filter accordingly.
func TestWeakTopics_EmptyAndCustomThreshold(t *testing.T) {
	if weak := WeakTopics(nil, DefaultWeakTopicThreshold); len(weak) != 0 {
		t.Fatalf("empty breakdown must yield no weak topics, got %v", weak)
	}
	rows := []TopicBreakdown{
		{Topic: "Deadlock", Attempted: 10, Correct: 8, Accuracy: 80},
		{Topic: "Paging", Attempted: 10, Correct: 6, Accuracy: 60},
	}
	weak := WeakTopics(rows, 90)
	if len(weak) != 2 {
		t.Fatalf("threshold 90 must flag both, got %v", weak)
	}
	if weak[0].Topic != "Paging" || weak[1].Topic != "Deadlock" {
		t.Fatalf("weakest-first order wrong: %v", weak)
	}
	strict := WeakTopics(rows, 60)
	if len(strict) != 0 {
		t.Fatalf("accuracy == threshold must not be weak, got %v", strict)
	}
}

func TestQuizSessionValidate_Extra(t *testing.T) {
	var nilSession *QuizSession
	if err := nilSession.Validate(); err == nil {
		t.Fatal("nil session must fail validation")
	}
	for name, s := range map[string]QuizSession{
		"invalid mode":   {Mode: "invent", TotalQuestions: 1},
		"negative total": {Mode: ModeMCQ, TotalQuestions: -1},
		"over max total": {Mode: ModeMCQ, TotalQuestions: MaxNumQuestions + 1},
		"invalid status": {Status: "done"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := s.Validate(); err == nil {
				t.Fatalf("expected validation error for %+v", s)
			}
		})
	}
	// Mode is normalized to lowercase.
	s := QuizSession{Mode: "MCQ", TotalQuestions: 3}
	if err := s.Validate(); err != nil {
		t.Fatalf("uppercase mode must normalize: %v", err)
	}
	if s.Mode != ModeMCQ {
		t.Fatalf("mode = %q want %q", s.Mode, ModeMCQ)
	}
	if s.Status != SessionStatusInProgress {
		t.Fatalf("default status = %q want in_progress", s.Status)
	}
}

func TestQuizAttemptValidate_MissingFields(t *testing.T) {
	var nilAttempt *QuizAttempt
	if err := nilAttempt.Validate(); err == nil {
		t.Fatal("nil attempt must fail validation")
	}
	cases := map[string]QuizAttempt{
		"missing session":  {QuestionID: "q1", SelectedAnswer: 0, CorrectAnswer: 0},
		"missing question": {SessionID: "s1", SelectedAnswer: 0, CorrectAnswer: 0},
		"bad correct":      {SessionID: "s1", QuestionID: "q1", SelectedAnswer: 0, CorrectAnswer: 9},
		"negative correct": {SessionID: "s1", QuestionID: "q1", SelectedAnswer: 0, CorrectAnswer: -1},
	}
	for name, a := range cases {
		t.Run(name, func(t *testing.T) {
			if err := a.Validate(); err == nil {
				t.Fatalf("expected validation error for %+v", a)
			}
		})
	}
	// Whitespace-only IDs are treated as missing.
	ws := QuizAttempt{SessionID: "  ", QuestionID: "q1", SelectedAnswer: 0, CorrectAnswer: 0}
	if err := ws.Validate(); err == nil {
		t.Fatal("whitespace session_id must fail")
	}
}

func TestEvaluationService_NotFound(t *testing.T) {
	svc := NewEvaluationService(newFakeEvalStore())
	ctx := context.Background()
	if _, err := svc.GetResult(ctx, "missing"); err == nil {
		t.Fatal("GetResult on unknown session must fail")
	}
	if _, err := svc.SubmitAttempt(ctx, QuizAttempt{
		SessionID: "missing", QuestionID: "q1", SelectedAnswer: 0, CorrectAnswer: 0,
	}); err == nil {
		t.Fatal("SubmitAttempt on unknown session must fail")
	}
	if _, err := svc.SubmitAttempts(ctx, "missing", []QuizAttempt{
		{QuestionID: "q1", SelectedAnswer: 0, CorrectAnswer: 0},
	}); err == nil {
		t.Fatal("SubmitAttempts on unknown session must fail")
	}
}

// GetResult on an unknown session must surface apperr.ErrNotFound so
// handlers map it to 404.
func TestEvaluationService_NotFoundIs404(t *testing.T) {
	svc := NewEvaluationService(newFakeEvalStore())
	if _, err := svc.GetResult(context.Background(), "nope"); err != apperr.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEvaluationService_NilStore(t *testing.T) {
	svc := NewEvaluationService(nil)
	ctx := context.Background()
	if _, err := svc.CreateSession(ctx, QuizSession{}); err == nil {
		t.Fatal("nil store CreateSession must fail")
	}
	if _, err := svc.SubmitAttempt(ctx, QuizAttempt{}); err == nil {
		t.Fatal("nil store SubmitAttempt must fail")
	}
	if _, err := svc.SubmitAttempts(ctx, "s1", []QuizAttempt{{}}); err == nil {
		t.Fatal("nil store SubmitAttempts must fail")
	}
	if _, err := svc.GetResult(ctx, "s1"); err == nil {
		t.Fatal("nil store GetResult must fail")
	}
}

// Mismatched session IDs inside a bulk payload are pinned to the URL
// session (SubmitAttempts overwrites SessionID).
func TestEvaluationService_BulkPinsSessionID(t *testing.T) {
	svc := NewEvaluationService(newFakeEvalStore())
	ctx := context.Background()
	session, err := svc.CreateSession(ctx, QuizSession{Mode: ModeMCQ, TotalQuestions: 2})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	got, err := svc.SubmitAttempts(ctx, session.ID, []QuizAttempt{
		{SessionID: "other", QuestionID: "q1", SelectedAnswer: 0, CorrectAnswer: 0, Topic: "Deadlock"},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if got[0].SessionID != session.ID {
		t.Fatalf("bulk must pin session id, got %q", got[0].SessionID)
	}
	if strings.TrimSpace(got[0].Topic) != "Deadlock" {
		t.Fatalf("topic must be trimmed, got %q", got[0].Topic)
	}
}
