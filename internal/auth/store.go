package auth

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

// Store is the persistence contract for users, sessions and preferences.
type Store interface {
	CreateUser(ctx context.Context, u User) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error)
	GetUserByID(ctx context.Context, id string) (User, error)
	CreateSession(ctx context.Context, s Session) (Session, error)
	GetSessionByHash(ctx context.Context, tokenHash string) (Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	ListSessionsByUser(ctx context.Context, userID string) ([]Session, error)
	GetPreferences(ctx context.Context, userID string) (Preferences, error)
	UpsertPreferences(ctx context.Context, p Preferences) (Preferences, error)
}

// postgresStore is the SQL implementation of Store
// (schema: migrations/009_add_auth.sql).
type postgresStore struct {
	db *sql.DB
}

// NewPostgresStore creates an auth Store backed by PostgreSQL.
func NewPostgresStore(db *sql.DB) Store {
	return &postgresStore{db: db}
}

func (s *postgresStore) CreateUser(ctx context.Context, u User) (User, error) {
	var out User
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO users (id, email, display_name, password_hash)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, email, display_name, password_hash, created_at, updated_at`,
		u.ID, u.Email, u.DisplayName, u.PasswordHash,
	).Scan(&out.ID, &out.Email, &out.DisplayName, &out.PasswordHash, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, fmt.Errorf("auth: email already registered: %w", apperr.ErrDuplicate)
		}
		return User{}, fmt.Errorf("auth: create user: %w", err)
	}
	return out, nil
}

func (s *postgresStore) GetUserByEmail(ctx context.Context, email string) (User, error) {
	var out User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, display_name, password_hash, created_at, updated_at
		 FROM users WHERE email = $1`, NormalizeEmail(email),
	).Scan(&out.ID, &out.Email, &out.DisplayName, &out.PasswordHash, &out.CreatedAt, &out.UpdatedAt)
	if err == sql.ErrNoRows {
		return User{}, apperr.ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: get user by email: %w", err)
	}
	return out, nil
}

func (s *postgresStore) GetUserByID(ctx context.Context, id string) (User, error) {
	var out User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, display_name, password_hash, created_at, updated_at
		 FROM users WHERE id = $1`, strings.TrimSpace(id),
	).Scan(&out.ID, &out.Email, &out.DisplayName, &out.PasswordHash, &out.CreatedAt, &out.UpdatedAt)
	if err == sql.ErrNoRows {
		return User{}, apperr.ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: get user by id: %w", err)
	}
	return out, nil
}

func (s *postgresStore) CreateSession(ctx context.Context, sess Session) (Session, error) {
	var out Session
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at)
		 VALUES ($1, $2, $3)
		 RETURNING token_hash, user_id, created_at, expires_at`,
		sess.TokenHash, sess.UserID, sess.ExpiresAt,
	).Scan(&out.TokenHash, &out.UserID, &out.CreatedAt, &out.ExpiresAt)
	if err != nil {
		return Session{}, fmt.Errorf("auth: create session: %w", err)
	}
	return out, nil
}

func (s *postgresStore) GetSessionByHash(ctx context.Context, tokenHash string) (Session, error) {
	var out Session
	err := s.db.QueryRowContext(ctx,
		`SELECT token_hash, user_id, created_at, expires_at
		 FROM sessions WHERE token_hash = $1`, tokenHash,
	).Scan(&out.TokenHash, &out.UserID, &out.CreatedAt, &out.ExpiresAt)
	if err == sql.ErrNoRows {
		return Session{}, apperr.ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("auth: get session: %w", err)
	}
	return out, nil
}

