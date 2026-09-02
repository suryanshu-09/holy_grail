package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

// mockTopicsRepo for handler tests reuses similar mock but simplified.
type mockTopicsRepo struct {
	topics          map[string]topics.Topic
	setCalls        [][]string
	mergeCalls      [][2]string
	listForQuestion []topics.Topic
	getByIDErr      error
	setErr          error
	mergeErr        error
	listErr         error
}

func newMockTopicsRepo() *mockTopicsRepo {
	return &mockTopicsRepo{topics: make(map[string]topics.Topic)}
}
func (m *mockTopicsRepo) List(_ context.Context, _ topics.Filter) ([]topics.Topic, error) {
	return nil, nil
}
func (m *mockTopicsRepo) GetByID(_ context.Context, id string) (topics.Topic, error) {
	if m.getByIDErr != nil {
		return topics.Topic{}, m.getByIDErr
	}
	if t, ok := m.topics[id]; ok {
		return t, nil
	}
	return topics.Topic{}, apperr.ErrNotFound
}
func (m *mockTopicsRepo) Create(_ context.Context, _ string, _ *string) (topics.Topic, error) {
	return topics.Topic{}, nil
}
func (m *mockTopicsRepo) GetByName(_ context.Context, _ string, _ *string) (topics.Topic, error) {
	return topics.Topic{}, apperr.ErrNotFound
}
func (m *mockTopicsRepo) FindOrCreate(_ context.Context, _ string, _ *string) (topics.Topic, error) {
	return topics.Topic{}, nil
}
func (m *mockTopicsRepo) ListWithCounts(_ context.Context, _ topics.Filter) ([]topics.TopicWithCount, error) {
	return nil, nil
}
func (m *mockTopicsRepo) MergeTopics(_ context.Context, s, t string) error {
	m.mergeCalls = append(m.mergeCalls, [2]string{s, t})
	if m.mergeErr != nil {
		return m.mergeErr
	}
	delete(m.topics, s)
	return nil
}
func (m *mockTopicsRepo) AddQuestionTopic(_ context.Context, _, _ string, _ *float64) error { return nil }
func (m *mockTopicsRepo) RemoveQuestionTopic(_ context.Context, _, _ string) error        { return nil }
func (m *mockTopicsRepo) ListTopicsForQuestion(_ context.Context, _ string) ([]topics.Topic, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.listForQuestion != nil {
		return m.listForQuestion, nil
	}
	if len(m.setCalls) > 0 {
		last := m.setCalls[len(m.setCalls)-1]
		out := []topics.Topic{}
		for _, id := range last {
			if t, ok := m.topics[id]; ok {
				out = append(out, t)
			}
		}
		if out == nil {
			out = []topics.Topic{}
		}
		return out, nil
	}
	return []topics.Topic{}, nil
}
func (m *mockTopicsRepo) ListQuestionTopics(_ context.Context, _ string) ([]topics.QuestionTopic, error) {
	return nil, nil
}
func (m *mockTopicsRepo) SetQuestionTopics(_ context.Context, _ string, ids []string, _ map[string]*float64) error {
	if m.setErr != nil {
		return m.setErr
	}
	cp := make([]string, len(ids))
	copy(cp, ids)
	m.setCalls = append(m.setCalls, cp)
	return nil
}

func TestHandleCorrectQuestionTopics_Success(t *testing.T) {
	repo := newMockTopicsRepo()
	repo.topics["t1"] = topics.Topic{ID: "t1", Name: "Deadlock"}
	repo.topics["t2"] = topics.Topic{ID: "t2", Name: "Paging"}
	svc := topics.NewService(repo)
	handler := handleCorrectQuestionTopics(svc)

	body, _ := json.Marshal(map[string]interface{}{"topic_ids": []string{"t1", "t2"}})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/questions/q1/topics", bytes.NewReader(body))
	req.SetPathValue("id", "q1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body %s", w.Code, w.Body.String())
	}
	var got []topics.Topic
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 topics, got %d", len(got))
	}
}

