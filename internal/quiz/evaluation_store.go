package quiz

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

// EvaluationStore persists quiz sessions and attempts.
//
// It is the write/read side of Phase 15: sessions group the attempts
// submitted for a generated quiz so metrics (score, accuracy, attempted,
// correct, incorrect, average time) and per-topic accuracy / weak topics
// can be computed via ComputeSessionMetrics / ComputeTopicBreakdown /
// WeakTopics (see evaluation.go).
type EvaluationStore interface {
	CreateSession(ctx context.Context, s QuizSession) (QuizSession, error)
	GetSession(ctx context.Context, id string) (QuizSession, error)
	RecordAttempt(ctx context.Context, a QuizAttempt) (QuizAttempt, error)
	ListAttempts(ctx context.Context, sessionID string) ([]QuizAttempt, error)
}

// evaluationRepository is the SQL implementation of EvaluationStore.
type evaluationRepository struct {
	db *sql.DB
}

// NewEvaluationRepository creates an EvaluationStore backed by PostgreSQL
// (schema: migrations/007_add_quiz_evaluation.sql).
func NewEvaluationRepository(db *sql.DB) EvaluationStore {
	return &evaluationRepository{db: db}
}

func (r *evaluationRepository) CreateSession(ctx context.Context, s QuizSession) (QuizSession, error) {
	if err := s.Validate(); err != nil {
		return QuizSession{}, err
	}
	var out QuizSession
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO quiz_sessions (mode, subject, total_questions, status)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, mode, subject, total_questions, status, created_at, completed_at`,
		string(s.Mode), s.Subject, s.TotalQuestions, s.Status,
	).Scan(&out.ID, &out.Mode, &out.Subject, &out.TotalQuestions, &out.Status, &out.CreatedAt, &out.CompletedAt)
	if err != nil {
		return QuizSession{}, fmt.Errorf("quiz: create session: %w", err)
	}
	return out, nil
}

func (r *evaluationRepository) GetSession(ctx context.Context, id string) (QuizSession, error) {
	if strings.TrimSpace(id) == "" {
		return QuizSession{}, fmt.Errorf("quiz: session id is required")
	}
	var out QuizSession
	err := r.db.QueryRowContext(ctx,
		`SELECT id, mode, subject, total_questions, status, created_at, completed_at
		 FROM quiz_sessions WHERE id = $1`, strings.TrimSpace(id),
	).Scan(&out.ID, &out.Mode, &out.Subject, &out.TotalQuestions, &out.Status, &out.CreatedAt, &out.CompletedAt)
	if err == sql.ErrNoRows {
		return QuizSession{}, apperr.ErrNotFound
	}
	if err != nil {
		return QuizSession{}, fmt.Errorf("quiz: get session: %w", err)
	}
	return out, nil
}

func (r *evaluationRepository) RecordAttempt(ctx context.Context, a QuizAttempt) (QuizAttempt, error) {
	if err := a.Validate(); err != nil {
		return QuizAttempt{}, err
	}
	// The parent session must exist so attempts never dangle.
	if _, err := r.GetSession(ctx, a.SessionID); err != nil {
		return QuizAttempt{}, err
	}
	var out QuizAttempt
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO quiz_attempts
		 (session_id, question_id, source_question_id, question_text, selected_answer, correct_answer, is_correct, time_taken_seconds, topic, subject)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING id, session_id, question_id, source_question_id, question_text,
		           selected_answer, correct_answer, is_correct, time_taken_seconds, topic, subject, created_at`,
		a.SessionID, a.QuestionID, a.SourceQuestionID, a.QuestionText, a.SelectedAnswer,
		a.CorrectAnswer, a.IsCorrect, a.TimeTakenSeconds, a.Topic, a.Subject,
	).Scan(&out.ID, &out.SessionID, &out.QuestionID, &out.SourceQuestionID, &out.QuestionText,
		&out.SelectedAnswer, &out.CorrectAnswer, &out.IsCorrect, &out.TimeTakenSeconds,
		&out.Topic, &out.Subject, &out.CreatedAt)
	if err != nil {
		return QuizAttempt{}, fmt.Errorf("quiz: record attempt: %w", err)
	}
	return out, nil
}

