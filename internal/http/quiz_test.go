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

	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
)

type mockQuizGenerator struct {
	called int
	got    []quiz.QuizRequest
	resp   quiz.QuizResponse
	err    error
}

func (m *mockQuizGenerator) Generate(_ context.Context, req quiz.QuizRequest) (quiz.QuizResponse, error) {
	m.called++
	m.got = append(m.got, req)
	if m.err != nil {
		return quiz.QuizResponse{}, m.err
	}
	return m.resp, nil
}

func quizOKResponse() quiz.QuizResponse {
	return quiz.QuizResponse{Questions: []quiz.QuizQuestion{
		{ID: "quiz-q1", SourceQuestionID: "q1", DocumentID: "d1", Question: "What is deadlock?", Options: []string{"A", "B", "C", "D"}, CorrectAnswer: 1, Explanation: "Because q1."},
		{ID: "quiz-q2", SourceQuestionID: "q2", DocumentID: "d1", Question: "What is paging?", Options: []string{"A", "B", "C", "D"}, CorrectAnswer: 0, Explanation: "Because q2."},
	}}
}

func TestHandleQuizGenerateGET_Success(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	handler := handleQuizGenerate(mock)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quiz/generate?query=Quiz+me+on+deadlocks&mode=mcq&length=2&difficulty=medium&topic=Deadlock&subject=Operating+Systems", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body %s", w.Code, w.Body.String())
	}
	if mock.called != 1 {
		t.Fatalf("expected generator called once, got %d", mock.called)
	}
	got := mock.got[0]
	if got.Mode != quiz.ModeMCQ {
		t.Errorf("mode = %q want mcq", got.Mode)
	}
	if got.NumQuestions != 2 {
		t.Errorf("num_questions = %d want 2", got.NumQuestions)
	}
	if got.Difficulty != "medium" {
		t.Errorf("difficulty = %q want medium", got.Difficulty)
	}
	if len(got.Topics) != 1 || got.Topics[0] != "Deadlock" {
		t.Errorf("topics = %v want [Deadlock]", got.Topics)
	}
	if got.Subject != "Operating Systems" {
		t.Errorf("subject = %q", got.Subject)
	}
	if got.Query != "Quiz me on deadlocks" {
		t.Errorf("query = %q", got.Query)
	}
	var resp quiz.QuizResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(resp.Questions))
	}
	// Source IDs preserved through the handler.
	if resp.Questions[0].SourceQuestionID != "q1" || resp.Questions[1].SourceQuestionID != "q2" {
		t.Errorf("source IDs not preserved: %+v", resp.Questions)
	}
	if resp.Questions[0].DocumentID != "d1" {
		t.Errorf("document ID not preserved: %+v", resp.Questions[0])
	}
}

