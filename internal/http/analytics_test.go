package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
)

// fakeHistoryLister is an in-memory QuizHistoryLister for analytics tests.
type fakeHistoryLister struct {
	sessions []quiz.QuizSession
	err      error
}

func (f *fakeHistoryLister) ListSessions(_ context.Context, limit, offset int) ([]quiz.QuizSession, error) {
	if f.err != nil {
		return nil, f.err
	}
	if offset >= len(f.sessions) {
		return []quiz.QuizSession{}, nil
	}
	end := offset + limit
	if end > len(f.sessions) {
		end = len(f.sessions)
	}
	return f.sessions[offset:end], nil
}

// fakeQuestionGetter resolves question records for difficulty enrichment.
type fakeQuestionGetter struct {
	byID map[string]questions.Question
}

func (f *fakeQuestionGetter) Get(_ context.Context, id string) (questions.Question, error) {
	if q, ok := f.byID[id]; ok {
		return q, nil
	}
	return questions.Question{}, errTestNotFound
}

type testNotFound string

func (e testNotFound) Error() string { return string(e) }

const errTestNotFound = testNotFound("not found")

// seedAnalyticsEval builds an evaluator with one session holding two
// attempts (one correct Deadlock, one incorrect Paging).
func seedAnalyticsEval(t *testing.T) *fakeQuizEvaluator {
	t.Helper()
	eval := newFakeQuizEvaluator()
	sess, err := eval.CreateSession(context.Background(), quiz.QuizSession{Mode: quiz.ModeMCQ, TotalQuestions: 2})
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	eval.attempts[sess.ID] = []quiz.QuizAttempt{
		{ID: "a1", SessionID: sess.ID, QuestionID: "quiz-q1", SourceQuestionID: "q1", SelectedAnswer: 1, CorrectAnswer: 1, IsCorrect: true, TimeTakenSeconds: 30, Topic: "Deadlock", CreatedAt: day},
		{ID: "a2", SessionID: sess.ID, QuestionID: "quiz-q2", SourceQuestionID: "q2", SelectedAnswer: 0, CorrectAnswer: 2, IsCorrect: false, TimeTakenSeconds: 60, Topic: "Paging", CreatedAt: day},
	}
	return eval
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
}

func TestAnalyticsMasterySessionScope(t *testing.T) {
	deps := AnalyticsDeps{Eval: seedAnalyticsEval(t)}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/mastery?session_id=sess-1", nil)
	w := httptest.NewRecorder()
	handleAnalyticsMastery(deps).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Scope  analyticsScope `json:"scope"`
		Topics []struct {
			Topic   string  `json:"topic"`
			Mastery float64 `json:"mastery"`
			Level   string  `json:"level"`
		} `json:"topics"`
	}
	decodeBody(t, w, &body)
	if body.Scope.Attempts != 2 || body.Scope.Sessions != 1 || body.Scope.SessionID != "sess-1" {
		t.Fatalf("scope wrong: %+v", body.Scope)
	}
	if len(body.Topics) != 2 {
		t.Fatalf("expected 2 topics, got %+v", body.Topics)
	}
}

func TestAnalyticsHistoryAndTiming(t *testing.T) {
	deps := AnalyticsDeps{Eval: seedAnalyticsEval(t)}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/history?session_id=sess-1", nil)
	w := httptest.NewRecorder()
	handleAnalyticsHistory(deps).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("history expected 200 got %d %s", w.Code, w.Body.String())
	}
	var history struct {
		Days []struct {
			Date     string  `json:"date"`
			Accuracy float64 `json:"accuracy"`
		} `json:"days"`
	}
	decodeBody(t, w, &history)
	if len(history.Days) != 1 || history.Days[0].Date != "2026-09-21" || history.Days[0].Accuracy != 50 {
		t.Fatalf("history wrong: %+v", history.Days)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/analytics/timing?session_id=sess-1", nil)
	w = httptest.NewRecorder()
	handleAnalyticsTiming(deps).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("timing expected 200 got %d %s", w.Code, w.Body.String())
	}
	var timing struct {
		Timing struct {
			Count  int     `json:"count"`
			Avg    float64 `json:"avg_seconds"`
			Median float64 `json:"median_seconds"`
			P90    float64 `json:"p90_seconds"`
		} `json:"timing"`
	}
	decodeBody(t, w, &timing)
	if timing.Timing.Count != 2 || timing.Timing.Avg != 45 || timing.Timing.Median != 45 {
		t.Fatalf("timing wrong: %+v", timing.Timing)
	}
}

