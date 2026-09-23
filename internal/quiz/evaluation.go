package quiz

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Session lifecycle states for a quiz evaluation session.
const (
	SessionStatusInProgress = "in_progress"
	SessionStatusCompleted  = "completed"
)

// DefaultWeakTopicThreshold is the accuracy percentage below which a topic
// counts as weak (see WeakTopics). Exported so handlers/services share it.
const DefaultWeakTopicThreshold = 70.0

// QuizSession is one evaluated quiz run. It groups the attempts submitted
// for a generated quiz so metrics and per-topic accuracy can be computed.
// UserID attributes the session to its owner (Phase 20); empty marks a
// legacy/anonymous session visible to everyone.
type QuizSession struct {
	// ID is the server-assigned session ID (UUID string). May be empty pre-insert.
	ID string `json:"id,omitempty" db:"id"`
	// Mode is the quiz generation mode used for this session (see QuizMode).
	Mode QuizMode `json:"mode,omitempty" db:"mode"`
	// Subject optionally scopes the session (e.g. "Operating Systems").
	Subject string `json:"subject,omitempty" db:"subject"`
	// TotalQuestions is the number of questions in the quiz being evaluated.
	TotalQuestions int `json:"total_questions" db:"total_questions"`
	// Status is in_progress until the client finishes the quiz.
	Status string `json:"status" db:"status"`
	// UserID is the owner's user ID when created authenticated.
	UserID string `json:"user_id,omitempty" db:"user_id"`
	// CreatedAt is when the session was opened.
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	// CompletedAt is set when the session is finished. Nil while in progress.
	CompletedAt *time.Time `json:"completed_at,omitempty" db:"completed_at"`
}

// Validate checks the session shape. It normalizes Mode/Subject/Status.
func (s *QuizSession) Validate() error {
	if s == nil {
		return fmt.Errorf("quiz session is nil")
	}
	if s.Mode != "" {
		switch QuizMode(strings.ToLower(strings.TrimSpace(string(s.Mode)))) {
		case ModeOriginal, ModeMCQ, ModeSimilar, ModeMixed:
			s.Mode = QuizMode(strings.ToLower(strings.TrimSpace(string(s.Mode))))
		default:
			return fmt.Errorf("invalid mode %q: must be one of original, mcq, similar, mixed", string(s.Mode))
		}
	}
	if s.TotalQuestions < 0 || s.TotalQuestions > MaxNumQuestions {
		return fmt.Errorf("total_questions %d out of range [0,%d]", s.TotalQuestions, MaxNumQuestions)
	}
	s.Subject = strings.TrimSpace(s.Subject)
	if s.Status == "" {
		s.Status = SessionStatusInProgress
	}
	switch s.Status {
	case SessionStatusInProgress, SessionStatusCompleted:
	default:
		return fmt.Errorf("invalid status %q: must be %q or %q", s.Status, SessionStatusInProgress, SessionStatusCompleted)
	}
	return nil
}

