package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testService() *Service {
	svc := NewService(NewMemoryStore())
	svc.SetNowFunc(time.Now)
	return svc
}

func TestRegisterAndLogin(t *testing.T) {
	svc := testService()
	ctx := context.Background()

	user, token, err := svc.Register(ctx, "Alice@Example.com", "s3cur3pass", "Alice")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user.Email != "alice@example.com" {
		t.Fatalf("email not normalized: %q", user.Email)
	}
	if user.PasswordHash == "s3cur3pass" || user.PasswordHash == "" {
		t.Fatalf("password not hashed")
	}
	if token == "" {
		t.Fatalf("no session token issued")
	}

	got, err := svc.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("authenticated wrong user")
	}

	// Duplicate registration must fail.
	if _, _, err := svc.Register(ctx, "alice@example.com", "anotherpass", ""); err == nil {
		t.Fatalf("duplicate register succeeded")
	}

	// Login with correct password issues a new token.
	_, token2, err := svc.Login(ctx, "alice@example.com", "s3cur3pass")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token2 == "" || token2 == token {
		t.Fatalf("login did not issue a fresh token")
	}

	// Wrong password and unknown email both yield unauthorized.
	if _, _, err := svc.Login(ctx, "alice@example.com", "wrongpass1"); err == nil {
		t.Fatalf("login with wrong password succeeded")
	}
	if _, _, err := svc.Login(ctx, "nobody@example.com", "s3cur3pass"); err == nil {
		t.Fatalf("login with unknown email succeeded")
	}
}

func TestValidation(t *testing.T) {
	svc := testService()
	ctx := context.Background()
	if _, _, err := svc.Register(ctx, "not-an-email", "s3cur3pass", ""); err == nil {
		t.Fatalf("invalid email accepted")
	}
	if _, _, err := svc.Register(ctx, "a@b.co", "short", ""); err == nil {
		t.Fatalf("short password accepted")
	}
}

func TestLogoutAndExpiry(t *testing.T) {
	svc := testService()
	ctx := context.Background()

	_, token, err := svc.Register(ctx, "bob@example.com", "s3cur3pass", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(ctx, token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := svc.Authenticate(ctx, token); err == nil {
		t.Fatalf("revoked token still authenticates")
	}
	// Logout is idempotent.
	if err := svc.Logout(ctx, token); err != nil {
		t.Fatalf("second logout: %v", err)
	}

	// Expired sessions are rejected (and revoked lazily).
	svc.SetTTL(time.Millisecond)
	_, expiring, err := svc.Register(ctx, "carol@example.com", "s3cur3pass", "")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := svc.Authenticate(ctx, expiring); err == nil {
		t.Fatalf("expired token authenticates")
	}
}

func TestPreferences(t *testing.T) {
	svc := testService()
	ctx := context.Background()
	user, _, err := svc.Register(ctx, "dave@example.com", "s3cur3pass", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetPreferences(ctx, user.ID)
	if err != nil {
		t.Fatalf("get prefs: %v", err)
	}
	if got.UserID != user.ID {
		t.Fatalf("prefs user mismatch")
	}
	updated, err := svc.UpdatePreferences(ctx, user.ID, Preferences{
		DefaultSubject:      "Operating Systems",
		PreferredDifficulty: "medium",
		DefaultQuizLength:   10,
	})
	if err != nil {
		t.Fatalf("update prefs: %v", err)
	}
	if updated.PreferredDifficulty != "medium" || updated.DefaultQuizLength != 10 {
		t.Fatalf("prefs not stored: %+v", updated)
	}
	if _, err := svc.UpdatePreferences(ctx, user.ID, Preferences{PreferredDifficulty: "extreme"}); err == nil {
		t.Fatalf("invalid difficulty accepted")
	}
}

func TestTokenFromRequest(t *testing.T) {
	newReq := func(bearer, cookie string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: cookie})
		}
		return r
	}
	tok, viaCookie := TokenFromRequest(newReq("abc123", ""))
	if tok != "abc123" || viaCookie {
		t.Fatalf("bearer parse = %q cookie=%v", tok, viaCookie)
	}
	tok, viaCookie = TokenFromRequest(newReq("", "cookietok"))
	if tok != "cookietok" || !viaCookie {
		t.Fatalf("cookie parse = %q cookie=%v", tok, viaCookie)
	}
	// Bearer takes precedence over cookie.
	tok, viaCookie = TokenFromRequest(newReq("abc123", "cookietok"))
	if tok != "abc123" || viaCookie {
		t.Fatalf("precedence = %q cookie=%v", tok, viaCookie)
	}
}
