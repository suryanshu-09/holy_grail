package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/search"
	"github.com/suryanshu-09/holy_grail/internal/study"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

// fakeStudyTopics resolves topic names for study guide tests.
type fakeStudyTopics struct {
	byName map[string]topics.Topic
	err    error
}

func (f *fakeStudyTopics) GetByName(_ context.Context, name string, _ *string) (topics.Topic, error) {
	if f.err != nil {
		return topics.Topic{}, f.err
	}
	if t, ok := f.byName[strings.ToLower(strings.TrimSpace(name))]; ok {
		return t, nil
	}
	return topics.Topic{}, errTestNotFound
}

func studyTestQuestions() []questions.Question {
	t1 := "Explain deadlock prevention with resource allocation."
	t2 := "What is paging in operating systems?"
	return []questions.Question{
		{ID: "q1", DocumentID: "d1", QuestionText: &t1},
		{ID: "q2", DocumentID: "d1", QuestionText: &t2},
	}
}

func studyTestSearcher(t *testing.T) *mockSearcher {
	t.Helper()
	pyqs := studyTestQuestions()
	results := make([]search.Result, 0, len(pyqs))
	for _, q := range pyqs {
		results = append(results, search.Result{Question: q, Similarity: 0.9})
	}
	return &mockSearcher{resp: search.Response{Results: results}}
}

func TestHandleStudyGuideGET(t *testing.T) {
	subject := "Operating Systems"
	deps := StudyDeps{
		Topics:   &fakeStudyTopics{byName: map[string]topics.Topic{"deadlock": {ID: "t1", Name: "Deadlock", Subject: &subject}}},
		Searcher: studyTestSearcher(t),
		Quiz:     &mockQuizGenerator{resp: quizOKResponse()},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/study/guide?topic=Deadlock", nil)
	w := httptest.NewRecorder()
	handleStudyGuide(deps).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	var guide study.StudyGuide
	if err := json.NewDecoder(w.Body).Decode(&guide); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if guide.Topic != "Deadlock" {
		t.Fatalf("topic = %q want Deadlock", guide.Topic)
	}
	if guide.Subject != "Operating Systems" {
		t.Fatalf("subject should resolve via Topics, got %q", guide.Subject)
	}
	if guide.Explanation == "" || guide.Example == "" || len(guide.KeyPoints) == 0 {
		t.Fatalf("guide flow incomplete: %+v", guide)
	}
	if len(guide.PYQRefs) != 2 {
		t.Fatalf("expected 2 PYQ refs, got %d", len(guide.PYQRefs))
	}
	// Quiz generator upgrades NextQuiz.
	if len(guide.NextQuiz.Questions) != 2 || guide.NextQuiz.Questions[0].SourceQuestionID != "q1" {
		t.Fatalf("next quiz should come from Quiz generator: %+v", guide.NextQuiz)
	}
}

func TestHandleStudyGuidePOST(t *testing.T) {
	deps := StudyDeps{Searcher: studyTestSearcher(t)}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/study/guide",
		strings.NewReader(`{"topic":"Paging","subject":"Operating Systems","limit":5,"weak":true}`))
	w := httptest.NewRecorder()
	handleStudyGuide(deps).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	var guide study.StudyGuide
	if err := json.NewDecoder(w.Body).Decode(&guide); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !guide.WeakTopic || guide.Subject != "Operating Systems" {
		t.Fatalf("weak/subject wrong: %+v", guide)
	}
	// No Quiz dep: deterministic fallback quiz from PYQs.
	if len(guide.NextQuiz.Questions) != 2 {
		t.Fatalf("fallback quiz should have 2 questions, got %d", len(guide.NextQuiz.Questions))
	}
}

func TestHandleStudyGuideValidation(t *testing.T) {
	deps := StudyDeps{Searcher: studyTestSearcher(t)}
	// Missing topic on GET and POST maps to 400.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/study/guide?subject=OS", nil)
	w := httptest.NewRecorder()
	handleStudyGuide(deps).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GET without topic: expected 400 got %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/study/guide", strings.NewReader(`{"subject":"OS"}`))
	w = httptest.NewRecorder()
	handleStudyGuide(deps).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST without topic: expected 400 got %d", w.Code)
	}
	// Bad limit / weak / body map to 400.
	for _, target := range []string{
		"/api/v1/study/guide?topic=Deadlock&limit=0",
		"/api/v1/study/guide?topic=Deadlock&weak=maybe",
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		w := httptest.NewRecorder()
		handleStudyGuide(deps).ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400 got %d", target, w.Code)
		}
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/study/guide", strings.NewReader(`{invalid`))
	w = httptest.NewRecorder()
	handleStudyGuide(deps).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: expected 400 got %d", w.Code)
	}
	// Wrong method maps to 405.
	req = httptest.NewRequest(http.MethodPut, "/api/v1/study/guide?topic=Deadlock", nil)
	w = httptest.NewRecorder()
	handleStudyGuide(deps).ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 got %d", w.Code)
	}
}

func TestHandleStudyGuideNilDeps(t *testing.T) {
	// Fully nil deps still return a (PYQ-less) guide, never a 500.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/study/guide?topic=Deadlock", nil)
	w := httptest.NewRecorder()
	handleStudyGuide(StudyDeps{}).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	var guide study.StudyGuide
	if err := json.NewDecoder(w.Body).Decode(&guide); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if guide.Topic != "Deadlock" || guide.Explanation == "" {
		t.Fatalf("guide wrong: %+v", guide)
	}
}
