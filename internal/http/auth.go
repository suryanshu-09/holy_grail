package http

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/auth"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
)

// authRateLimit caps login/register attempts per client IP.
var authRateLimit = httpx.NewRateLimiter(10, time.Minute)

// sessionCookieMaxAge mirrors DefaultSessionTTL in seconds for the cookie.
const sessionCookieMaxAge = 30 * 24 * 60 * 60

// secureCookies reports whether the Secure flag should be set (production).
func secureCookies(cfgEnv string) bool {
	return strings.EqualFold(strings.TrimSpace(cfgEnv), "production")
}

// registerRequest is the JSON body for POST /api/v1/auth/register.
type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// loginRequest is the JSON body for POST /api/v1/auth/login.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// userDTO is the wire shape for a user (matches lib/api.ts AuthUser;
// the password hash is never exposed).
type userDTO struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

func toUserDTO(u auth.User) userDTO {
	dto := userDTO{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName}
	if !u.CreatedAt.IsZero() {
		dto.CreatedAt = u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return dto
}

// sessionDTO is the login/register response: the user plus the bearer
// token (also set as the hg_session cookie for browser clients).
type sessionDTO struct {
	User  userDTO `json:"user"`
	Token string  `json:"token"`
}

// writeSession issues the cookie + JSON body for a fresh session.
func writeSession(w http.ResponseWriter, env string, user auth.User, token string, status int) {
	auth.SetSessionCookie(w, token, secureCookies(env), sessionCookieMaxAge)
	httpx.WriteJSON(w, status, sessionDTO{User: toUserDTO(user), Token: token})
}

// writeAuthError maps auth service errors to HTTP statuses.
func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apperr.ErrDuplicate):
		httpx.Error(w, http.StatusConflict, "email already registered")
	case errors.Is(err, apperr.ErrUnauthorized):
		httpx.Error(w, http.StatusUnauthorized, "invalid email or password")
	case err != nil && isAuthValidationError(err):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	default:
		httpx.LogError("auth failed", err)
		httpx.Error(w, http.StatusInternalServerError, "authentication failed")
	}
}

// isAuthValidationError reports client (400) errors by message marker.
func isAuthValidationError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{
		"is required", "invalid", "must be", "too long", "at least", "at most", "out of range",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// checkAuthRateLimit rejects bursts with 429 + Retry-After.
func checkAuthRateLimit(w http.ResponseWriter, r *http.Request) bool {
	if !authRateLimit.Allow(httpx.ClientIP(r)) {
		w.Header().Set("Retry-After", "60")
		httpx.Error(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return false
	}
	return true
}

// decodeAuthBody parses a JSON body with the shared size cap.
func decodeAuthBody(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	if msg, tooLarge := httpx.DecodeJSONBody(w, r, dst, httpx.MaxJSONBodyBytes); msg != "" {
		if tooLarge {
			httpx.Error(w, http.StatusRequestEntityTooLarge, msg)
		} else {
			httpx.Error(w, http.StatusBadRequest, msg)
		}
		return false
	}
	return true
}

// handleRegister handles POST /api/v1/auth/register.
func handleRegister(svc *auth.Service, env string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if svc == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "auth not configured")
			return
		}
		if !checkAuthRateLimit(w, r) {
			return
		}
		var body registerRequest
		if !decodeAuthBody(w, r, &body) {
			return
		}
		user, token, err := svc.Register(r.Context(), body.Email, body.Password, body.DisplayName)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		writeSession(w, env, user, token, http.StatusCreated)
	})
}

// handleLogin handles POST /api/v1/auth/login.
func handleLogin(svc *auth.Service, env string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if svc == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "auth not configured")
			return
		}
		if !checkAuthRateLimit(w, r) {
			return
		}
		var body loginRequest
		if !decodeAuthBody(w, r, &body) {
			return
		}
		user, token, err := svc.Login(r.Context(), body.Email, body.Password)
		if err != nil {
			// Map invalid-credentials to 401 without revealing which
			// field was wrong (the service already normalizes both).
			if errors.Is(err, apperr.ErrUnauthorized) {
				httpx.Error(w, http.StatusUnauthorized, "invalid email or password")
				return
			}
			writeAuthError(w, err)
			return
		}
		writeSession(w, env, user, token, http.StatusOK)
	})
}

// handleLogout handles POST /api/v1/auth/logout: revokes the current
// session and clears the cookie. Idempotent (always 200 when configured).
func handleLogout(svc *auth.Service, env string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if svc == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "auth not configured")
			return
		}
		if token, _ := auth.TokenFromRequest(r); token != "" {
			_ = svc.Logout(r.Context(), token)
		}
		auth.ClearSessionCookie(w, secureCookies(env))
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
	})
}

// handleMe handles GET /api/v1/auth/me: the current user or 401.
func handleMe() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "authentication required")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, toUserDTO(user))
	})
}

// preferencesDTO is the wire shape for user preferences
// (matches lib/api.ts UserPreferences).
type preferencesDTO struct {
	UserID              string `json:"user_id"`
	DefaultSubject      string `json:"default_subject,omitempty"`
	PreferredDifficulty string `json:"preferred_difficulty,omitempty"`
	DefaultQuizLength   int    `json:"default_quiz_length,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
}

func toPreferencesDTO(p auth.Preferences) preferencesDTO {
	dto := preferencesDTO{
		UserID:              p.UserID,
		DefaultSubject:      p.DefaultSubject,
		PreferredDifficulty: p.PreferredDifficulty,
		DefaultQuizLength:   p.DefaultQuizLength,
	}
	if !p.UpdatedAt.IsZero() {
		dto.UpdatedAt = p.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return dto
}

// handlePreferences serves GET + PATCH /api/v1/users/me/preferences.
func handlePreferences(svc *auth.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "auth not configured")
			return
		}
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "authentication required")
			return
		}
		switch r.Method {
		case http.MethodGet:
			prefs, err := svc.GetPreferences(r.Context(), user.ID)
			if err != nil {
				httpx.LogError("get preferences failed", err)
				httpx.Error(w, http.StatusInternalServerError, "failed to load preferences")
				return
			}
			httpx.WriteJSON(w, http.StatusOK, toPreferencesDTO(prefs))
		case http.MethodPatch:
			var body struct {
				DefaultSubject      *string `json:"default_subject"`
				PreferredDifficulty *string `json:"preferred_difficulty"`
				DefaultQuizLength   *int    `json:"default_quiz_length"`
			}
			if !decodeAuthBody(w, r, &body) {
				return
			}
			current, err := svc.GetPreferences(r.Context(), user.ID)
			if err != nil {
				httpx.LogError("get preferences failed", err)
				httpx.Error(w, http.StatusInternalServerError, "failed to load preferences")
				return
			}
			if body.DefaultSubject != nil {
				current.DefaultSubject = *body.DefaultSubject
			}
			if body.PreferredDifficulty != nil {
				current.PreferredDifficulty = *body.PreferredDifficulty
			}
			if body.DefaultQuizLength != nil {
				current.DefaultQuizLength = *body.DefaultQuizLength
			}
			updated, err := svc.UpdatePreferences(r.Context(), user.ID, current)
			if err != nil {
				if isAuthValidationError(err) {
					httpx.Error(w, http.StatusBadRequest, err.Error())
					return
				}
				httpx.LogError("update preferences failed", err)
				httpx.Error(w, http.StatusInternalServerError, "failed to save preferences")
				return
			}
			httpx.WriteJSON(w, http.StatusOK, toPreferencesDTO(updated))
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPatch)
		}
	})
}