func TestHandleQuizGeneratePOST_SuccessAliases(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	handler := handleQuizGenerate(mock)
	body, _ := json.Marshal(map[string]any{
		"q": "deadlock quiz", "mode": "similar", "limit": 2,
		"topic": "Deadlock", "topics": []string{"Paging"}, "difficulty": "easy", "subject": "OS",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	got := mock.got[0]
	if got.Mode != quiz.ModeSimilar {
		t.Errorf("mode = %q want similar", got.Mode)
	}
	if got.NumQuestions != 2 {
		t.Errorf("limit alias not honored: %d", got.NumQuestions)
	}
	if got.Query != "deadlock quiz" {
		t.Errorf("q alias not honored: %q", got.Query)
	}
	if len(got.Topics) != 2 {
		t.Errorf("topic+topics not merged: %v", got.Topics)
	}
}

func TestHandleQuizGeneratePOST_NumQuestionsCamelCase(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	handler := handleQuizGenerate(mock)
	body, _ := json.Marshal(map[string]any{"mode": "mcq", "numQuestions": 2})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/generate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	if mock.got[0].NumQuestions != 2 {
		t.Errorf("numQuestions camelCase not honored: %+v", mock.got[0])
	}
}

func TestHandleQuizGenerate_Validation(t *testing.T) {
	cases := []struct {
		name string
		url  string
		body string
	}{
		{"bad mode GET", "/api/v1/quiz/generate?mode=invent", ""},
		{"bad length GET", "/api/v1/quiz/generate?mode=mcq&length=100", ""},
		{"negative length GET", "/api/v1/quiz/generate?mode=mcq&length=-1", ""},
		{"bad difficulty GET", "/api/v1/quiz/generate?mode=mcq&difficulty=expert", ""},
		{"bad mode POST", "", `{"mode":"invent","num_questions":2}`},
		{"bad difficulty POST", "", `{"mode":"mcq","difficulty":"expert"}`},
		{"overlong POST", "", `{"mode":"mcq","num_questions":99}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockQuizGenerator{resp: quizOKResponse()}
			handler := handleQuizGenerate(mock)
			var req *http.Request
			if tc.body != "" {
				req = httptest.NewRequest(http.MethodPost, "/api/v1/quiz/generate", strings.NewReader(tc.body))
			} else {
				req = httptest.NewRequest(http.MethodGet, tc.url, nil)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
			}
			if mock.called != 0 {
				t.Errorf("generator must not be called on validation failure")
			}
		})
	}
}

func TestHandleQuizGenerate_MalformedRejection(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	handler := handleQuizGenerate(mock)
	for name, body := range map[string]string{
		"invalid json":   "{invalid json",
		"unknown field":  `{"mode":"mcq","bogus":1}`,
		"non-int length": `{"mode":"mcq","length":"many"}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/generate", strings.NewReader(body))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d %s", name, w.Code, w.Body.String())
			}
		})
	}
	// Non-integer length query param is also a 400.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quiz/generate?mode=mcq&length=many", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-integer length, got %d", w.Code)
	}
}

func TestHandleQuizGenerate_NoSourcesIs400(t *testing.T) {
	mock := &mockQuizGenerator{err: fmt.Errorf("quiz: no source questions match the requested filters")}
	handler := handleQuizGenerate(mock)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quiz/generate?mode=mcq&topic=UnknownTopic", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty retrieval, got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleQuizGenerate_InternalErrorIs500(t *testing.T) {
	mock := &mockQuizGenerator{err: fmt.Errorf("quiz: hybrid retrieval: connection reset")}
	handler := handleQuizGenerate(mock)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quiz/generate?mode=mcq", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleQuizGenerate_NilGenerator503(t *testing.T) {
	handler := handleQuizGenerate(nil)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/api/v1/quiz/generate?mode=mcq", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s expected 503 got %d", method, w.Code)
		}
	}
}

func TestHandleQuizGenerate_MethodNotAllowed(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	handler := handleQuizGenerate(mock)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/quiz/generate?mode=mcq", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 got %d", w.Code)
	}
	if allow := w.Header().Get("Allow"); !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") {
		t.Errorf("Allow header = %q want GET, POST", allow)
	}
}

func TestHandleQuizGenerate_LengthDifficultyTopicFiltering(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	handler := handleQuizGenerate(mock)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quiz/generate?mode=mcq&length=5&difficulty=Hard&topic=Deadlock&topics=Deadlock%2CPaging&subject=OS&q=quiz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	got := mock.got[0]
	if got.NumQuestions != 5 {
		t.Errorf("length not passed: %d", got.NumQuestions)
	}
	if got.Difficulty != "hard" {
		t.Errorf("difficulty not normalized: %q", got.Difficulty)
	}
	if len(got.Topics) < 2 {
		t.Errorf("topics not merged/deduped: %v", got.Topics)
	}
	seen := map[string]bool{}
	for _, topic := range got.Topics {
		if seen[topic] {
			t.Errorf("duplicate topic %q", topic)
		}
		seen[topic] = true
	}
}

func TestRouter_QuizRoute(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	cfg := &config.AppConfig{CORSAllowedOrigin: "*"}
	router := NewRouter(cfg, RouterDeps{Quiz: mock})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quiz/generate?mode=mcq&length=2", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("router quiz expected 200 got %d %s", w.Code, w.Body.String())
	}
	var resp quiz.QuizResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Questions) != 2 {
		t.Errorf("expected 2 questions, got %d", len(resp.Questions))
	}
}
