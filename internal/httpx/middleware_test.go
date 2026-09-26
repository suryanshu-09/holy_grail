package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestCORSConcreteOrigin(t *testing.T) {
	h := CORS("https://app.example.com")(okHandler())
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Origin", "https://app.example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("Allow-Origin = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Allow-Credentials = %q, want true", got)
	}
}

func TestCORSMismatchedOriginNotEchoed(t *testing.T) {
	h := CORS("https://app.example.com")(okHandler())
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got == "https://evil.example.com" {
		t.Fatalf("must not echo untrusted origin")
	}
}

func TestCORSWildcardNoCredentials(t *testing.T) {
	h := CORS("*")(okHandler())
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin = %q, want *", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("wildcard must not set Allow-Credentials, got %q", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	h := CORS("https://app.example.com")(okHandler())
	r := httptest.NewRequest("OPTIONS", "/", nil)
	r.Header.Set("Origin", "https://app.example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d", w.Code)
	}
}

func TestSecurityHeadersHSTS(t *testing.T) {
	h := SecurityHeaders(okHandler())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if got := w.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatalf("HSTS header missing")
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
}

func TestRateLimitMiddleware429RetryAfter(t *testing.T) {
	limiter := NewRateLimiter(2, time.Minute)
	h := RateLimitMiddleware(limiter)(okHandler())
	var last *httptest.ResponseRecorder
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "10.0.0.1:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		last = w
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", last.Code)
	}
	if got := last.Header().Get("Retry-After"); got == "" {
		t.Fatalf("Retry-After header missing on 429")
	}
}

func TestRateLimitMiddlewareNilPassthrough(t *testing.T) {
	h := RateLimitMiddleware(nil)(okHandler())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("nil limiter should passthrough, got %d", w.Code)
	}
}

func TestRateLimitMiddlewareOptionsBypass(t *testing.T) {
	limiter := NewRateLimiter(1, time.Minute)
	h := RateLimitMiddleware(limiter)(okHandler())
	r1 := httptest.NewRequest("GET", "/", nil)
	r1.RemoteAddr = "10.0.0.2:1"
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request = %d", w1.Code)
	}
	// Preflight from same IP must still pass even though budget is spent.
	r2 := httptest.NewRequest("OPTIONS", "/", nil)
	r2.RemoteAddr = "10.0.0.2:1"
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("OPTIONS bypass failed, got %d", w2.Code)
	}
}
