package auth

import (
	"fmt"
	"strings"
	"time"
)

// User mirrors the users table. PasswordHash is never serialized to JSON.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name,omitempty"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Session mirrors the sessions table. TokenHash stores the SHA-256 hex of
// the opaque bearer token; the raw token is only ever held client-side
// (secure cookie and/or Authorization: Bearer header).
type Session struct {
	TokenHash string    `json:"-"`
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Preferences mirrors the user_preferences table: per-user defaults for
// quiz generation and display.
type Preferences struct {
	UserID              string    `json:"user_id"`
	DefaultSubject      string    `json:"default_subject,omitempty"`
	PreferredDifficulty string    `json:"preferred_difficulty,omitempty"`
	DefaultQuizLength   int       `json:"default_quiz_length,omitempty"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// ValidatePreferences normalizes and checks preference values.
func (p *Preferences) ValidatePreferences() error {
	if p == nil {
		return fmt.Errorf("auth: preferences are nil")
	}
	if strings.TrimSpace(p.UserID) == "" {
		return fmt.Errorf("auth: user_id is required")
	}
	p.DefaultSubject = strings.TrimSpace(p.DefaultSubject)
	d := strings.ToLower(strings.TrimSpace(p.PreferredDifficulty))
	switch d {
	case "", "easy", "medium", "hard":
		p.PreferredDifficulty = d
	default:
		return fmt.Errorf("auth: invalid preferred_difficulty %q: must be easy, medium or hard", p.PreferredDifficulty)
	}
	if p.DefaultQuizLength < 0 || p.DefaultQuizLength > 100 {
		return fmt.Errorf("auth: default_quiz_length %d out of range [0,100]", p.DefaultQuizLength)
	}
	return nil
}

// NormalizeEmail lowercases and trims an email address.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail reports whether email looks like a usable login identifier.
func ValidateEmail(email string) error {
	email = NormalizeEmail(email)
	if email == "" {
		return fmt.Errorf("auth: email is required")
	}
	if len(email) > 254 {
		return fmt.Errorf("auth: email too long")
	}
	at := strings.Index(email, "@")
	if at < 1 || at == len(email)-1 {
		return fmt.Errorf("auth: invalid email %q", email)
	}
	if !strings.Contains(email[at+1:], ".") {
		return fmt.Errorf("auth: invalid email %q", email)
	}
	return nil
}

// ValidatePassword enforces the registration/login password policy.
// bcrypt caps inputs at 72 bytes, so longer passwords are rejected early.
func ValidatePassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("auth: password must be at least 8 characters")
	}
	if len(password) > 72 {
		return fmt.Errorf("auth: password must be at most 72 characters")
	}
	return nil
}
