package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/search"
)

// Phase 27 wire-up: AdaptiveOptions into the quiz handler (optional
// query/body params, defaults off for backward compat).

func TestQuizGenerateGET_AdaptiveDefaultsOff(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	handler := handleQuizGenerate(mock)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quiz/generate?mode=mcq&length=2", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body %s", w.Code, w.Body.String())
	}
	if mock.got[0].Adaptive != nil {
		t.Errorf("default request should have nil Adaptive, got %+v", mock.got[0].Adaptive)
	}
}

func TestQuizGenerateGET_AdaptiveParams(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	handler := handleQuizGenerate(mock)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quiz/generate?mode=mcq&length=2&topic=Algebra&adaptive=true&weak_topics=Geometry&review_due=q9", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body %s", w.Code, w.Body.String())
	}
	got := mock.got[0]
	if got.Adaptive == nil || !got.Adaptive.Enabled {
		t.Fatalf("adaptive should be enabled, got %+v", got.Adaptive)
	}
	found := false
	for _, topic := range got.Topics {
		if topic == "Geometry" {
			found = true
		}
	}
	if !found {
		t.Errorf("weak_topics should merge into topics, got %v", got.Topics)
	}
	found = false
	for _, id := range got.OnlySourceIDs {
		if id == "q9" {
			found = true
		}
	}
	if !found {
		t.Errorf("review_due should merge into only_source_ids, got %v", got.OnlySourceIDs)
	}
}

func TestQuizGeneratePOST_AdaptiveBody(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	handler := handleQuizGenerate(mock)
	body := `{"mode":"mcq","num_questions":2,"adaptive":true,"weak_topics":["Geometry"],"review_due":["q9"],"prefer_incorrect":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/generate", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body %s", w.Code, w.Body.String())
	}
	got := mock.got[0]
	if got.Adaptive == nil || !got.Adaptive.Enabled {
		t.Fatalf("adaptive should be enabled, got %+v", got.Adaptive)
	}
	if !got.Adaptive.PreferIncorrect {
		t.Errorf("prefer_incorrect should propagate, got %+v", got.Adaptive)
	}
}

// Phase 27 wire-up: AdvancedFilter flags into the search handler.

func TestSearchGET_AdvancedDefaultsOff(t *testing.T) {
	qt := "Explain deadlock"
	q := questions.Question{ID: "q1", QuestionText: &qt}
	mock := &mockSearcher{resp: search.Response{Results: []search.Result{{Question: q, Similarity: 0.9}}}}
	handler := handleSearch(mock)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=deadlock", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body %s", w.Code, w.Body.String())
	}
	if mock.query != "deadlock" {
		t.Errorf("default query should pass through untouched, got %q", mock.query)
	}
}

func TestSearchGET_RewriteAndContextual(t *testing.T) {
	qt := "database question"
	q := questions.Question{ID: "q1", QuestionText: &qt}
	mock := &mockSearcher{resp: search.Response{Results: []search.Result{{Question: q, Similarity: 0.9}}}}
	handler := handleSearch(mock)
	// "db" rewrites to an expanded form containing "database".
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=db&rewrite=true", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(mock.query, "database") {
		t.Errorf("rewrite should expand synonyms, got %q", mock.query)
	}

	mock2 := &mockSearcher{resp: search.Response{Results: []search.Result{{Question: q, Similarity: 0.9}}}}
	handler2 := handleSearch(mock2)
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=paging&subject=OS&contextual=true", nil)
	w2 := httptest.NewRecorder()
	handler2.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body %s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(mock2.query, "[subject: OS]") {
		t.Errorf("contextual should prefix subject, got %q", mock2.query)
	}
}

func TestSearchGET_MultiQueryAndParentChild(t *testing.T) {
	qt := "Explain deadlock in operating systems"
	q1 := questions.Question{ID: "q1", DocumentID: "d1", QuestionText: &qt}
	q2 := questions.Question{ID: "q2", DocumentID: "d2", QuestionText: &qt}
	mock := &mockSearcher{resp: search.Response{Results: []search.Result{
		{Question: q1, Similarity: 0.9},
		{Question: q2, Similarity: 0.8},
	}}}
	handler := handleSearch(mock)
	// multi_query fans out; merged response stays a vector envelope.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=deadlock&multi_query=true", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body %s", w.Code, w.Body.String())
	}
	var vresp search.Response
	if err := json.NewDecoder(w.Body).Decode(&vresp); err != nil {
		t.Fatalf("decode vector envelope: %v", err)
	}
	if vresp.Count != 2 {
		t.Errorf("merged multi-query count = %d want 2", vresp.Count)
	}

	// parent_child groups hits by document.
	mock2 := &mockSearcher{resp: search.Response{Results: []search.Result{
		{Question: q1, Similarity: 0.9},
		{Question: q2, Similarity: 0.8},
	}}}
	handler2 := handleSearch(mock2)
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=deadlock&parent_child=true", nil)
	w2 := httptest.NewRecorder()
	handler2.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body %s", w2.Code, w2.Body.String())
	}
	var grouped search.ParentChildResponse
	if err := json.NewDecoder(w2.Body).Decode(&grouped); err != nil {
		t.Fatalf("decode parent-child envelope: %v", err)
	}
	if grouped.Count != 2 {
		t.Errorf("parent groups = %d want 2", grouped.Count)
	}
}

func TestSearchPOST_AdvancedBody(t *testing.T) {
	qt := "database question"
	q := questions.Question{ID: "q1", QuestionText: &qt}
	mock := &mockSearcher{resp: search.Response{Results: []search.Result{{Question: q, Similarity: 0.9}}}}
	handler := handleSearch(mock)
	body := `{"query":"db","rewrite":true,"contextual":true,"subject":"OS"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/search", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(mock.query, "database") || !strings.Contains(mock.query, "[subject: OS]") {
		t.Errorf("POST rewrite+contextual should transform query, got %q", mock.query)
	}
}
