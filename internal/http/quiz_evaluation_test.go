package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
)

// fakeQuizEvaluator is an in-memory QuizEvaluator for handler tests.
type fakeQuizEvaluator struct {
	sessions map[string]quiz.QuizSession
	attempts map[string][]quiz.QuizAttempt
	err      error
}

func newFakeQuizEvaluator() *fakeQuizEvaluator {
	return &fakeQuizEvaluator{
		sessions: map[string]quiz.QuizSession{},
		attempts: map[string][]quiz.QuizAttempt{},
	}
}

func (f *fakeQuizEvaluator) CreateSession(_ context.Context, s quiz.QuizSession) (quiz.QuizSession, error) {
	if f.err != nil {
		return quiz.QuizSession{}, f.err
	}
	if err := s.Validate(); err != nil {
		return quiz.QuizSession{}, err
	}
	s.ID = "sess-1"
	s.CreatedAt = time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	f.sessions[s.ID] = s
	return s, nil
}

func (f *fakeQuizEvaluator) SubmitAttempt(_ context.Context, a quiz.QuizAttempt) (quiz.QuizAttempt, error) {
	if f.err != nil {
		return quiz.QuizAttempt{}, f.err
	}
	if err := a.Validate(); err != nil {
		return quiz.QuizAttempt{}, err
	}
	if _, ok := f.sessions[a.SessionID]; !ok {
		return quiz.QuizAttempt{}, apperr.ErrNotFound
	}
	a.ID = "att-" + a.QuestionID
	f.attempts[a.SessionID] = append(f.attempts[a.SessionID], a)
	return a, nil
}

func (f *fakeQuizEvaluator) SubmitAttempts(_ context.Context, sessionID string, attempts []quiz.QuizAttempt) ([]quiz.QuizAttempt, error) {
	if f.err != nil {
		return nil, f.err
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if len(attempts) == 0 {
		return nil, fmt.Errorf("attempts must contain at least 1 item")
	}
	out := make([]quiz.QuizAttempt, 0, len(attempts))
	for i, a := range attempts {
		a.SessionID = strings.TrimSpace(sessionID)
		rec, err := f.SubmitAttempt(context.Background(), a)
		if err != nil {
			return nil, fmt.Errorf("attempts[%d]: %w", i, err)
		}
		out = append(out, rec)
	}
	return out, nil
}

func (f *fakeQuizEvaluator) GetResult(_ context.Context, sessionID string) (quiz.SessionResult, error) {
	if f.err != nil {
		return quiz.SessionResult{}, f.err
	}
	s, ok := f.sessions[strings.TrimSpace(sessionID)]
	if !ok {
		return quiz.SessionResult{}, apperr.ErrNotFound
	}
	return quiz.NewSessionResult(s, append([]quiz.QuizAttempt{}, f.attempts[s.ID]...)), nil
}

func TestHandleCreateQuizSession(t *testing.T) {
	eval := newFakeQuizEvaluator()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions",
		strings.NewReader(`{"mode":"mcq","num_questions":5,"subject":"Operating Systems"}`))
	w := httptest.NewRecorder()
	handleCreateQuizSession(eval).ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d %s", w.Code, w.Body.String())
	}
	var dto quizSessionDTO
	if err := json.NewDecoder(w.Body).Decode(&dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.ID != "sess-1" || dto.Mode != "mcq" || dto.TotalQuestions != 5 {
		t.Fatalf("unexpected session dto: %+v", dto)
	}
}

