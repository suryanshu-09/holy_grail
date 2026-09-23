package httpx

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter is a fixed-window in-memory rate limiter keyed by an
// arbitrary string (client IP, user ID, ...). It is intended for
// low-volume abuse protection (login/register bursts), not precise
// distributed throttling: workers do not share state.
//
// The zero value is unusable; construct with NewRateLimiter.
type RateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	hits   map[string][]time.Time
}

// NewRateLimiter allows at most limit events per window for each key.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	return &RateLimiter{
		limit:  limit,
		window: window,
		now:    time.Now,
		hits:   map[string][]time.Time{},
	}
}

// SetNowFunc overrides the clock (tests).
func (l *RateLimiter) SetNowFunc(fn func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if fn != nil {
		l.now = fn
	}
}

// Allow reports whether an event for key may proceed, recording it when
// allowed. Events older than the window are forgotten.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

// ClientIP extracts the caller IP for rate-limit keys, preferring
// X-Forwarded-For (first entry) when behind a proxy.
func ClientIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if first, _, _ := strings.Cut(fwd, ","); strings.TrimSpace(first) != "" {
			return strings.TrimSpace(first)
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
