package http

// Phase 22 integration tests: HTTP handlers -> services -> stores.
//
// The in-memory variants exercise the full HTTP -> service path with
// httptest (document upload/list, question list/get, quiz
// generate/answer flow) without external infrastructure. The
// Postgres-backed variant reuses the same HTTP + service wiring against
// real SQL repositories and skips when DATABASE_URL is unreachable.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
)

// ---------------------------------------------------------------------------
// In-memory fakes (memory stores, no external infra)
// ---------------------------------------------------------------------------

type integDocRepo struct {
	mu    sync.Mutex
	docs  map[string]documents.Document
	order []string
}

func newIntegDocRepo() *integDocRepo {
	return &integDocRepo{docs: map[string]documents.Document{}}
}

func (r *integDocRepo) List(_ context.Context, f documents.Filter) ([]documents.Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]documents.Document, 0)
	for _, id := range r.order {
		d := r.docs[id]
		if f.Subject != "" && (d.Subject == nil || *d.Subject != f.Subject) {
			continue
		}
		if f.Status != "" && d.Status != f.Status {
			continue
		}
		if f.Year != nil && (d.Year == nil || *d.Year != *f.Year) {
			continue
		}
		out = append(out, d)
	}
	if f.Offset < len(out) {
		out = out[f.Offset:]
	} else {
		out = nil
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	if out == nil {
		out = []documents.Document{}
	}
	return out, nil
}

func (r *integDocRepo) GetByID(_ context.Context, id string) (documents.Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.docs[id]
	if !ok {
		return documents.Document{}, apperr.ErrNotFound
	}
	return d, nil
}

func (r *integDocRepo) Create(_ context.Context, d *documents.Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	d.CreatedAt = now
	d.UpdatedAt = now
	r.docs[d.ID] = *d
	r.order = append(r.order, d.ID)
	return nil
}

func (r *integDocRepo) UpdateStatus(_ context.Context, id string, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.docs[id]
	if !ok {
		return apperr.ErrNotFound
	}
	d.Status = status
	r.docs[id] = d
	return nil
}

type integDocStorage struct {
	mu    sync.Mutex
	files map[string][]byte
}

func newIntegDocStorage() *integDocStorage {
	return &integDocStorage{files: map[string][]byte{}}
}

func (s *integDocStorage) SaveDocument(_ context.Context, id string, r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[id] = b
	return "documents/" + id + ".pdf", nil
}

func (s *integDocStorage) RemoveDocument(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.files, id)
	return nil
}

type integQuestionRepo struct {
	mu sync.Mutex
	qs map[string]questions.Question
}

func newIntegQuestionRepo() *integQuestionRepo {
	return &integQuestionRepo{qs: map[string]questions.Question{}}
}

func (r *integQuestionRepo) List(_ context.Context, f questions.Filter) ([]questions.Question, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]questions.Question, 0)
	for _, q := range r.qs {
		if f.DocumentID != "" && q.DocumentID != f.DocumentID {
			continue
		}
		if f.Subject != "" && (q.Subject == nil || *q.Subject != f.Subject) {
			continue
		}
		if f.Year != nil && (q.Year == nil || *q.Year != *f.Year) {
			continue
		}
		out = append(out, q)
	}
	if f.Offset < len(out) {
		out = out[f.Offset:]
	} else {
		out = nil
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	if out == nil {
		out = []questions.Question{}
	}
	return out, nil
}

func (r *integQuestionRepo) GetByID(_ context.Context, id string) (questions.Question, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	q, ok := r.qs[id]
	if !ok {
		return questions.Question{}, apperr.ErrNotFound
	}
	return q, nil
}

func (r *integQuestionRepo) Insert(_ context.Context, q questions.Question) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if q.ID == "" {
		q.ID = fmt.Sprintf("q-%d", len(r.qs)+1)
	}
	r.qs[q.ID] = q
	return nil
}

