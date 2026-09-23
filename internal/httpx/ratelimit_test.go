package httpx

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterWindow(t *testing.T) {
	l := NewRateLimiter(2, time.Minute)
	now := time.Now()
	l.SetNowFunc(func() time.Time { return now })

	if !l.Allow("ip") || !l.Allow("ip") {
		t.Fatalf("first two events should pass")
	}
	if l.Allow("ip") {
		t.Fatalf("third event within window should be rejected")
	}
	// A different key has its own bucket.
	if !l.Allow("other") {
		t.Fatalf("other key should pass")
	}
	// After the window, the bucket refills.
	now = now.Add(2 * time.Minute)
	if !l.Allow("ip") {
		t.Fatalf("event after window should pass")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.5:1234"
	if got := ClientIP(r); got != "10.0.0.5" {
		t.Fatalf("remote addr ip = %q", got)
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if got := ClientIP(r); got != "203.0.113.7" {
		t.Fatalf("forwarded ip = %q", got)
	}
}
