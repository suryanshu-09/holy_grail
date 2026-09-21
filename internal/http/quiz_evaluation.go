package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
)

// QuizEvaluator is the Phase 15 evaluation contract: record attempts and
// compute session metrics (score, accuracy, attempted, correct, incorrect,
// average time) plus per-topic accuracy and weak topics.
// *quiz.EvaluationService satisfies it, so it can be wired directly.
type QuizEvaluator interface {
	CreateSession(ctx context.Context, session quiz.QuizSession) (quiz.QuizSession, error)
	SubmitAttempt(ctx context.Context, attempt quiz.QuizAttempt) (quiz.QuizAttempt, error)
	SubmitAttempts(ctx context.Context, sessionID string, attempts []quiz.QuizAttempt) ([]quiz.QuizAttempt, error)
	GetResult(ctx context.Context, sessionID string) (quiz.SessionResult, error)
}

// createQuizSessionRequest is the JSON body for POST /api/v1/quiz/sessions.
// Topic/topics are accepted (quiz generation context) but stored per-attempt;
// the session itself records mode/subject/total_questions.
type createQuizSessionRequest struct {
	Mode         string   `json:"mode"`
	NumQuestions *int     `json:"num_questions"`
	Subject      string   `json:"subject"`
	Topic        string   `json:"topic"`
	Topics       []string `json:"topics"`
}

// quizSessionDTO is the wire shape for a session (matches lib/api.ts QuizSession).
type quizSessionDTO struct {
	ID             string `json:"id"`
	Mode           string `json:"mode,omitempty"`
	Subject        string `json:"subject,omitempty"`
	TotalQuestions int    `json:"total_questions"`
	CreatedAt      string `json:"created_at,omitempty"`
}

