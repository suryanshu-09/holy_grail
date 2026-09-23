package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

// DefaultSessionTTL is the lifetime of a login session (30 days).
const DefaultSessionTTL = 30 * 24 * time.Hour

// Service contains the business rules for authentication: registration,
// login (bcrypt verification), session issuance/validation/revocation,
// and per-user preferences.
type Service struct {
	store Store
	ttl   time.Duration
	now   func() time.Time
}

// NewService creates an auth service. A nil store makes every method fail
// closed; handlers map that to 503 like other unconfigured pipelines.
func NewService(store Store) *Service {
	return &Service{store: store, ttl: DefaultSessionTTL, now: time.Now}
}

// SetTTL overrides the session lifetime (tests).
func (s *Service) SetTTL(d time.Duration) {
	if d > 0 {
		s.ttl = d
	}
}

// SetNowFunc overrides the clock (tests).
func (s *Service) SetNowFunc(fn func() time.Time) {
	if fn != nil {
		s.now = fn
	}
}

// Register creates a user and immediately issues a session token.
// Duplicate emails yield apperr.ErrDuplicate (mapped to 409 by handlers).
func (s *Service) Register(ctx context.Context, email, password, displayName string) (User, string, error) {
	if s == nil || s.store == nil {
		return User{}, "", errors.New("auth: store not configured")
	}
	email = NormalizeEmail(email)
	if err := ValidateEmail(email); err != nil {
		return User{}, "", err
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, "", err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, "", err
	}
	id, err := NewID()
	if err != nil {
		return User{}, "", err
	}
	user, err := s.store.CreateUser(ctx, User{
		ID:           id,
		Email:        email,
		DisplayName:  strings.TrimSpace(displayName),
		PasswordHash: hash,
	})
	if err != nil {
		return User{}, "", err
	}
	token, err := s.issueSession(ctx, user.ID)
	if err != nil {
		return User{}, "", err
	}
	return user, token, nil
}

// Login verifies credentials and issues a fresh session token.
// Unknown emails and wrong passwords both yield apperr.ErrUnauthorized
// so callers cannot enumerate registered addresses.
func (s *Service) Login(ctx context.Context, email, password string) (User, string, error) {
	if s == nil || s.store == nil {
		return User{}, "", errors.New("auth: store not configured")
	}
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		// Run a dummy compare so timing does not reveal whether the
		// email exists (constant-time-ish enumeration resistance).
		_ = VerifyPassword("$2a$10$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", password)
		return User{}, "", apperr.ErrUnauthorized
	}
	if err := VerifyPassword(user.PasswordHash, password); err != nil {
		return User{}, "", apperr.ErrUnauthorized
	}
	token, err := s.issueSession(ctx, user.ID)
	if err != nil {
		return User{}, "", err
	}
	return user, token, nil
}

// Authenticate resolves a raw bearer token to its user. Expired sessions
// are revoked lazily and report apperr.ErrUnauthorized, as do unknown
// tokens (fail closed).
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if s == nil || s.store == nil {
		return User{}, errors.New("auth: store not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return User{}, apperr.ErrUnauthorized
	}
	sess, err := s.store.GetSessionByHash(ctx, HashToken(token))
	if err != nil {
		return User{}, apperr.ErrUnauthorized
	}
	if !sess.ExpiresAt.IsZero() && !s.now().Before(sess.ExpiresAt) {
		_ = s.store.DeleteSession(ctx, sess.TokenHash)
		return User{}, apperr.ErrUnauthorized
	}
	user, err := s.store.GetUserByID(ctx, sess.UserID)
	if err != nil {
		return User{}, apperr.ErrUnauthorized
	}
	return user, nil
}

// Logout revokes one session token. Unknown tokens still return nil so
// logout is idempotent from the client's perspective.
func (s *Service) Logout(ctx context.Context, token string) error {
	if s == nil || s.store == nil {
		return errors.New("auth: store not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if err := s.store.DeleteSession(ctx, HashToken(token)); err != nil && !errors.Is(err, apperr.ErrNotFound) {
		return err
	}
	return nil
}

// GetPreferences returns the user's preferences (zero value when unset).
func (s *Service) GetPreferences(ctx context.Context, userID string) (Preferences, error) {
	if s == nil || s.store == nil {
		return Preferences{}, errors.New("auth: store not configured")
	}
	if strings.TrimSpace(userID) == "" {
		return Preferences{}, errors.New("auth: user_id is required")
	}
	return s.store.GetPreferences(ctx, strings.TrimSpace(userID))
}

// UpdatePreferences validates and stores the user's preferences.
func (s *Service) UpdatePreferences(ctx context.Context, userID string, p Preferences) (Preferences, error) {
	if s == nil || s.store == nil {
		return Preferences{}, errors.New("auth: store not configured")
	}
	if strings.TrimSpace(userID) == "" {
		return Preferences{}, errors.New("auth: user_id is required")
	}
	p.UserID = strings.TrimSpace(userID)
	if err := p.ValidatePreferences(); err != nil {
		return Preferences{}, err
	}
	return s.store.UpsertPreferences(ctx, p)
}

// issueSession mints a token, persists its hash with an expiry, and
// returns the raw token to hand to the client.
func (s *Service) issueSession(ctx context.Context, userID string) (string, error) {
	token, err := NewToken()
	if err != nil {
		return "", err
	}
	if _, err := s.store.CreateSession(ctx, Session{
		TokenHash: HashToken(token),
		UserID:    userID,
		ExpiresAt: s.now().Add(s.ttl),
	}); err != nil {
		return "", err
	}
	return token, nil
}
