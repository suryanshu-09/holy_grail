package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
)

func TestHandleQuizEvaluation_MethodNotAllowed(t *testing.T) {
	eval := newFakeQuizEvaluator()
	cases := []struct {
		name    string
		handler http.Handler
		method  string
		target  string
	}{
		{"create via GET", handleCreateQuizSession(eval), http.MethodGet, "/api/v1/quiz/sessions"},
		{"get via POST", handleGetQuizSession(eval), http.MethodPost, "/api/v1/quiz/sessions/sess-1"},
		{"single submit via GET", handleSubmitQuizAttempt(eval), http.MethodGet, "/api/v1/quiz/sessions/sess-1/attempts"},
		{"bulk submit via GET", handleSubmitQuizAttemptsBulk(eval), http.MethodGet, "/api/v1/quiz/sessions/sess-1/attempts/bulk"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, nil)
			req.SetPathValue("id", "sess-1")
			w := httptest.NewRecorder()
			tc.handler.ServeHTTP(w, req)
			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected 405 got %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleQuizEvaluation_MissingSessionID(t *testing.T) {
	eval := newFakeQuizEvaluator()
	// No path value set: handlers must reject with 400.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/quiz/sessions/", nil)
	w := httptest.NewRecorder()
	handleGetQuizSession(eval).ServeHTTP(w, getReq)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("get without id: expected 400 got %d", w.Code)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions//attempts",
		strings.NewReader(`{"question_id":"q1","selected_answer":0,"correct_answer":0}`))
	w = httptest.NewRecorder()
	handleSubmitQuizAttempt(eval).ServeHTTP(w, postReq)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("submit without id: expected 400 got %d", w.Code)
	}

	bulkReq := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions//attempts/bulk",
		strings.NewReader(`{"attempts":[{"question_id":"q1","selected_answer":0,"correct_answer":0}]}`))
	w = httptest.NewRecorder()
	handleSubmitQuizAttemptsBulk(eval).ServeHTTP(w, bulkReq)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bulk without id: expected 400 got %d", w.Code)
	}
}