func TestAnalyticsDifficultyWithQuestions(t *testing.T) {
	easy, hard := "easy", "hard"
	deps := AnalyticsDeps{
		Eval: seedAnalyticsEval(t),
		Questions: &fakeQuestionGetter{byID: map[string]questions.Question{
			"q1": {ID: "q1", Difficulty: &easy},
			"q2": {ID: "q2", Difficulty: &hard},
		}},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/difficulty?session_id=sess-1", nil)
	w := httptest.NewRecorder()
	handleAnalyticsDifficulty(deps).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Difficulties []struct {
			Difficulty string  `json:"difficulty"`
			Accuracy   float64 `json:"accuracy"`
		} `json:"difficulties"`
	}
	decodeBody(t, w, &body)
	if len(body.Difficulties) != 2 {
		t.Fatalf("expected 2 difficulty rows, got %+v", body.Difficulties)
	}

	// Without a Questions getter everything groups under unknown.
	deps.Questions = nil
	req = httptest.NewRequest(http.MethodGet, "/api/v1/analytics/difficulty?session_id=sess-1", nil)
	w = httptest.NewRecorder()
	handleAnalyticsDifficulty(deps).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	body.Difficulties = nil
	decodeBody(t, w, &body)
	if len(body.Difficulties) != 1 || body.Difficulties[0].Difficulty != "unknown" {
		t.Fatalf("expected one unknown row, got %+v", body.Difficulties)
	}
}

func TestAnalyticsReadinessUserScope(t *testing.T) {
	eval := seedAnalyticsEval(t)
	deps := AnalyticsDeps{
		Eval:    eval,
		History: &fakeHistoryLister{sessions: []quiz.QuizSession{{ID: "sess-1"}}},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/readiness?total_topics=4", nil)
	w := httptest.NewRecorder()
	handleAnalyticsReadiness(deps).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Scope     analyticsScope `json:"scope"`
		Readiness struct {
			Score float64 `json:"score"`
			Band  string  `json:"band"`
		} `json:"readiness"`
	}
	decodeBody(t, w, &body)
	if body.Scope.Sessions != 1 || body.Scope.Attempts != 2 {
		t.Fatalf("scope wrong: %+v", body.Scope)
	}
	if body.Readiness.Band == "" {
		t.Fatalf("readiness band missing: %+v", body.Readiness)
	}

	// Invalid total_topics maps to 400.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/analytics/readiness?total_topics=nope", nil)
	w = httptest.NewRecorder()
	handleAnalyticsReadiness(deps).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestAnalyticsUnavailableAndNotFound(t *testing.T) {
	// Nil evaluator maps to 503 on every endpoint.
	handlers := []http.Handler{
		handleAnalyticsMastery(AnalyticsDeps{}),
		handleAnalyticsHistory(AnalyticsDeps{}),
		handleAnalyticsDifficulty(AnalyticsDeps{}),
		handleAnalyticsTiming(AnalyticsDeps{}),
		handleAnalyticsReadiness(AnalyticsDeps{}),
	}
	for i, h := range handlers {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/mastery?session_id=sess-1", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("handler %d: expected 503 got %d", i, w.Code)
		}
	}
	// User scope without a history lister also maps to 503.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/mastery", nil)
	w := httptest.NewRecorder()
	handleAnalyticsMastery(AnalyticsDeps{Eval: seedAnalyticsEval(t)}).ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 got %d %s", w.Code, w.Body.String())
	}
	// Unknown session maps to 404.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/analytics/mastery?session_id=missing", nil)
	w = httptest.NewRecorder()
	handleAnalyticsMastery(AnalyticsDeps{Eval: seedAnalyticsEval(t)}).ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 got %d %s", w.Code, w.Body.String())
	}
	// Wrong method maps to 405.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/analytics/mastery?session_id=sess-1", nil)
	w = httptest.NewRecorder()
	handleAnalyticsMastery(AnalyticsDeps{Eval: seedAnalyticsEval(t)}).ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 got %d %s", w.Code, w.Body.String())
	}
}