func (s *postgresStore) DeleteSession(ctx context.Context, tokenHash string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	if err != nil {
		return fmt.Errorf("auth: delete session: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (s *postgresStore) ListSessionsByUser(ctx context.Context, userID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT token_hash, user_id, created_at, expires_at
		 FROM sessions WHERE user_id = $1 ORDER BY created_at DESC`, strings.TrimSpace(userID))
	if err != nil {
		return nil, fmt.Errorf("auth: list sessions: %w", err)
	}
	defer rows.Close()
	out := make([]Session, 0)
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.TokenHash, &sess.UserID, &sess.CreatedAt, &sess.ExpiresAt); err != nil {
			return nil, fmt.Errorf("auth: scan session: %w", err)
		}
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: rows sessions: %w", err)
	}
	return out, nil
}

func (s *postgresStore) GetPreferences(ctx context.Context, userID string) (Preferences, error) {
	var out Preferences
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, default_subject, preferred_difficulty, default_quiz_length, updated_at
		 FROM user_preferences WHERE user_id = $1`, strings.TrimSpace(userID),
	).Scan(&out.UserID, &out.DefaultSubject, &out.PreferredDifficulty, &out.DefaultQuizLength, &out.UpdatedAt)
	if err == sql.ErrNoRows {
		return Preferences{UserID: strings.TrimSpace(userID)}, nil
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("auth: get preferences: %w", err)
	}
	return out, nil
}

func (s *postgresStore) UpsertPreferences(ctx context.Context, p Preferences) (Preferences, error) {
	var out Preferences
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO user_preferences (user_id, default_subject, preferred_difficulty, default_quiz_length, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (user_id) DO UPDATE SET
		   default_subject = EXCLUDED.default_subject,
		   preferred_difficulty = EXCLUDED.preferred_difficulty,
		   default_quiz_length = EXCLUDED.default_quiz_length,
		   updated_at = now()
		 RETURNING user_id, default_subject, preferred_difficulty, default_quiz_length, updated_at`,
		p.UserID, p.DefaultSubject, p.PreferredDifficulty, p.DefaultQuizLength,
	).Scan(&out.UserID, &out.DefaultSubject, &out.PreferredDifficulty, &out.DefaultQuizLength, &out.UpdatedAt)
	if err != nil {
		return Preferences{}, fmt.Errorf("auth: upsert preferences: %w", err)
	}
	return out, nil
}

// isUniqueViolation reports Postgres unique-constraint violations (SQLSTATE
// 23505) without importing a driver-specific package.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "23505") ||
		strings.Contains(err.Error(), "duplicate key")
}

// memoryStore is an in-memory Store for unit tests and environments
// without a database. It mirrors Postgres semantics: emails are unique
// (case-insensitive), unknown lookups yield apperr.ErrNotFound.
type memoryStore struct {
	mu    sync.Mutex
	users map[string]User    // id -> user
	email map[string]string  // normalized email -> id
	sess  map[string]Session // token_hash -> session
	prefs map[string]Preferences
}

// NewMemoryStore creates an empty in-memory auth store.
func NewMemoryStore() Store {
	return &memoryStore{
		users: map[string]User{},
		email: map[string]string{},
		sess:  map[string]Session{},
		prefs: map[string]Preferences{},
	}
}

func (s *memoryStore) CreateUser(_ context.Context, u User) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email := NormalizeEmail(u.Email)
	if _, exists := s.email[email]; exists {
		return User{}, fmt.Errorf("auth: email already registered: %w", apperr.ErrDuplicate)
	}
	now := time.Now()
	u.Email = email
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	u.UpdatedAt = now
	s.users[u.ID] = u
	s.email[email] = u.ID
	return u, nil
}

func (s *memoryStore) GetUserByEmail(_ context.Context, email string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.email[NormalizeEmail(email)]
	if !ok {
		return User{}, apperr.ErrNotFound
	}
	return s.users[id], nil
}

func (s *memoryStore) GetUserByID(_ context.Context, id string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[strings.TrimSpace(id)]
	if !ok {
		return User{}, apperr.ErrNotFound
	}
	return u, nil
}

func (s *memoryStore) CreateSession(_ context.Context, sess Session) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now()
	}
	s.sess[sess.TokenHash] = sess
	return sess, nil
}

func (s *memoryStore) GetSessionByHash(_ context.Context, tokenHash string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sess[tokenHash]
	if !ok {
		return Session{}, apperr.ErrNotFound
	}
	return sess, nil
}

func (s *memoryStore) DeleteSession(_ context.Context, tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sess[tokenHash]; !ok {
		return apperr.ErrNotFound
	}
	delete(s.sess, tokenHash)
	return nil
}

func (s *memoryStore) ListSessionsByUser(_ context.Context, userID string) ([]Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Session, 0)
	for _, sess := range s.sess {
		if sess.UserID == strings.TrimSpace(userID) {
			out = append(out, sess)
		}
	}
	return out, nil
}

func (s *memoryStore) GetPreferences(_ context.Context, userID string) (Preferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.prefs[strings.TrimSpace(userID)]; ok {
		return p, nil
	}
	return Preferences{UserID: strings.TrimSpace(userID)}, nil
}

func (s *memoryStore) UpsertPreferences(_ context.Context, p Preferences) (Preferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.UpdatedAt = time.Now()
	s.prefs[p.UserID] = p
	return p, nil
}

var (
	_ Store = (*postgresStore)(nil)
	_ Store = (*memoryStore)(nil)
)