func toQuizSessionDTO(s quiz.QuizSession) quizSessionDTO {
	dto := quizSessionDTO{
		ID:             s.ID,
		Mode:           string(s.Mode),
		Subject:        s.Subject,
		TotalQuestions: s.TotalQuestions,
	}
	if !s.CreatedAt.IsZero() {
		dto.CreatedAt = s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return dto
}

// quizAttemptRequest is the JSON body for a single attempt. Pointers detect
// missing/null selected_answer / correct_answer so they map to 400.
type quizAttemptRequest struct {
	QuestionID       string   `json:"question_id"`
	SourceQuestionID string   `json:"source_question_id"`
	QuestionText     string   `json:"question_text"`
	SelectedAnswer   *int     `json:"selected_answer"`
	CorrectAnswer    *int     `json:"correct_answer"`
	Topic            string   `json:"topic"`
	Subject          string   `json:"subject"`
	TimeTakenSeconds *float64 `json:"time_taken_seconds"`
}

func (r *quizAttemptRequest) toAttempt(sessionID string) (quiz.QuizAttempt, error) {
	if r == nil {
		return quiz.QuizAttempt{}, errors.New("attempt is required")
	}
	if strings.TrimSpace(r.QuestionID) == "" {
		return quiz.QuizAttempt{}, errors.New("question_id is required")
	}
	if r.SelectedAnswer == nil {
		return quiz.QuizAttempt{}, errors.New("selected_answer is required")
	}
	if r.CorrectAnswer == nil {
		return quiz.QuizAttempt{}, errors.New("correct_answer is required")
	}
	timeTaken := 0.0
	if r.TimeTakenSeconds != nil {
		timeTaken = *r.TimeTakenSeconds
	}
	a := quiz.QuizAttempt{
		SessionID:        strings.TrimSpace(sessionID),
		QuestionID:       strings.TrimSpace(r.QuestionID),
		SourceQuestionID: strings.TrimSpace(r.SourceQuestionID),
		QuestionText:     strings.TrimSpace(r.QuestionText),
		SelectedAnswer:   *r.SelectedAnswer,
		CorrectAnswer:    *r.CorrectAnswer,
		Topic:            strings.TrimSpace(r.Topic),
		Subject:          strings.TrimSpace(r.Subject),
		TimeTakenSeconds: timeTaken,
	}
	if err := a.Validate(); err != nil {
		return quiz.QuizAttempt{}, err
	}
	return a, nil
}

// quizAttemptDTO is the wire shape for an attempt (matches lib/api.ts QuizAttempt).
type quizAttemptDTO struct {
	ID               string  `json:"id"`
	SessionID        string  `json:"session_id"`
	QuestionID       string  `json:"question_id"`
	SourceQuestionID string  `json:"source_question_id,omitempty"`
	QuestionText     string  `json:"question_text,omitempty"`
	SelectedAnswer   int     `json:"selected_answer"`
	CorrectAnswer    int     `json:"correct_answer"`
	IsCorrect        bool    `json:"is_correct"`
	TimeTakenSeconds float64 `json:"time_taken_seconds"`
	Topic            string  `json:"topic,omitempty"`
	Subject          string  `json:"subject,omitempty"`
}

func toQuizAttemptDTO(a quiz.QuizAttempt) quizAttemptDTO {
	return quizAttemptDTO{
		ID:               a.ID,
		SessionID:        a.SessionID,
		QuestionID:       a.QuestionID,
		SourceQuestionID: a.SourceQuestionID,
		QuestionText:     a.QuestionText,
		SelectedAnswer:   a.SelectedAnswer,
		CorrectAnswer:    a.CorrectAnswer,
		IsCorrect:        a.IsCorrect,
		TimeTakenSeconds: a.TimeTakenSeconds,
		Topic:            a.Topic,
		Subject:          a.Subject,
	}
}

// quizMetricsDTO is the wire metrics shape (matches lib/api.ts QuizMetrics).
type quizMetricsDTO struct {
	Score              int     `json:"score"`
	Accuracy           float64 `json:"accuracy"`
	QuestionsAttempted int     `json:"questions_attempted"`
	QuestionsCorrect   int     `json:"questions_correct"`
	QuestionsIncorrect int     `json:"questions_incorrect"`
	AverageTimeSeconds float64 `json:"average_time_seconds"`
	TotalQuestions     int     `json:"total_questions"`
}

// quizTopicDTO is the wire per-topic row (matches lib/api.ts TopicMetric).
type quizTopicDTO struct {
	Topic     string  `json:"topic"`
	Subject   string  `json:"subject,omitempty"`
	Attempted int     `json:"attempted"`
	Correct   int     `json:"correct"`
	Incorrect int     `json:"incorrect"`
	Accuracy  float64 `json:"accuracy"`
}

// quizSessionDetailDTO is the GET session response
// (matches lib/api.ts QuizSessionDetail).
type quizSessionDetailDTO struct {
	Session    quizSessionDTO   `json:"session"`
	Attempts   []quizAttemptDTO `json:"attempts"`
	Metrics    quizMetricsDTO   `json:"metrics"`
	Topics     []quizTopicDTO   `json:"topics"`
	WeakTopics []string         `json:"weak_topics"`
}

func toQuizSessionDetailDTO(res quiz.SessionResult) quizSessionDetailDTO {
	attempts := make([]quizAttemptDTO, 0, len(res.Attempts))
	for _, a := range res.Attempts {
		attempts = append(attempts, toQuizAttemptDTO(a))
	}
	// Resolve a subject per topic from the first tagged attempt; fall back
	// to the session subject so the UI can group by subject when present.
	subjectByTopic := make(map[string]string, len(res.Topics))
	for _, a := range res.Attempts {
		t := strings.TrimSpace(a.Topic)
		if t == "" {
			continue
		}
		if _, ok := subjectByTopic[t]; !ok {
			sub := strings.TrimSpace(a.Subject)
			if sub == "" {
				sub = strings.TrimSpace(res.Session.Subject)
			}
			subjectByTopic[t] = sub
		}
	}
	topics := make([]quizTopicDTO, 0, len(res.Topics))
	for _, tb := range res.Topics {
		topics = append(topics, quizTopicDTO{
			Topic:     tb.Topic,
			Subject:   subjectByTopic[tb.Topic],
			Attempted: tb.Attempted,
			Correct:   tb.Correct,
			Incorrect: tb.Incorrect,
			Accuracy:  tb.Accuracy,
		})
	}
	weak := make([]string, 0, len(res.WeakTopic))
	for _, w := range res.WeakTopic {
		weak = append(weak, w.Topic)
	}
	m := res.Metrics
	return quizSessionDetailDTO{
		Session:  toQuizSessionDTO(res.Session),
		Attempts: attempts,
		Metrics: quizMetricsDTO{
			Score:              m.Score,
			Accuracy:           m.Accuracy,
			QuestionsAttempted: m.Attempted,
			QuestionsCorrect:   m.Correct,
			QuestionsIncorrect: m.Incorrect,
			AverageTimeSeconds: m.AverageTimeSeconds,
			TotalQuestions:     res.Session.TotalQuestions,
		},
		Topics:     topics,
		WeakTopics: weak,
	}
}

// isEvaluationValidationError reports whether err is a client (400) error.
func isEvaluationValidationError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{
		"is required", "invalid", "out of range", "out of bounds",
		"must be", "must contain", "at least",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func writeEvaluationError(w http.ResponseWriter, err error) {
	if errors.Is(err, apperr.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "quiz session not found")
		return
	}
	if isEvaluationValidationError(err) {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.LogError("quiz evaluation failed", err)
	httpx.Error(w, http.StatusInternalServerError, "quiz evaluation failed")
}

// handleCreateQuizSession handles POST /api/v1/quiz/sessions.
func handleCreateQuizSession(eval QuizEvaluator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if eval == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "quiz evaluation not configured")
			return
		}
		var body createQuizSessionRequest
		if r.ContentLength != 0 {
			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()
			if err := dec.Decode(&body); err != nil {
				httpx.Error(w, http.StatusBadRequest, "invalid request body")
				return
			}
		}
		session := quiz.QuizSession{
			Mode:    quiz.QuizMode(strings.TrimSpace(body.Mode)),
			Subject: strings.TrimSpace(body.Subject),
		}
		if body.NumQuestions != nil {
			session.TotalQuestions = *body.NumQuestions
		}
		created, err := eval.CreateSession(r.Context(), session)
		if err != nil {
			writeEvaluationError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, toQuizSessionDTO(created))
	})
}