func TestHandleSubmitQuizAttempt_NotFound(t *testing.T) {
	eval := newFakeQuizEvaluator() // no sessions created
	body := `{"question_id":"q1","selected_answer":0,"correct_answer":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/missing/attempts", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	handleSubmitQuizAttempt(eval).ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("submit to unknown session: expected 404 got %d %s", w.Code, w.Body.String())
	}

	bulk := `{"attempts":[{"question_id":"q1","selected_answer":0,"correct_answer":0}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/missing/attempts/bulk", strings.NewReader(bulk))
	req.SetPathValue("id", "missing")
	w = httptest.NewRecorder()
	handleSubmitQuizAttemptsBulk(eval).ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("bulk to unknown session: expected 404 got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleSubmitBulk_InvalidItemIndexed(t *testing.T) {
	eval := newFakeQuizEvaluator()
	if _, err := eval.CreateSession(context.Background(), quiz.QuizSession{Mode: quiz.ModeMCQ}); err != nil {
		t.Fatal(err)
	}
	// Second item is out of bounds: error must name the item index.
	body := `{"attempts":[
		{"question_id":"q1","selected_answer":0,"correct_answer":0},
		{"question_id":"q2","selected_answer":9,"correct_answer":0}
	]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/sess-1/attempts/bulk", strings.NewReader(body))
	req.SetPathValue("id", "sess-1")
	w := httptest.NewRecorder()
	handleSubmitQuizAttemptsBulk(eval).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "attempts[1]") {
		t.Fatalf("bulk error must index the bad item, got %s", w.Body.String())
	}
}

func TestHandleSubmitBulk_MalformedBodies(t *testing.T) {
	eval := newFakeQuizEvaluator()
	if _, err := eval.CreateSession(context.Background(), quiz.QuizSession{Mode: quiz.ModeMCQ}); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"bad json":      `{invalid`,
		"missing field": `{}`,
		"null attempts": `{"attempts":null}`,
		"unknown field": `{"attempts":[],"bogus":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/sess-1/attempts/bulk", strings.NewReader(body))
			req.SetPathValue("id", "sess-1")
			w := httptest.NewRecorder()
			handleSubmitQuizAttemptsBulk(eval).ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleSubmitAttempt_MalformedBodies(t *testing.T) {
	eval := newFakeQuizEvaluator()
	if _, err := eval.CreateSession(context.Background(), quiz.QuizSession{Mode: quiz.ModeMCQ}); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"bad json":      `{invalid`,
		"empty":         ``,
		"unknown field": `{"question_id":"q1","selected_answer":0,"correct_answer":0,"bogus":1}`,
	} {
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
}

// Nil evaluator must yield 503 on every evaluation route.
func TestHandleQuizEvaluationNil_AllHandlers503(t *testing.T) {
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/quiz/sessions/sess-1", nil)
	getReq.SetPathValue("id", "sess-1")
	w := httptest.NewRecorder()
	handleGetQuizSession(nil).ServeHTTP(w, getReq)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("get nil: expected 503 got %d", w.Code)
	}

	subReq := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/sess-1/attempts",
		strings.NewReader(`{"question_id":"q1","selected_answer":0,"correct_answer":0}`))
	subReq.SetPathValue("id", "sess-1")
	w = httptest.NewRecorder()
	handleSubmitQuizAttempt(nil).ServeHTTP(w, subReq)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("submit nil: expected 503 got %d", w.Code)
	}

	bulkReq := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/sess-1/attempts/bulk",
		strings.NewReader(`{"attempts":[{"question_id":"q1","selected_answer":0,"correct_answer":0}]}`))
	bulkReq.SetPathValue("id", "sess-1")
	w = httptest.NewRecorder()
	handleSubmitQuizAttemptsBulk(nil).ServeHTTP(w, bulkReq)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("bulk nil: expected 503 got %d", w.Code)
	}
}

// Unit-test the read-model DTO mapping: metrics fields, per-topic rows,
// weak-topic names, and session-subject fallback for untagged subjects.
func TestToQuizSessionDetailDTO_Mapping(t *testing.T) {
	session := quiz.QuizSession{ID: "sess-9", Mode: quiz.ModeMCQ, Subject: "Operating Systems", TotalQuestions: 4}
	attempts := []quiz.QuizAttempt{
		{ID: "a1", SessionID: "sess-9", QuestionID: "q1", SelectedAnswer: 1, CorrectAnswer: 1, IsCorrect: true, TimeTakenSeconds: 10, Topic: "Deadlock", Subject: "Operating Systems"},
		{ID: "a2", SessionID: "sess-9", QuestionID: "q2", SelectedAnswer: 0, CorrectAnswer: 2, IsCorrect: false, TimeTakenSeconds: 20, Topic: "Deadlock"},
		{ID: "a3", SessionID: "sess-9", QuestionID: "q3", SelectedAnswer: 0, CorrectAnswer: 0, IsCorrect: true, TimeTakenSeconds: 30, Topic: "Paging"},
		{ID: "a4", SessionID: "sess-9", QuestionID: "q4", SelectedAnswer: 1, CorrectAnswer: 1, IsCorrect: true, TimeTakenSeconds: 40},
	}
	dto := toQuizSessionDetailDTO(quiz.NewSessionResult(session, attempts))

	m := dto.Metrics
	if m.Score != 3 || m.QuestionsAttempted != 4 || m.QuestionsCorrect != 3 || m.QuestionsIncorrect != 1 {
		t.Fatalf("metrics mapping wrong: %+v", m)
	}
	wantAcc := 3.0 / 4.0 * 100
	if m.Accuracy != wantAcc {
		t.Fatalf("accuracy = %v want %v", m.Accuracy, wantAcc)
	}
	if m.AverageTimeSeconds != 25 {
		t.Fatalf("avg time = %v want 25", m.AverageTimeSeconds)
	}
	if m.TotalQuestions != 4 {
		t.Fatalf("total_questions = %d want 4", m.TotalQuestions)
	}
	// Untagged attempt is excluded from per-topic rows.
	if len(dto.Topics) != 2 {
		t.Fatalf("topics mapping wrong: %+v", dto.Topics)
	}
	if dto.Topics[0].Topic != "Deadlock" || dto.Topics[0].Accuracy != 50 {
		t.Fatalf("deadlock row wrong: %+v", dto.Topics[0])
	}
	// Subject falls back to the session subject when the attempt has none.
	if dto.Topics[0].Subject != "Operating Systems" {
		t.Fatalf("subject fallback wrong: %+v", dto.Topics[0])
	}
	if len(dto.WeakTopics) != 1 || dto.WeakTopics[0] != "Deadlock" {
		t.Fatalf("weak_topics mapping wrong: %v", dto.WeakTopics)
	}
	if len(dto.Attempts) != 4 || !dto.Attempts[0].IsCorrect || dto.Attempts[1].IsCorrect {
		t.Fatalf("attempts mapping wrong: %+v", dto.Attempts)
	}
}

// Round-trip through the router: bulk submit then GET detail reflects
// metrics + weak topics end to end.
func TestRouter_QuizEvaluationBulkThenGet(t *testing.T) {
	eval := newFakeQuizEvaluator()
	router := NewRouter(&config.AppConfig{CORSAllowedOrigin: "*"}, RouterDeps{QuizEval: eval})
	create := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions",
		strings.NewReader(`{"mode":"mcq","num_questions":2,"subject":"Operating Systems"}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, create)
	if w.Code != http.StatusCreated {
		t.Fatalf("create expected 201 got %d %s", w.Code, w.Body.String())
	}

	bulk := `{"attempts":[
		{"question_id":"quiz-q1","selected_answer":1,"correct_answer":1,"time_taken_seconds":10,"topic":"Deadlock"},
		{"question_id":"quiz-q2","selected_answer":0,"correct_answer":2,"time_taken_seconds":30,"topic":"Deadlock"}
	]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/sess-1/attempts/bulk", strings.NewReader(bulk))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("bulk expected 201 got %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/quiz/sessions/sess-1", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get expected 200 got %d %s", w.Code, w.Body.String())
	}
	var detail quizSessionDetailDTO
	if err := json.NewDecoder(w.Body).Decode(&detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.Metrics.QuestionsAttempted != 2 || detail.Metrics.Score != 1 || detail.Metrics.Accuracy != 50 {
		t.Fatalf("router metrics wrong: %+v", detail.Metrics)
	}
	if detail.Metrics.AverageTimeSeconds != 20 {
		t.Fatalf("router avg time = %v want 20", detail.Metrics.AverageTimeSeconds)
	}
	if len(detail.WeakTopics) != 1 || detail.WeakTopics[0] != "Deadlock" {
		t.Fatalf("router weak topics wrong: %v", detail.WeakTopics)
	}
}
