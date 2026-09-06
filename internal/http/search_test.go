package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/search"
)

type mockSearcher struct {
	called bool
	query  string
	filter search.Filter
	resp   search.Response
	err    error
}

func (m *mockSearcher) Search(_ context.Context, query string, filter search.Filter) (search.Response, error) {
	m.called = true
	m.query = query
	m.filter = filter
	if m.err != nil {
		return search.Response{}, m.err
	}
	// echo back
	if m.resp.Results == nil {
		m.resp.Results = []search.Result{}
	}
	m.resp.Query = query
	m.resp.Count = len(m.resp.Results)
	if m.resp.Metric == "" {
		m.resp.Metric = search.DefaultMetric
	}
	return m.resp, nil
}

func TestHandleSearchGET_Success(t *testing.T) {
	qt := "Explain deadlock"
	q := questions.Question{ID: "q1", QuestionText: &qt}
	mock := &mockSearcher{resp: search.Response{Results: []search.Result{{Question: q, Similarity: 0.95, Distance: 0.05}}}}
	handler := handleSearch(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=deadlock&subject=Operating%20Systems&year_min=2020&threshold=0.7&limit=5", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body %s", w.Code, w.Body.String())
	}
	if !mock.called || mock.query != "deadlock" {
		t.Fatalf("search not called or query mismatch: %q", mock.query)
	}
	if mock.filter.Subject != "Operating Systems" {
		t.Errorf("subject filter = %q want Operating Systems", mock.filter.Subject)
	}
	if mock.filter.YearMin == nil || *mock.filter.YearMin != 2020 {
		t.Errorf("year_min filter mismatch: %v", mock.filter.YearMin)
	}
	if mock.filter.Threshold == nil || *mock.filter.Threshold != 0.7 {
		t.Errorf("threshold mismatch: %v", mock.filter.Threshold)
	}
	if mock.filter.Limit != 5 {
		t.Errorf("limit = %d want 5", mock.filter.Limit)
	}
	var resp search.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Question.ID != "q1" {
		t.Errorf("response results mismatch: %v", resp.Results)
	}
}

func TestHandleSearchGET_MissingQuery(t *testing.T) {
	mock := &mockSearcher{}
	handler := handleSearch(mock)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?subject=OS", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing query, got %d", w.Code)
	}
	if mock.called {
		t.Errorf("search should not be called on missing query")
	}
}

func TestHandleSearchGET_InvalidThreshold(t *testing.T) {
	mock := &mockSearcher{}
	handler := handleSearch(mock)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=test&threshold=2", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid threshold, got %d", w.Code)
	}
}

func TestHandleSearchGET_NoSearcher(t *testing.T) {
	handler := handleSearch(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when searcher nil, got %d", w.Code)
	}
}

func TestHandleSearchPOST_Success(t *testing.T) {
	qt := "What is paging?"
	q := questions.Question{ID: "q2", QuestionText: &qt}
	mock := &mockSearcher{resp: search.Response{Results: []search.Result{{Question: q, Similarity: 0.88, Distance: 0.12}}}}
	handler := handleSearch(mock)
	body, _ := json.Marshal(map[string]interface{}{"query": "paging", "subject": "Operating Systems", "threshold": 0.5, "limit": 10})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST expected 200 got %d %s", w.Code, w.Body.String())
	}
	if !mock.called || mock.query != "paging" {
		t.Fatalf("POST query mismatch: %q", mock.query)
	}
	if mock.filter.Subject != "Operating Systems" {
		t.Errorf("POST subject filter mismatch")
	}
}

func TestHandleSearchPOST_CamelCase(t *testing.T) {
	mock := &mockSearcher{resp: search.Response{Results: []search.Result{}}}
	handler := handleSearch(mock)
	body, _ := json.Marshal(map[string]interface{}{"q": "deadlock", "topicId": "t1", "documentId": "d1", "yearMin": 2020})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST camelCase expected 200 got %d %s", w.Code, w.Body.String())
	}
	if mock.filter.TopicID != "t1" {
		t.Errorf("topicId camelCase not parsed: %q", mock.filter.TopicID)
	}
	if mock.filter.DocumentID != "d1" {
		t.Errorf("documentId camelCase not parsed: %q", mock.filter.DocumentID)
	}
	if mock.filter.YearMin == nil || *mock.filter.YearMin != 2020 {
		t.Errorf("yearMin camelCase not parsed: %v", mock.filter.YearMin)
	}
}

func TestHandleSearchPOST_InvalidBody(t *testing.T) {
	mock := &mockSearcher{}
	handler := handleSearch(mock)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/search", bytes.NewReader([]byte("{invalid json")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid json, got %d", w.Code)
	}
}

func TestHandleSearch_MethodNotAllowed(t *testing.T) {
	mock := &mockSearcher{}
	handler := handleSearch(mock)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/search?q=test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandleSearchGET_AllFilters(t *testing.T) {
	mock := &mockSearcher{resp: search.Response{Results: []search.Result{}}}
	handler := handleSearch(mock)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=test&topic=Deadlock&topic_id=t1&document_id=d1&question_type=mcq&difficulty=hard&year=2022&year_min=2020&year_max=2023", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	if mock.filter.Topic != "Deadlock" {
		t.Errorf("topic = %q want Deadlock", mock.filter.Topic)
	}
	if mock.filter.TopicID != "t1" {
		t.Errorf("topic_id = %q want t1", mock.filter.TopicID)
	}
	if mock.filter.DocumentID != "d1" {
		t.Errorf("document_id = %q want d1", mock.filter.DocumentID)
	}
	if mock.filter.QuestionType != "mcq" {
		t.Errorf("question_type = %q want mcq", mock.filter.QuestionType)
	}
	if mock.filter.Difficulty != "hard" {
		t.Errorf("difficulty = %q want hard", mock.filter.Difficulty)
	}
	if mock.filter.Year == nil || *mock.filter.Year != 2022 {
		t.Errorf("year filter mismatch")
	}
}

func TestRouter_SearchRoute(t *testing.T) {
	mock := &mockSearcher{resp: search.Response{Results: []search.Result{}}}
	cfg := &config.AppConfig{CORSAllowedOrigin: "*"}
	deps := RouterDeps{Searcher: mock}
	router := NewRouter(cfg, deps)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=hello", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("router search expected 200 got %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected CORS header *, got %q", got)
	}
}