// handleGetQuizSession handles GET /api/v1/quiz/sessions/{id} with metrics,
// per-topic breakdown, and weak topics.
func handleGetQuizSession(eval QuizEvaluator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if eval == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "quiz evaluation not configured")
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			httpx.Error(w, http.StatusBadRequest, "session id is required")
			return
		}
		res, err := eval.GetResult(r.Context(), id)
		if err != nil {
			writeEvaluationError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, toQuizSessionDetailDTO(res))
	})
}

// handleSubmitQuizAttempt handles POST /api/v1/quiz/sessions/{id}/attempts.
func handleSubmitQuizAttempt(eval QuizEvaluator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if eval == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "quiz evaluation not configured")
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			httpx.Error(w, http.StatusBadRequest, "session id is required")
			return
		}
		var body quizAttemptRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
		attempt, err := body.toAttempt(id)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		recorded, err := eval.SubmitAttempt(r.Context(), attempt)
		if err != nil {
			writeEvaluationError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, toQuizAttemptDTO(recorded))
	})
}

// handleSubmitQuizAttemptsBulk handles POST /api/v1/quiz/sessions/{id}/attempts/bulk.
func handleSubmitQuizAttemptsBulk(eval QuizEvaluator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if eval == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "quiz evaluation not configured")
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			httpx.Error(w, http.StatusBadRequest, "session id is required")
			return
		}
		var body struct {
			Attempts []quizAttemptRequest `json:"attempts"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if len(body.Attempts) == 0 {
			httpx.Error(w, http.StatusBadRequest, "attempts must contain at least 1 item")
			return
		}
		attempts := make([]quiz.QuizAttempt, 0, len(body.Attempts))
		for i, raw := range body.Attempts {
			a, err := raw.toAttempt(id)
			if err != nil {
				httpx.Error(w, http.StatusBadRequest, "attempts["+strconv.Itoa(i)+"]: "+err.Error())
				return
			}
			attempts = append(attempts, a)
		}
		recorded, err := eval.SubmitAttempts(r.Context(), id, attempts)
		if err != nil {
			writeEvaluationError(w, err)
			return
		}
		dtos := make([]quizAttemptDTO, 0, len(recorded))
		for _, a := range recorded {
			dtos = append(dtos, toQuizAttemptDTO(a))
		}
		httpx.WriteJSON(w, http.StatusCreated, dtos)
	})
}