func TestHandleCorrectQuestionTopics_SnakeAndCamel(t *testing.T) {
	repo := newMockTopicsRepo()
	repo.topics["t1"] = topics.Topic{ID: "t1", Name: "Deadlock"}
	svc := topics.NewService(repo)
	handler := handleCorrectQuestionTopics(svc)

	// camelCase
	body, _ := json.Marshal(map[string]interface{}{"topicIds": []string{"t1"}})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/questions/q1/topics", bytes.NewReader(body))
	req.SetPathValue("id", "q1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("camelCase expected 200 got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleCorrectQuestionTopics_InvalidBody(t *testing.T) {
	repo := newMockTopicsRepo()
	svc := topics.NewService(repo)
	handler := handleCorrectQuestionTopics(svc)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/questions/q1/topics", bytes.NewReader([]byte("{invalid")))
	req.SetPathValue("id", "q1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleCorrectQuestionTopics_NotFound(t *testing.T) {
	repo := newMockTopicsRepo()
	// no topics inserted, so t1 missing will cause NotFound
	svc := topics.NewService(repo)
	handler := handleCorrectQuestionTopics(svc)
	body, _ := json.Marshal(map[string]interface{}{"topic_ids": []string{"missing"}})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/questions/q1/topics", bytes.NewReader(body))
	req.SetPathValue("id", "q1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body %s", w.Code, w.Body.String())
	}
}

func TestHandleCorrectQuestionTopics_MissingID(t *testing.T) {
	repo := newMockTopicsRepo()
	svc := topics.NewService(repo)
	handler := handleCorrectQuestionTopics(svc)
	body, _ := json.Marshal(map[string]interface{}{"topic_ids": []string{"t1"}})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/questions/q1/topics", bytes.NewReader(body))
	// intentionally not setting PathValue
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing id, got %d", w.Code)
	}
}

func TestHandleCorrectQuestionTopics_MethodNotAllowed(t *testing.T) {
	repo := newMockTopicsRepo()
	svc := topics.NewService(repo)
	handler := handleCorrectQuestionTopics(svc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/questions/q1/topics", nil)
	req.SetPathValue("id", "q1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandleCorrectQuestionTopics_ClearEmpty(t *testing.T) {
	repo := newMockTopicsRepo()
	repo.topics["t1"] = topics.Topic{ID: "t1", Name: "Deadlock"}
	svc := topics.NewService(repo)
	handler := handleCorrectQuestionTopics(svc)
	// empty topic_ids should clear and return 200 with empty list
	body, _ := json.Marshal(map[string]interface{}{"topic_ids": []string{}})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/questions/q1/topics", bytes.NewReader(body))
	req.SetPathValue("id", "q1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", w.Code, w.Body.String())
	}
	var got []topics.Topic
	_ = json.NewDecoder(w.Body).Decode(&got)
	if len(got) != 0 {
		t.Fatalf("expected 0 topics after clear, got %d", len(got))
	}
}

func TestHandleMergeTopics_Success(t *testing.T) {
	repo := newMockTopicsRepo()
	repo.topics["target"] = topics.Topic{ID: "target", Name: "Deadlock"}
	repo.topics["s1"] = topics.Topic{ID: "s1", Name: "Deadlocks"}
	svc := topics.NewService(repo)
	handler := handleMergeTopics(svc)
	body, _ := json.Marshal(map[string]interface{}{"target_id": "target", "source_ids": []string{"s1"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/topics/merge", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "merged" {
		t.Errorf("expected merged status, got %v", resp)
	}
}

func TestHandleMergeTopics_CamelCase(t *testing.T) {
	repo := newMockTopicsRepo()
	repo.topics["target"] = topics.Topic{ID: "target", Name: "Deadlock"}
	repo.topics["s1"] = topics.Topic{ID: "s1", Name: "Deadlocks"}
	svc := topics.NewService(repo)
	handler := handleMergeTopics(svc)
	body, _ := json.Marshal(map[string]interface{}{"targetId": "target", "sourceIds": []string{"s1"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/topics/merge", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("camelCase expected 200, got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleMergeTopics_MissingTarget(t *testing.T) {
	repo := newMockTopicsRepo()
	svc := topics.NewService(repo)
	handler := handleMergeTopics(svc)
	body, _ := json.Marshal(map[string]interface{}{"source_ids": []string{"s1"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/topics/merge", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleMergeTopics_MissingSources(t *testing.T) {
	repo := newMockTopicsRepo()
	svc := topics.NewService(repo)
	handler := handleMergeTopics(svc)
	body, _ := json.Marshal(map[string]interface{}{"target_id": "target"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/topics/merge", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleMergeTopics_NotFound(t *testing.T) {
	repo := newMockTopicsRepo()
	// target missing
	svc := topics.NewService(repo)
	handler := handleMergeTopics(svc)
	body, _ := json.Marshal(map[string]interface{}{"target_id": "missing", "source_ids": []string{"s1"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/topics/merge", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleMergeTopics_ValidationError(t *testing.T) {
	repo := newMockTopicsRepo()
	repo.topics["target"] = topics.Topic{ID: "target", Name: "Deadlock"}
	svc := topics.NewService(repo)
	handler := handleMergeTopics(svc)
	// target == source
	body, _ := json.Marshal(map[string]interface{}{"target_id": "target", "source_ids": []string{"target"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/topics/merge", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleMergeTopics_MethodNotAllowed(t *testing.T) {
	repo := newMockTopicsRepo()
	svc := topics.NewService(repo)
	handler := handleMergeTopics(svc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/topics/merge", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandleMergeTopics_InternalError(t *testing.T) {
	repo := newMockTopicsRepo()
	repo.topics["target"] = topics.Topic{ID: "target", Name: "Deadlock"}
	repo.topics["s1"] = topics.Topic{ID: "s1", Name: "Deadlocks"}
	repo.mergeErr = errors.New("db down")
	svc := topics.NewService(repo)
	handler := handleMergeTopics(svc)
	body, _ := json.Marshal(map[string]interface{}{"target_id": "target", "source_ids": []string{"s1"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/topics/merge", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d %s", w.Code, w.Body.String())
	}
}

func TestRouter_RegistersNewRoutes(t *testing.T) {
	repo := newMockTopicsRepo()
	repo.topics["t1"] = topics.Topic{ID: "t1", Name: "Deadlock"}
	repo.topics["target"] = topics.Topic{ID: "target", Name: "Deadlock"}
	repo.topics["s1"] = topics.Topic{ID: "s1", Name: "Deadlocks"}
	svc := topics.NewService(repo)
	cfg := &config.AppConfig{CORSAllowedOrigin: "*"}
	deps := RouterDeps{Topics: svc}
	router := NewRouter(cfg, deps)

	// Test PATCH /api/v1/questions/{id}/topics via router
	body, _ := json.Marshal(map[string]interface{}{"topic_ids": []string{"t1"}})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/questions/q1/topics", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("router PATCH expected 200, got %d %s", w.Code, w.Body.String())
	}

	// Test POST /api/v1/topics/merge via router
	body2, _ := json.Marshal(map[string]interface{}{"target_id": "target", "source_ids": []string{"s1"}})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/topics/merge", bytes.NewReader(body2))
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("router POST merge expected 200, got %d %s", w2.Code, w2.Body.String())
	}

	// Test CORS header for PATCH
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected CORS header *, got %q", got)
	}
}

func TestCORS_AllowsPATCH(t *testing.T) {
	cfg := &config.AppConfig{CORSAllowedOrigin: "*"}
	repo := newMockTopicsRepo()
	svc := topics.NewService(repo)
	deps := RouterDeps{Topics: svc}
	router := NewRouter(cfg, deps)
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/topics/merge", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	allow := w.Header().Get("Access-Control-Allow-Methods")
	if allow != "GET, POST, PATCH, OPTIONS" {
		t.Errorf("expected CORS methods to include PATCH, got %q", allow)
	}
}