// QuizAttempt is a single answered question within a session. It stores per
// spec: session, question, selected answer, correct answer, is_correct, and
// time_taken, plus topic/subject so weak-topic analysis is possible.
type QuizAttempt struct {
	// ID is the server-assigned attempt ID (UUID string). Empty pre-insert.
	ID string `json:"id,omitempty" db:"id"`
	// SessionID is the parent QuizSession ID. Required.
	SessionID string `json:"session_id" db:"session_id"`
	// QuestionID is the quiz question answered (QuizQuestion.ID). Required.
	QuestionID string `json:"question_id" db:"question_id"`
	// SourceQuestionID is the underlying PYQ (QuizQuestion.SourceQuestionID).
	SourceQuestionID string `json:"source_question_id,omitempty" db:"source_question_id"`
	// QuestionText is the answered question stem (optional, for display).
	QuestionText string `json:"question_text,omitempty" db:"question_text"`
	// SelectedAnswer is the 0-based option index the user picked.
	SelectedAnswer int `json:"selected_answer" db:"selected_answer"`
	// CorrectAnswer is the 0-based index of the right option.
	CorrectAnswer int `json:"correct_answer" db:"correct_answer"`
	// IsCorrect is derived as SelectedAnswer == CorrectAnswer (stored per spec).
	IsCorrect bool `json:"is_correct" db:"is_correct"`
	// TimeTakenSeconds is seconds spent on this question. Must be >= 0.
	TimeTakenSeconds float64 `json:"time_taken_seconds" db:"time_taken_seconds"`
	// Topic enables per-topic accuracy (e.g. "Deadlock"). Empty = untagged.
	Topic string `json:"topic,omitempty" db:"topic"`
	// Subject optionally records the question subject.
	Subject string `json:"subject,omitempty" db:"subject"`
	// CreatedAt is when the attempt was recorded.
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Validate checks the attempt shape and derives IsCorrect from the answers.
// Topic/Subject/SourceQuestionID are trimmed; empty Topic is allowed.
func (a *QuizAttempt) Validate() error {
	if a == nil {
		return fmt.Errorf("quiz attempt is nil")
	}
	if strings.TrimSpace(a.SessionID) == "" {
		return fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(a.QuestionID) == "" {
		return fmt.Errorf("question_id is required")
	}
	if a.SelectedAnswer < 0 || a.SelectedAnswer >= RequiredOptions {
		return fmt.Errorf("selected_answer %d out of bounds [0,%d)", a.SelectedAnswer, RequiredOptions)
	}
	if a.CorrectAnswer < 0 || a.CorrectAnswer >= RequiredOptions {
		return fmt.Errorf("correct_answer %d out of bounds [0,%d)", a.CorrectAnswer, RequiredOptions)
	}
	if a.TimeTakenSeconds < 0 {
		return fmt.Errorf("time_taken_seconds %.2f must be >= 0", a.TimeTakenSeconds)
	}
	a.SessionID = strings.TrimSpace(a.SessionID)
	a.QuestionID = strings.TrimSpace(a.QuestionID)
	a.SourceQuestionID = strings.TrimSpace(a.SourceQuestionID)
	a.QuestionText = strings.TrimSpace(a.QuestionText)
	a.Topic = strings.TrimSpace(a.Topic)
	a.Subject = strings.TrimSpace(a.Subject)
	a.IsCorrect = a.SelectedAnswer == a.CorrectAnswer
	return nil
}

// SessionMetrics aggregates a session per spec: score, accuracy, questions
// attempted/correct/incorrect, and average time per question.
type SessionMetrics struct {
	// Score is the number of correct answers (== Correct).
	Score int `json:"score"`
	// Accuracy is percent correct over attempted (0 when nothing attempted).
	Accuracy float64 `json:"accuracy"`
	// Attempted is the number of attempts recorded.
	Attempted int `json:"attempted"`
	// Correct is the number of attempts with IsCorrect == true.
	Correct int `json:"correct"`
	// Incorrect is Attempted - Correct.
	Incorrect int `json:"incorrect"`
	// AverageTimeSeconds is the mean TimeTakenSeconds (0 when none attempted).
	AverageTimeSeconds float64 `json:"average_time_seconds"`
}

// ComputeSessionMetrics folds attempts into SessionMetrics. IsCorrect is
// trusted as stored (Validate derives it on write).
func ComputeSessionMetrics(attempts []QuizAttempt) SessionMetrics {
	m := SessionMetrics{Attempted: len(attempts)}
	if len(attempts) == 0 {
		return m
	}
	var totalTime float64
	for _, a := range attempts {
		if a.IsCorrect {
			m.Correct++
		}
		totalTime += a.TimeTakenSeconds
	}
	m.Incorrect = m.Attempted - m.Correct
	m.Score = m.Correct
	m.Accuracy = float64(m.Correct) / float64(m.Attempted) * 100
	m.AverageTimeSeconds = totalTime / float64(m.Attempted)
	return m
}

// TopicBreakdown is the per-topic accuracy row used for weak-topic analysis
// (e.g. Deadlock 82%, Paging 61%).
type TopicBreakdown struct {
	// Topic is the topic name as submitted on the attempt.
	Topic string `json:"topic"`
	// Attempted is the number of attempts tagged with this topic.
	Attempted int `json:"attempted"`
	// Correct is the number of correct attempts in this topic.
	Correct int `json:"correct"`
	// Incorrect is Attempted - Correct.
	Incorrect int `json:"incorrect"`
	// Accuracy is percent correct within this topic.
	Accuracy float64 `json:"accuracy"`
}

// ComputeTopicBreakdown groups attempts by trimmed Topic (empty topics are
// skipped) and returns rows sorted by topic name for determinism.
func ComputeTopicBreakdown(attempts []QuizAttempt) []TopicBreakdown {
	byTopic := make(map[string]*TopicBreakdown)
	for _, a := range attempts {
		t := strings.TrimSpace(a.Topic)
		if t == "" {
			continue
		}
		row, ok := byTopic[t]
		if !ok {
			row = &TopicBreakdown{Topic: t}
			byTopic[t] = row
		}
		row.Attempted++
		if a.IsCorrect {
			row.Correct++
		}
	}
	out := make([]TopicBreakdown, 0, len(byTopic))
	for _, row := range byTopic {
		row.Incorrect = row.Attempted - row.Correct
		if row.Attempted > 0 {
			row.Accuracy = float64(row.Correct) / float64(row.Attempted) * 100
		}
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Topic < out[j].Topic })
	return out
}

// WeakTopics returns the breakdown rows with Accuracy below threshold,
// sorted ascending by accuracy (weakest first). Topics with no attempts
// never appear (ComputeTopicBreakdown omits them).
func WeakTopics(breakdown []TopicBreakdown, threshold float64) []TopicBreakdown {
	out := make([]TopicBreakdown, 0)
	for _, row := range breakdown {
		if row.Accuracy < threshold {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Accuracy == out[j].Accuracy {
			return out[i].Topic < out[j].Topic
		}
		return out[i].Accuracy < out[j].Accuracy
	})
	return out
}

// SessionResult bundles a session with its attempts, aggregate metrics,
// per-topic breakdown, and weak topics (Accuracy < DefaultWeakTopicThreshold).
type SessionResult struct {
	Session   QuizSession      `json:"session"`
	Attempts  []QuizAttempt    `json:"attempts"`
	Metrics   SessionMetrics   `json:"metrics"`
	Topics    []TopicBreakdown `json:"topics"`
	WeakTopic []TopicBreakdown `json:"weak_topics"`
}

// NewSessionResult builds the read-model response for GET session endpoints.
func NewSessionResult(session QuizSession, attempts []QuizAttempt) SessionResult {
	breakdown := ComputeTopicBreakdown(attempts)
	return SessionResult{
		Session:   session,
		Attempts:  append([]QuizAttempt{}, attempts...),
		Metrics:   ComputeSessionMetrics(attempts),
		Topics:    breakdown,
		WeakTopic: WeakTopics(breakdown, DefaultWeakTopicThreshold),
	}
}