func TestHandleCreateQuizSessionValidation(t *testing.T) {
	eval := newFakeQuizEvaluator()
	for name, body := range map[string]string{
		"bad mode": `{"mode":"invent"}`,
		"overlong": `{"mode":"mcq","num_questions":99}`,
		"bad json": `{invalid`,
		"unknown":  `{"mode":"mcq","bogus":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions", strings.NewReader(body))
			w := httptest.NewRecorder()
			handleCreateQuizSession(eval).ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleSubmitAttemptAndGetResult(t *testing.T) {
	eval := newFakeQuizEvaluator()
	if _, err := eval.CreateSession(context.Background(), quiz.QuizSession{Mode: quiz.ModeMCQ, TotalQuestions: 2}); err != nil {
		t.Fatal(err)
	}
	// Single submit.
	body := `{"question_id":"quiz-q1","selected_answer":1,"correct_answer":1,"time_taken_seconds":12,"topic":"Deadlock","subject":"Operating Systems"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/sess-1/attempts", strings.NewReader(body))
	req.SetPathValue("id", "sess-1")
	w := httptest.NewRecorder()
	handleSubmitQuizAttempt(eval).ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("submit expected 201 got %d %s", w.Code, w.Body.String())
	}
	var att quizAttemptDTO
	if err := json.NewDecoder(w.Body).Decode(&att); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !att.IsCorrect {
		t.Fatalf("is_correct must be derived: %+v", att)
	}
	// Bulk submit with one wrong answer.
	bulk := `{"attempts":[{"question_id":"quiz-q2","selected_answer":0,"correct_answer":2,"time_taken_seconds":18,"topic":"Paging"}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/sess-1/attempts/bulk", strings.NewReader(bulk))
	req.SetPathValue("id", "sess-1")
	w = httptest.NewRecorder()
	handleSubmitQuizAttemptsBulk(eval).ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("bulk expected 201 got %d %s", w.Code, w.Body.String())
	}
	// GET detail: metrics + topics + weak topics.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/quiz/sessions/sess-1", nil)
	req.SetPathValue("id", "sess-1")
	w = httptest.NewRecorder()
	handleGetQuizSession(eval).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get expected 200 got %d %s", w.Code, w.Body.String())
	}
	var detail quizSessionDetailDTO
	if err := json.NewDecoder(w.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Metrics.QuestionsAttempted != 2 || detail.Metrics.QuestionsCorrect != 1 ||
		detail.Metrics.QuestionsIncorrect != 1 || detail.Metrics.Score != 1 {
		t.Fatalf("metrics wrong: %+v", detail.Metrics)
	}
	if detail.Metrics.AverageTimeSeconds != 15 {
		t.Fatalf("avg time = %v want 15", detail.Metrics.AverageTimeSeconds)
	}
	if detail.Metrics.TotalQuestions != 2 {
		t.Fatalf("total_questions = %d want 2", detail.Metrics.TotalQuestions)
	}
	if len(detail.Topics) != 2 {
		t.Fatalf("topics wrong: %+v", detail.Topics)
	}
	if len(detail.WeakTopics) != 1 || detail.WeakTopics[0] != "Paging" {
		t.Fatalf("weak_topics = %v want [Paging]", detail.WeakTopics)
	}
}

func TestHandleSubmitAttemptValidation(t *testing.T) {
	eval := newFakeQuizEvaluator()
	if _, err := eval.CreateSession(context.Background(), quiz.QuizSession{Mode: quiz.ModeMCQ}); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"missing question": `{"selected_answer":0,"correct_answer":0}`,
		"missing answers":  `{"question_id":"q1"}`,
		"out of bounds":    `{"question_id":"q1","selected_answer":9,"correct_answer":0}`,
		"negative time":    `{"question_id":"q1","selected_answer":0,"correct_answer":0,"time_taken_seconds":-1}`,
		"empty bulk":       ``, // handled separately below
	}
	for name, body := range cases {
		if body == "" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/sess-1/attempts", strings.NewReader(body))
			req.SetPathValue("id", "sess-1")
			w := httptest.NewRecorder()
			handleSubmitQuizAttempt(eval).ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
			}
		})
	}
	// Empty bulk is a 400.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/sess-1/attempts/bulk",
		bytes.NewReader([]byte(`{"attempts":[]}`)))
	req.SetPathValue("id", "sess-1")
	w := httptest.NewRecorder()
	handleSubmitQuizAttemptsBulk(eval).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty bulk expected 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleGetQuizSessionNotFound(t *testing.T) {
	eval := newFakeQuizEvaluator()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quiz/sessions/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	handleGetQuizSession(eval).ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleQuizEvaluationNilService503(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handleCreateQuizSession(nil).ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 got %d", w.Code)
	}
}

func TestRouter_QuizEvaluationRoutes(t *testing.T) {
	eval := newFakeQuizEvaluator()
	cfg := &config.AppConfig{CORSAllowedOrigin: "*"}
	router := NewRouter(cfg, RouterDeps{QuizEval: eval})
	// Create.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions",
		strings.NewReader(`{"mode":"mcq","num_questions":1}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("router create expected 201 got %d %s", w.Code, w.Body.String())
	}
	// Get.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/quiz/sessions/sess-1", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("router get expected 200 got %d %s", w.Code, w.Body.String())
	}
	var detail quizSessionDetailDTO
	if err := json.NewDecoder(w.Body).Decode(&detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.Metrics.QuestionsAttempted != 0 || detail.Metrics.Score != 0 {
		t.Fatalf("fresh session metrics must be zero: %+v", detail.Metrics)
	}
}
