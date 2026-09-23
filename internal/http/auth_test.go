package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/auth"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
	"github.com/suryanshu-09/holy_grail/internal/storage"
)

func testAuthService() *auth.Service {
	return auth.NewService(auth.NewMemoryStore())
}

func doJSON(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthRegisterLoginMeLogout(t *testing.T) {
	svc := testAuthService()

	rec := doJSON(t, handleRegister(svc, "development"), http.MethodPost, "/api/v1/auth/register",
		`{"email":"alice@example.com","password":"s3cur3pass","display_name":"Alice"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d body=%s, want 201", rec.Code, rec.Body.String())
	}
	var sess sessionDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatal(err)
	}
	if sess.Token == "" || sess.User.Email != "alice@example.com" {
		t.Fatalf("bad session payload: %+v", sess)
	}
	if rec.Header().Get("Set-Cookie") == "" {
		t.Fatalf("missing Set-Cookie on register")
	}

	// Duplicate registration conflicts.
	rec = doJSON(t, handleRegister(svc, "development"), http.MethodPost, "/api/v1/auth/register",
		`{"email":"alice@example.com","password":"s3cur3pass"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate register = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}

	// Invalid input is a 400.
	rec = doJSON(t, handleRegister(svc, "development"), http.MethodPost, "/api/v1/auth/register",
		`{"email":"nope","password":"s3cur3pass"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad email register = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}

	// Login works and reports the user.
	rec = doJSON(t, handleLogin(svc, "development"), http.MethodPost, "/api/v1/auth/login",
		`{"email":"alice@example.com","password":"s3cur3pass"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var login sessionDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}

	// Wrong password is a 401 (no user enumeration).
	rec = doJSON(t, handleLogin(svc, "development"), http.MethodPost, "/api/v1/auth/login",
		`{"email":"alice@example.com","password":"wrongpass1"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401 body=%s", rec.Code, rec.Body.String())
	}

	// /me with the bearer token returns the user.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	user, _ := svc.Authenticate(context.Background(), login.Token)
	req = req.WithContext(auth.WithUser(req.Context(), user))
	rec = httptest.NewRecorder()
	handleMe().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me = %d, want 200", rec.Code)
	}

	// /me without credentials is a 401.
	rec = doJSON(t, handleMe(), http.MethodGet, "/api/v1/auth/me", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon me = %d, want 401", rec.Code)
	}

	// Logout revokes the token.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	rec = httptest.NewRecorder()
	handleLogout(svc, "development").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout = %d, want 200", rec.Code)
	}
	if _, err := svc.Authenticate(context.Background(), login.Token); err == nil {
		t.Fatalf("revoked token still valid")
	}

	// Nil service is a 503.
	rec = doJSON(t, handleRegister(nil, "development"), http.MethodPost, "/api/v1/auth/register",
		`{"email":"x@y.co","password":"s3cur3pass"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil register = %d, want 503", rec.Code)
	}
}

func TestPreferencesRoundTrip(t *testing.T) {
	svc := testAuthService()
	user, _, err := svc.Register(context.Background(), "pref@example.com", "s3cur3pass", "")
	if err != nil {
		t.Fatal(err)
	}
	withUser := func(req *http.Request) *http.Request {
		return req.WithContext(auth.WithUser(req.Context(), user))
	}

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/v1/users/me/preferences", nil))
	rec := httptest.NewRecorder()
	handlePreferences(svc).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get prefs = %d", rec.Code)
	}

	req = withUser(httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/preferences",
		strings.NewReader(`{"default_subject":"OS","preferred_difficulty":"hard","default_quiz_length":15}`)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handlePreferences(svc).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch prefs = %d body=%s", rec.Code, rec.Body.String())
	}
	var prefs preferencesDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &prefs); err != nil {
		t.Fatal(err)
	}
	if prefs.PreferredDifficulty != "hard" || prefs.DefaultQuizLength != 15 {
		t.Fatalf("prefs not saved: %+v", prefs)
	}

	// Invalid difficulty is a 400.
	req = withUser(httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/preferences",
		strings.NewReader(`{"preferred_difficulty":"extreme"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handlePreferences(svc).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad prefs = %d, want 400", rec.Code)
	}

	// Anonymous is a 401.
	rec = doJSON(t, handlePreferences(svc), http.MethodGet, "/api/v1/users/me/preferences", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon prefs = %d, want 401", rec.Code)
	}
}

func TestDocumentOwnership(t *testing.T) {
	svc := testAuthService()
	ctx := context.Background()
	alice, _, err := svc.Register(ctx, "alice@example.com", "s3cur3pass", "")
	if err != nil {
		t.Fatal(err)
	}
	bob, _, err := svc.Register(ctx, "bob@example.com", "s3cur3pass", "")
	if err != nil {
		t.Fatal(err)
	}

	store, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := &ownedDocRepo{docs: map[string]documents.Document{}}
	docSvc := documents.NewService(repo, store)

	// Alice uploads: the row is attributed to her.
	aliceCtx := auth.WithUser(ctx, alice)
	owned := "alice-id"
	repo.docs[owned] = documents.Document{ID: owned, Filename: "a.pdf", Status: "uploaded", UserID: &alice.ID}

	// Bob cannot read Alice's document (masked as 404).
	bobCtx := auth.WithUser(ctx, bob)
	if _, err := docSvc.Get(bobCtx, owned); err == nil {
		t.Fatalf("bob read alice's document")
	}
	// Alice can.
	if _, err := docSvc.Get(aliceCtx, owned); err != nil {
		t.Fatalf("alice cannot read own document: %v", err)
	}
	// Legacy unowned rows stay visible to everyone.
	repo.docs["legacy"] = documents.Document{ID: "legacy", Filename: "l.pdf", Status: "uploaded"}
	if _, err := docSvc.Get(ctx, "legacy"); err != nil {
		t.Fatalf("anonymous cannot read legacy document: %v", err)
	}
	if _, err := docSvc.Get(bobCtx, "legacy"); err != nil {
		t.Fatalf("bob cannot read legacy document: %v", err)
	}
}

type ownedDocRepo struct {
	docs map[string]documents.Document
}

func (f *ownedDocRepo) List(_ context.Context, _ documents.Filter) ([]documents.Document, error) {
	out := []documents.Document{}
	for _, d := range f.docs {
		out = append(out, d)
	}
	return out, nil
}
func (f *ownedDocRepo) GetByID(_ context.Context, id string) (documents.Document, error) {
	d, ok := f.docs[id]
	if !ok {
		return documents.Document{}, apperr.ErrNotFound
	}
	return d, nil
}
func (f *ownedDocRepo) Create(_ context.Context, d *documents.Document) error { return nil }
func (f *ownedDocRepo) UpdateStatus(_ context.Context, _, _ string) error     { return nil }

func TestQuizSessionOwnership(t *testing.T) {
	authSvc := testAuthService()
	ctx := context.Background()
	alice, _, _ := authSvc.Register(ctx, "alice@example.com", "s3cur3pass", "")
	bob, _, _ := authSvc.Register(ctx, "bob@example.com", "s3cur3pass", "")

	store := &ownedEvalStore{sessions: map[string]quiz.QuizSession{}}
	eval := quiz.NewEvaluationService(store)

	aliceCtx := auth.WithUser(ctx, alice)
	created, err := eval.CreateSession(aliceCtx, quiz.QuizSession{TotalQuestions: 5})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.UserID != alice.ID {
		t.Fatalf("session not attributed: %+v", created)
	}
	// Bob's read is masked as not found.
	if _, err := eval.GetResult(auth.WithUser(ctx, bob), created.ID); err == nil {
		t.Fatalf("bob read alice's session")
	}
	// Alice's read works.
	if _, err := eval.GetResult(aliceCtx, created.ID); err != nil {
		t.Fatalf("alice cannot read own session: %v", err)
	}
	// Bob cannot submit to Alice's session either.
	if _, err := eval.SubmitAttempt(auth.WithUser(ctx, bob), quiz.QuizAttempt{
		SessionID: created.ID, QuestionID: "q1", SelectedAnswer: 0, CorrectAnswer: 0,
	}); err == nil {
		t.Fatalf("bob submitted to alice's session")
	}
}

type ownedEvalStore struct {
	sessions map[string]quiz.QuizSession
	attempts map[string][]quiz.QuizAttempt
}

func (f *ownedEvalStore) CreateSession(_ context.Context, s quiz.QuizSession) (quiz.QuizSession, error) {
	if s.ID == "" {
		s.ID = "sess-1"
	}
	f.sessions[s.ID] = s
	return s, nil
}
func (f *ownedEvalStore) GetSession(_ context.Context, id string) (quiz.QuizSession, error) {
	s, ok := f.sessions[id]
	if !ok {
		return quiz.QuizSession{}, apperr.ErrNotFound
	}
	return s, nil
}
func (f *ownedEvalStore) RecordAttempt(_ context.Context, a quiz.QuizAttempt) (quiz.QuizAttempt, error) {
	a.ID = "att-1"
	a.IsCorrect = a.SelectedAnswer == a.CorrectAnswer
	if f.attempts == nil {
		f.attempts = map[string][]quiz.QuizAttempt{}
	}
	f.attempts[a.SessionID] = append(f.attempts[a.SessionID], a)
	return a, nil
}
func (f *ownedEvalStore) ListAttempts(_ context.Context, sessionID string) ([]quiz.QuizAttempt, error) {
	return f.attempts[sessionID], nil
}

func TestQuizHistory(t *testing.T) {
	authSvc := testAuthService()
	ctx := context.Background()
	alice, _, _ := authSvc.Register(ctx, "alice@example.com", "s3cur3pass", "")

	store := &ownedEvalStore{sessions: map[string]quiz.QuizSession{}}
	eval := quiz.NewEvaluationService(store)
	aliceCtx := auth.WithUser(ctx, alice)
	if _, err := eval.CreateSession(aliceCtx, quiz.QuizSession{TotalQuestions: 3}); err != nil {
		t.Fatal(err)
	}

	// Anonymous history is an empty list (never another user's data).
	rec := doJSON(t, handleQuizHistory(eval), http.MethodGet, "/api/v1/quiz/sessions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("anon history = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("anon history = %s, want []", rec.Body.String())
	}

	// Authenticated history lists own sessions. The in-memory fake here
	// lacks ListSessionsByUser, so wire a lister-backed variant instead.
	histStore := &historyEvalStore{ownedEvalStore: store}
	histEval := quiz.NewEvaluationService(histStore)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quiz/sessions", nil)
	req = req.WithContext(aliceCtx)
	rec = httptest.NewRecorder()
	handleQuizHistory(histEval).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("history = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []quizSessionDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TotalQuestions != 3 {
		t.Fatalf("history = %+v", got)
	}

	// Nil lister is a 503.
	rec = doJSON(t, handleQuizHistory(nil), http.MethodGet, "/api/v1/quiz/sessions", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil history = %d, want 503", rec.Code)
	}
}

type historyEvalStore struct {
	*ownedEvalStore
}

func (f *historyEvalStore) ListSessionsByUser(_ context.Context, userID string, _, _ int) ([]quiz.QuizSession, error) {
	out := []quiz.QuizSession{}
	for _, s := range f.sessions {
		if s.UserID == userID {
			out = append(out, s)
		}
	}
	return out, nil
}