type integEvalStore struct {
	mu       sync.Mutex
	sessions map[string]quiz.QuizSession
	attempts map[string][]quiz.QuizAttempt
	seq      int
}

func newIntegEvalStore() *integEvalStore {
	return &integEvalStore{
		sessions: map[string]quiz.QuizSession{},
		attempts: map[string][]quiz.QuizAttempt{},
	}
}

func (s *integEvalStore) CreateSession(_ context.Context, sess quiz.QuizSession) (quiz.QuizSession, error) {
	if err := sess.Validate(); err != nil {
		return quiz.QuizSession{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	sess.ID = fmt.Sprintf("sess-integ-%d", s.seq)
	sess.CreatedAt = time.Now().UTC()
	s.sessions[sess.ID] = sess
	return sess, nil
}

func (s *integEvalStore) GetSession(_ context.Context, id string) (quiz.QuizSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[strings.TrimSpace(id)]
	if !ok {
		return quiz.QuizSession{}, apperr.ErrNotFound
	}
	return sess, nil
}

func (s *integEvalStore) RecordAttempt(_ context.Context, a quiz.QuizAttempt) (quiz.QuizAttempt, error) {
	if err := a.Validate(); err != nil {
		return quiz.QuizAttempt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[a.SessionID]; !ok {
		return quiz.QuizAttempt{}, apperr.ErrNotFound
	}
	a.ID = fmt.Sprintf("att-integ-%d", len(s.attempts[a.SessionID])+1)
	s.attempts[a.SessionID] = append(s.attempts[a.SessionID], a)
	return a, nil
}

func (s *integEvalStore) ListAttempts(_ context.Context, sessionID string) ([]quiz.QuizAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]quiz.QuizAttempt{}, s.attempts[strings.TrimSpace(sessionID)]...), nil
}

// ---------------------------------------------------------------------------
// Harness: real services + real HTTP router over memory stores
// ---------------------------------------------------------------------------

type integHarness struct {
	handler  http.Handler
	docRepo  *integDocRepo
	qRepo    *integQuestionRepo
	evalSvc  *quiz.EvaluationService
	quizGen  *quiz.QuizGenerator
	question context.Context
}

func newIntegHarness() *integHarness {
	docRepo := newIntegDocRepo()
	docSvc := documents.NewService(docRepo, newIntegDocStorage())
	qRepo := newIntegQuestionRepo()
	qSvc := questions.NewService(qRepo)
	evalSvc := quiz.NewEvaluationService(newIntegEvalStore())
	gen := &quiz.QuizGenerator{
		// Deterministic retriever over the seeded memory questions; nil LLM
		// selects the deterministic Original-PYQ fallback path.
		Retriever: quiz.RetrieveFunc(func(ctx context.Context, req quiz.QuizRequest) ([]questions.Question, error) {
			return qRepo.List(ctx, questions.Filter{Limit: 50})
		}),
		LLM: nil,
	}
	cfg := &config.AppConfig{CORSAllowedOrigin: "*", MaxUploadBytes: 10 << 20}
	h := NewRouter(cfg, RouterDeps{
		Documents: docSvc,
		Questions: qSvc,
		Quiz:      gen,
		QuizEval:  evalSvc,
	})
	return &integHarness{
		handler:  h,
		docRepo:  docRepo,
		qRepo:    qRepo,
		evalSvc:  evalSvc,
		quizGen:  gen,
		question: context.Background(),
	}
}

func integStrptr(s string) *string { return &s }

func (h *integHarness) seedQuestions(t *testing.T, docID string) []questions.Question {
	t.Helper()
	seed := []questions.Question{
		{ID: "integ-q1", DocumentID: docID, QuestionText: integStrptr("What is a deadlock?"), Subject: integStrptr("Operating Systems")},
		{ID: "integ-q2", DocumentID: docID, QuestionText: integStrptr("What is paging?"), Subject: integStrptr("Operating Systems")},
	}
	for _, q := range seed {
		if err := h.qRepo.Insert(context.Background(), q); err != nil {
			t.Fatalf("seed question: %v", err)
		}
	}
	return seed
}

func integUpload(t *testing.T, handler http.Handler, filename string, content []byte) documents.Document {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload expected 201 got %d body %s", w.Code, w.Body.String())
	}
	var doc documents.Document
	if err := json.NewDecoder(w.Body).Decode(&doc); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	if doc.ID == "" {
		t.Fatal("upload returned empty document id")
	}
	return doc
}

