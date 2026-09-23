package auth

import (
	"context"
	"net/http"
	"strings"
)

// SessionCookieName is the session cookie set on login/register.
const SessionCookieName = "hg_session"

// context keys for the authenticated user and the transport that
// carried the credential (needed for CSRF enforcement).
type ctxKey string

const (
	userKey      ctxKey = "auth_user"
	viaCookieKey ctxKey = "auth_via_cookie"
)

// WithUser attaches a user to the context (tests, middleware).
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// UserFromContext returns the authenticated user, if any.
func UserFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey).(User)
	if !ok || u.ID == "" {
		return User{}, false
	}
	return u, true
}

// UserIDFromContext returns the authenticated user ID or "" when anonymous.
func UserIDFromContext(ctx context.Context) string {
	if u, ok := UserFromContext(ctx); ok {
		return u.ID
	}
	return ""
}

// withAuthViaCookie marks the request as cookie-authenticated.
func withAuthViaCookie(ctx context.Context) context.Context {
	return context.WithValue(ctx, viaCookieKey, true)
}

// AuthViaCookie reports whether the request was authenticated with the
// session cookie (as opposed to a Bearer token). Cookie sessions are
// subject to CSRF checks on state-changing methods.
func AuthViaCookie(ctx context.Context) bool {
	v, _ := ctx.Value(viaCookieKey).(bool)
	return v
}

// TokenFromRequest extracts the session token: Authorization: Bearer
// takes precedence, falling back to the session cookie.
func TokenFromRequest(r *http.Request) (token string, viaCookie bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		parts := strings.SplitN(h, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			if t := strings.TrimSpace(parts[1]); t != "" {
				return t, false
			}
		}
	}
	if c, err := r.Cookie(SessionCookieName); err == nil {
		if t := strings.TrimSpace(c.Value); t != "" {
			return t, true
		}
	}
	return "", false
}

// OptionalAuth attaches the user to the request context when a valid
// credential is present, and passes anonymous requests through untouched.
// A nil service disables auth (all requests anonymous, handlers stay
// backward-compatible).
func OptionalAuth(svc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if svc == nil {
				next.ServeHTTP(w, r)
				return
			}
			token, viaCookie := TokenFromRequest(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			user, err := svc.Authenticate(r.Context(), token)
			if err != nil {
				// Invalid/expired credentials are ignored here (anonymous);
				// protected endpoints use RequireAuth to reject them.
				next.ServeHTTP(w, r)
				return
			}
			ctx := WithUser(r.Context(), user)
			if viaCookie {
				ctx = withAuthViaCookie(ctx)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// authenticator is the subset of *Service needed by RequireAuth.
type authenticator interface {
	Authenticate(ctx context.Context, token string) (User, error)
}

// RequireAuth rejects requests without a valid credential (401) and
// attaches the user otherwise. It re-validates the token so it is safe
// to mount on routes even without OptionalAuth in the chain.
func RequireAuth(svc authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if svc == nil {
				http.Error(w, `{"error":{"code":"Service Unavailable","message":"auth not configured"}}`, http.StatusServiceUnavailable)
				return
			}
			if _, ok := UserFromContext(r.Context()); ok {
				next.ServeHTTP(w, r)
				return
			}
			token, viaCookie := TokenFromRequest(r)
			if token == "" {
				http.Error(w, `{"error":{"code":"Unauthorized","message":"authentication required"}}`, http.StatusUnauthorized)
				return
			}
			user, err := svc.Authenticate(r.Context(), token)
			if err != nil {
				http.Error(w, `{"error":{"code":"Unauthorized","message":"invalid or expired session"}}`, http.StatusUnauthorized)
				return
			}
			ctx := WithUser(r.Context(), user)
			if viaCookie {
				ctx = withAuthViaCookie(ctx)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SetSessionCookie writes the session cookie. Secure is set in production
// (APP_ENV=production); HttpOnly + SameSite=Lax are always set.
func SetSessionCookie(w http.ResponseWriter, token string, secure bool, maxAge int) {
	c := &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   maxAge,
	}
	http.SetCookie(w, c)
}

// ClearSessionCookie expires the session cookie (same flags as set so the
// browser actually drops it).
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	c := &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   -1,
	}
	http.SetCookie(w, c)
}
