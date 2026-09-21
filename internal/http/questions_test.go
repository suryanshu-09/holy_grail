package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/questions"
)

// mockQuestionsRepo implements questions.Repository for handler tests.
type mockQuestionsRepo struct {
	byID    map[string]questions.Question
	getErr  error
	listOut []questions.Question
	listErr error
}

func (m *mockQuestionsRepo) List(_ context.Context, _ questions.Filter) ([]questions.Question, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.listOut != nil {
		return m.listOut, nil
	}
	return []questions.Question{}, nil
}

func (m *mockQuestionsRepo) GetByID(_ context.Context, id string) (questions.Question, error) {
	if m.getErr != nil {
		return questions.Question{}, m.getErr
	}
	if q, ok := m.byID[id]; ok {
		return q, nil
	}
	return questions.Question{}, apperr.ErrNotFound
}

func (m *mockQuestionsRepo) Insert(_ context.Context, _ questions.Question) error { return nil }

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

func traceabilityQuestion() questions.Question {
	return questions.Question{
		ID:           "q1",
		DocumentID:   "d1",
		QuestionText: strPtr("What is deadlock?"),
		StartPage:    intPtr(18),
		EndPage:      intPtr(19),
		ImagesJSON:   strPtr(`["img1.png"]`),
	}
}

func TestHandleQuestionByID_Success(t *testing.T) {
	repo := &mockQuestionsRepo{byID: map[string]questions.Question{"q1": traceabilityQuestion()}}
	svc := questions.NewService(repo)
	handler := handleQuestionByID(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/questions/q1", nil)
	req.SetPathValue("id", "q1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body %s", w.Code, w.Body.String())
	}
	var got questions.Question
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "q1" {
		t.Errorf("id = %q want q1", got.ID)
	}
	if got.DocumentID != "d1" {
		t.Errorf("document_id = %q want d1", got.DocumentID)
	}
	if got.QuestionText == nil || *got.QuestionText != "What is deadlock?" {
		t.Errorf("question_text not returned: %+v", got.QuestionText)
	}
	if got.StartPage == nil || *got.StartPage != 18 {
		t.Errorf("start_page not returned: %+v", got.StartPage)
	}
	if got.EndPage == nil || *got.EndPage != 19 {
		t.Errorf("end_page not returned: %+v", got.EndPage)
	}
	if got.ImagesJSON == nil || *got.ImagesJSON != `["img1.png"]` {
		t.Errorf("images_json not returned: %+v", got.ImagesJSON)
	}
}

func TestHandleQuestionByID_NotFound(t *testing.T) {
	repo := &mockQuestionsRepo{byID: map[string]questions.Question{}}
	svc := questions.NewService(repo)
	handler := handleQuestionByID(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/questions/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body %s", w.Code, w.Body.String())
	}
}

func TestHandleQuestionByID_MissingID(t *testing.T) {
	repo := &mockQuestionsRepo{byID: map[string]questions.Question{"q1": traceabilityQuestion()}}
	svc := questions.NewService(repo)
	handler := handleQuestionByID(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/questions/", nil)
	// intentionally not setting path value
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing id, got %d", w.Code)
	}
}

func TestHandleQuestionByID_MethodNotAllowed(t *testing.T) {
	repo := &mockQuestionsRepo{byID: map[string]questions.Question{"q1": traceabilityQuestion()}}
	svc := questions.NewService(repo)
	handler := handleQuestionByID(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/questions/q1", nil)
	req.SetPathValue("id", "q1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandleQuestionByID_InternalError(t *testing.T) {
	repo := &mockQuestionsRepo{getErr: errors.New("db down")}
	svc := questions.NewService(repo)
	handler := handleQuestionByID(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/questions/q1", nil)
	req.SetPathValue("id", "q1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body %s", w.Code, w.Body.String())
	}
}

func TestHandleQuestionByID_NilService(t *testing.T) {
	handler := handleQuestionByID(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/questions/q1", nil)
	req.SetPathValue("id", "q1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for nil service, got %d", w.Code)
	}
}

func TestRouter_QuestionByIDRoute(t *testing.T) {
	repo := &mockQuestionsRepo{byID: map[string]questions.Question{"q1": traceabilityQuestion()}}
	svc := questions.NewService(repo)
	cfg := &config.AppConfig{CORSAllowedOrigin: "*"}
	router := NewRouter(cfg, RouterDeps{Questions: svc})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/questions/q1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("router expected 200, got %d %s", w.Code, w.Body.String())
	}
	var got questions.Question
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "q1" || got.DocumentID != "d1" {
		t.Errorf("unexpected question returned: %+v", got)
	}

	// Missing question via router is 404.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/questions/missing", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("router expected 404 for missing, got %d %s", w2.Code, w2.Body.String())
	}
}
