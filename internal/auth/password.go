package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword hashes a plaintext password with bcrypt (DefaultCost).
// The caller must run ValidatePassword first so over-long inputs are
// rejected with a clear error instead of bcrypt's generic one.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword compares a bcrypt hash against a plaintext candidate.
// It returns nil on match, else an error (hash mismatch or malformed hash).
func VerifyPassword(hash, password string) error {
	if hash == "" || password == "" {
		return fmt.Errorf("auth: invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return fmt.Errorf("auth: invalid credentials")
	}
	return nil
}