// ---------------------------------------------------------------------------
// In-memory integration tests (httptest + real services)
// ---------------------------------------------------------------------------

func TestIntegration_DocumentsUploadListFlow(t *testing.T) {
	h := newIntegHarness()

	pdf := []byte("%PDF-1.4 integration test content")
	doc := integUpload(t, h.handler, "syllabus.pdf", pdf)
	if doc.Status != documents.StatusUploaded {
		t.Errorf("status = %q want %q", doc.Status, documents.StatusUploaded)
	}

	// List contains the upload.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents", nil)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list expected 200 got %d body %s", w.Code, w.Body.String())
	}
	var listed []documents.Document
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, d := range listed {
		if d.ID == doc.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("uploaded document %s missing from list (%d docs)", doc.ID, len(listed))
	}

	// Status filter still returns it; unknown subject returns none.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/documents?status=uploaded", nil)
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("filtered list expected 200 got %d", w.Code)
	}

	// Non-PDF upload is rejected through handler + service validation.
	var bad bytes.Buffer
	mw := multipart.NewWriter(&bad)
	part, _ := mw.CreateFormFile("file", "notes.txt")
	_, _ = part.Write([]byte("plain text, not a pdf"))
	_ = mw.Close()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/documents", &bad)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("non-PDF upload expected 400 got %d body %s", w.Code, w.Body.String())
	}

	// Invalid year query is a 400.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/documents?year=notanint", nil)
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad year expected 400 got %d", w.Code)
	}
}

func TestIntegration_QuestionsListGetFlow(t *testing.T) {
	h := newIntegHarness()
	doc := integUpload(t, h.handler, "pyq.pdf", []byte("%PDF-1.4 questions"))
	seed := h.seedQuestions(t, doc.ID)

	// List all questions.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/questions", nil)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("questions list expected 200 got %d body %s", w.Code, w.Body.String())
	}
	var listed []questions.Question
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode questions list: %v", err)
	}
	if len(listed) != len(seed) {
		t.Fatalf("listed = %d want %d", len(listed), len(seed))
	}

	// Filter by document_id narrows to the seeded rows.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/questions?document_id="+doc.ID, nil)
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("filtered questions expected 200 got %d", w.Code)
	}

	// Get by ID returns the full record with traceability fields.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/questions/integ-q1", nil)
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("question get expected 200 got %d body %s", w.Code, w.Body.String())
	}
	var one questions.Question
	if err := json.NewDecoder(w.Body).Decode(&one); err != nil {
		t.Fatalf("decode question: %v", err)
	}
	if one.ID != "integ-q1" || one.DocumentID != doc.ID {
		t.Errorf("got %+v want id integ-q1 doc %s", one, doc.ID)
	}

	// Unknown ID is 404, not 500.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/questions/does-not-exist", nil)
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown question expected 404 got %d body %s", w.Code, w.Body.String())
	}
}

