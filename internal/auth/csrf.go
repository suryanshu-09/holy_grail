package auth

import (
	"net/http"
	"net/url"
	"strings"
)

// CSRFMiddleware rejects cookie-authenticated state-changing requests
// whose Origin (or Referer fallback) does not match the request host or
// the configured allowed origin. Bearer-token requests are unaffected:
// stolen-token CSRF is out of scope because the token is not ambient
// (browsers do not attach Authorization headers cross-origin).
func CSRFMiddleware(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !AuthViaCookie(r.Context()) {
				next.ServeHTTP(w, r)
				return
			}
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				if !validCSRFOrigin(r, allowedOrigin) {
					http.Error(w, `{"error":{"code":"Forbidden","message":"csrf validation failed"}}`, http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// validCSRFOrigin checks Origin first, then Referer, against the request
// host or the configured origin. Missing headers fail closed for
// cookie-authenticated mutations.
func validCSRFOrigin(r *http.Request, allowedOrigin string) bool {
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return originMatchesHost(origin, r.Host) || originMatchesAllowed(origin, allowedOrigin)
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		u, err := url.Parse(referer)
		if err != nil {
			return false
		}
		if strings.EqualFold(u.Host, r.Host) {
			return true
		}
		if allowedOrigin != "" {
			if a, err := url.Parse(allowedOrigin); err == nil && strings.EqualFold(u.Host, a.Host) {
				return true
			}
		}
		return false
	}
	return false
}

func originMatchesHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

func originMatchesAllowed(origin, allowed string) bool {
	if allowed == "" || allowed == "*" {
		return allowed == "*"
	}
	a, err := url.Parse(allowed)
	if err != nil {
		return strings.EqualFold(strings.TrimSpace(allowed), origin)
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if a.Host != "" {
		return strings.EqualFold(u.Host, a.Host)
	}
	return strings.EqualFold(strings.TrimSpace(allowed), origin)
}
