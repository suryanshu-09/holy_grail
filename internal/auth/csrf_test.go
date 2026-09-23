package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRFMiddleware(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	h := CSRFMiddleware("https://app.example.com")(http.HandlerFunc(ok))

	// Bearer (non-cookie) requests skip CSRF.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer request = %d, want 200", rec.Code)
	}

	withCookie := func(method, origin, referer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/documents", nil)
		req.Host = "api.example.com"
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		req = req.WithContext(withAuthViaCookie(context.Background()))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// Missing headers fail closed.
	if rec := withCookie(http.MethodPost, "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin = %d, want 403", rec.Code)
	}
	// Matching Origin passes.
	if rec := withCookie(http.MethodPost, "https://api.example.com", ""); rec.Code != http.StatusOK {
		t.Fatalf("same-host origin = %d, want 200", rec.Code)
	}
	// Allowed-origin passes.
	if rec := withCookie(http.MethodPost, "https://app.example.com", ""); rec.Code != http.StatusOK {
		t.Fatalf("allowed origin = %d, want 200", rec.Code)
	}
	// Foreign Origin is rejected.
	if rec := withCookie(http.MethodPost, "https://evil.example.com", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign origin = %d, want 403", rec.Code)
	}
	// Referer fallback: same host passes.
	if rec := withCookie(http.MethodPost, "", "https://api.example.com/x"); rec.Code != http.StatusOK {
		t.Fatalf("same-host referer = %d, want 200", rec.Code)
	}
	// Safe methods are unaffected even without headers.
	if rec := withCookie(http.MethodGet, "", ""); rec.Code != http.StatusOK {
		t.Fatalf("safe method = %d, want 200", rec.Code)
	}
}