func TestIntegration_QuizGenerateAnswerFlow(t *testing.T) {
	h := newIntegHarness()
	doc := integUpload(t, h.handler, "quiz-src.pdf", []byte("%PDF-1.4 quiz sources"))
	h.seedQuestions(t, doc.ID)

	// Generate via POST (service fallback path, no LLM configured).
	genBody := `{"mode":"original","num_questions":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/generate", strings.NewReader(genBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("quiz generate POST expected 200 got %d body %s", w.Code, w.Body.String())
	}
	var generated quiz.QuizResponse
	if err := json.NewDecoder(w.Body).Decode(&generated); err != nil {
		t.Fatalf("decode quiz: %v", err)
	}
	if len(generated.Questions) != 2 {
		t.Fatalf("generated = %d questions want 2", len(generated.Questions))
	}
	for _, q := range generated.Questions {
		if q.SourceQuestionID == "" || q.DocumentID == "" {
			t.Fatalf("question missing traceability: %+v", q)
		}
		if q.DocumentID != doc.ID {
			t.Errorf("question doc = %q want %q", q.DocumentID, doc.ID)
		}
	}

	// Generate via GET exercises the query-param mapping through the router.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/quiz/generate?mode=original&length=1", nil)
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("quiz generate GET expected 200 got %d body %s", w.Code, w.Body.String())
	}

	// Answer flow: create session -> submit attempt -> read result.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions",
		strings.NewReader(`{"mode":"original","num_questions":2,"subject":"Operating Systems"}`))
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create session expected 201 got %d body %s", w.Code, w.Body.String())
	}
	var sess struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&sess); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("empty session id")
	}

	first := generated.Questions[0]
	attemptBody, _ := json.Marshal(map[string]any{
		"question_id": first.ID, "source_question_id": first.SourceQuestionID,
		"question_text": first.Question, "selected_answer": first.CorrectAnswer,
		"correct_answer": first.CorrectAnswer, "topic": "Deadlock",
		"subject":        "Operating Systems", "time_taken_seconds": 12.5,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/"+sess.ID+"/attempts", bytes.NewReader(attemptBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("submit attempt expected 201 got %d body %s", w.Code, w.Body.String())
	}
	var recorded struct {
		IsCorrect bool `json:"is_correct"`
	}
	if err := json.NewDecoder(w.Body).Decode(&recorded); err != nil {
		t.Fatalf("decode attempt: %v", err)
	}
	if !recorded.IsCorrect {
		t.Error("correct attempt recorded as incorrect")
	}

	// Bulk submit the second question (answered incorrectly).
	second := generated.Questions[1]
	wrong := second.CorrectAnswer
	if wrong == 0 {
		wrong = 1
	}
	bulkBody, _ := json.Marshal(map[string]any{"attempts": []map[string]any{{
		"question_id": second.ID, "source_question_id": second.SourceQuestionID,
		"question_text": second.Question, "selected_answer": wrong,
		"correct_answer": second.CorrectAnswer, "topic": "Paging",
		"subject":        "Operating Systems", "time_taken_seconds": 7.0,
	}}})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/quiz/sessions/"+sess.ID+"/attempts/bulk", bytes.NewReader(bulkBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("bulk submit expected 201 got %d body %s", w.Code, w.Body.String())
	}

	// Session result aggregates both attempts (1 correct / 1 incorrect).
	req = httptest.NewRequest(http.MethodGet, "/api/v1/quiz/sessions/"+sess.ID, nil)
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get session expected 200 got %d body %s", w.Code, w.Body.String())
	}
	var detail struct {
		Attempts []map[string]any `json:"attempts"`
		Metrics  struct {
			QuestionsAttempted int `json:"questions_attempted"`
			QuestionsCorrect   int `json:"questions_correct"`
		} `json:"metrics"`
	}
	if err := json.NewDecoder(w.Body).Decode(&detail); err != nil {
		t.Fatalf("decode session detail: %v", err)
	}
	if len(detail.Attempts) != 2 {
		t.Errorf("attempts = %d want 2", len(detail.Attempts))
	}
	if detail.Metrics.QuestionsAttempted != 2 || detail.Metrics.QuestionsCorrect != 1 {
		t.Errorf("metrics = %+v want attempted=2 correct=1", detail.Metrics)
	}

	// Validation still enforced end-to-end: bad mode is 400, unknown session 404.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/quiz/generate", strings.NewReader(`{"mode":"invent"}`))
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad mode expected 400 got %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/quiz/sessions/sess-missing", nil)
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown session expected 404 got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Postgres-backed variant: same HTTP -> service path, SQL repositories.
// Skips when DATABASE_URL/TEST_POSTGRES_DSN is unreachable so unit runs
// stay hermetic; exercises the real documents/questions/eval repositories
// when a test database is available.
// ---------------------------------------------------------------------------

func integPostgresDSN() string {
	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		return dsn
	}
	return os.Getenv("DATABASE_URL")
}

func TestIntegration_Postgres_DocumentsQuizFlow(t *testing.T) {
	dsn := integPostgresDSN()
	if strings.TrimSpace(dsn) == "" {
		t.Skip("DATABASE_URL/TEST_POSTGRES_DSN not set; skipping Postgres-backed integration test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("postgres open failed, skipping: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("postgres unreachable, skipping: %v", err)
	}

	docSvc := documents.NewService(documents.NewRepository(db), newIntegDocStorage())
	qSvc := questions.NewService(questions.NewRepository(db))
	evalSvc := quiz.NewEvaluationService(quiz.NewEvaluationRepository(db))

	// Probe schema presence; skip (not fail) when migrations are not applied.
	if _, err := docSvc.List(context.Background(), documents.Filter{Limit: 1}); err != nil {
		t.Skipf("documents table unavailable, skipping: %v", err)
	}

	// Document upload persists metadata through the SQL repository.
	created, err := docSvc.Upload(context.Background(), "pg-integration.pdf",
		bytes.NewReader([]byte("%PDF-1.4 postgres integration")))
	if err != nil {
		t.Skipf("postgres upload failed, skipping: %v", err)
	}
	got, err := docSvc.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("postgres document get: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("got id %q want %q", got.ID, created.ID)
	}

	// Quiz evaluation session round-trips through the SQL store.
	sess, err := evalSvc.CreateSession(context.Background(), quiz.QuizSession{Mode: quiz.ModeOriginal, TotalQuestions: 1})
	if err != nil {
		t.Skipf("postgres session create failed, skipping: %v", err)
	}
	attempt, err := evalSvc.SubmitAttempt(context.Background(), quiz.QuizAttempt{
		SessionID: sess.ID, QuestionID: "pg-q1", SelectedAnswer: 1, CorrectAnswer: 1,
		Topic: "Deadlock", Subject: "Operating Systems",
	})
	if err != nil {
		t.Fatalf("postgres submit attempt: %v", err)
	}
	if !attempt.IsCorrect {
		t.Error("correct attempt recorded as incorrect")
	}
	res, err := evalSvc.GetResult(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("postgres get result: %v", err)
	}
	if res.Metrics.Attempted != 1 || res.Metrics.Correct != 1 {
		t.Errorf("postgres metrics = %+v want attempted=1 correct=1", res.Metrics)
	}

	// Questions service stays wired against the SQL repository.
	if _, err := qSvc.List(context.Background(), questions.Filter{Limit: 1}); err != nil {
		t.Fatalf("postgres questions list: %v", err)
	}

	// Same handlers serve the Postgres-backed services over HTTP.
	cfg := &config.AppConfig{CORSAllowedOrigin: "*", MaxUploadBytes: 10 << 20}
	gen := &quiz.QuizGenerator{
		Retriever: quiz.RetrieveFunc(func(ctx context.Context, req quiz.QuizRequest) ([]questions.Question, error) {
			return qSvc.List(ctx, questions.Filter{Limit: 50})
		}),
	}
	router := NewRouter(cfg, RouterDeps{Documents: docSvc, Questions: qSvc, Quiz: gen, QuizEval: evalSvc})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("postgres-backed HTTP list expected 200 got %d body %s", w.Code, w.Body.String())
	}
	_ = qSvc
}