func (r *evaluationRepository) ListAttempts(ctx context.Context, sessionID string) ([]QuizAttempt, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("quiz: session id is required")
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, session_id, question_id, source_question_id, question_text,
		        selected_answer, correct_answer, is_correct, time_taken_seconds, topic, subject, created_at
		 FROM quiz_attempts WHERE session_id = $1 ORDER BY created_at ASC`, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, fmt.Errorf("quiz: list attempts: %w", err)
	}
	defer rows.Close()
	out := make([]QuizAttempt, 0)
	for rows.Next() {
		var a QuizAttempt
		if err := rows.Scan(&a.ID, &a.SessionID, &a.QuestionID, &a.SourceQuestionID, &a.QuestionText,
			&a.SelectedAnswer, &a.CorrectAnswer, &a.IsCorrect, &a.TimeTakenSeconds,
			&a.Topic, &a.Subject, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("quiz: scan attempt: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("quiz: rows attempts: %w", err)
	}
	return out, nil
}

// EvaluationService is the Phase 15 evaluation service: it records attempts
// and computes session metrics (score, accuracy, attempted, correct,
// incorrect, average time) plus per-topic accuracy and weak topics.
type EvaluationService struct {
	Store EvaluationStore
}

// NewEvaluationService creates the evaluation service.
func NewEvaluationService(store EvaluationStore) *EvaluationService {
	return &EvaluationService{Store: store}
}

// CreateSession opens a new evaluation session.
func (s *EvaluationService) CreateSession(ctx context.Context, session QuizSession) (QuizSession, error) {
	if s == nil || s.Store == nil {
		return QuizSession{}, fmt.Errorf("quiz: evaluation store is required")
	}
	return s.Store.CreateSession(ctx, session)
}

// SubmitAttempt records one attempt (is_correct is derived by Validate).
func (s *EvaluationService) SubmitAttempt(ctx context.Context, attempt QuizAttempt) (QuizAttempt, error) {
	if s == nil || s.Store == nil {
		return QuizAttempt{}, fmt.Errorf("quiz: evaluation store is required")
	}
	return s.Store.RecordAttempt(ctx, attempt)
}

// SubmitAttempts records a batch of attempts for one session in order.
// Every attempt must belong to sessionID; the batch stops at the first error.
func (s *EvaluationService) SubmitAttempts(ctx context.Context, sessionID string, attempts []QuizAttempt) ([]QuizAttempt, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("quiz: evaluation store is required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if len(attempts) == 0 {
		return nil, fmt.Errorf("attempts must contain at least 1 item")
	}
	out := make([]QuizAttempt, 0, len(attempts))
	for i, a := range attempts {
		a.SessionID = strings.TrimSpace(sessionID)
		rec, err := s.Store.RecordAttempt(ctx, a)
		if err != nil {
			return nil, fmt.Errorf("attempts[%d]: %w", i, err)
		}
		out = append(out, rec)
	}
	return out, nil
}

// GetResult loads a session with its attempts and computes metrics,
// per-topic breakdown, and weak topics.
func (s *EvaluationService) GetResult(ctx context.Context, sessionID string) (SessionResult, error) {
	if s == nil || s.Store == nil {
		return SessionResult{}, fmt.Errorf("quiz: evaluation store is required")
	}
	session, err := s.Store.GetSession(ctx, sessionID)
	if err != nil {
		return SessionResult{}, err
	}
	attempts, err := s.Store.ListAttempts(ctx, session.ID)
	if err != nil {
		return SessionResult{}, err
	}
	return NewSessionResult(session, attempts), nil
}
