package jobs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

// ErrJobNotClaimable is returned by Claim when the job exists but is not
// eligible to run: it is already active with a fresh heartbeat, or it is
// in a terminal state.
var ErrJobNotClaimable = errors.New("jobs: job not claimable")

// Store is the persistence contract for background jobs. PostgresStore is
// the production implementation; MemoryStore is the in-memory fallback for
// unit tests (no database or Redis required).
type Store interface {
	// Enqueue creates a job. When params.UniqueKey is set and a
	// non-terminal job with the same key exists, the existing job is
	// returned (dedup) instead of inserting a duplicate.
	Enqueue(ctx context.Context, params EnqueueParams) (Job, error)
	// Get returns a single job or apperr.ErrNotFound.
	Get(ctx context.Context, id string) (Job, error)
	// ListByDocument returns jobs for one document, newest first.
	ListByDocument(ctx context.Context, documentID string) ([]Job, error)
	// UpdateStatus sets the status (and optional last error). Completing
	// or failing stamps completed_at.
	UpdateStatus(ctx context.Context, id string, status JobStatus, lastError string) (Job, error)
	// UpdateProgress sets the 0-100 progress and current step label.
	UpdateProgress(ctx context.Context, id string, progress int, step ProgressStep) (Job, error)
	// Claim transitions a job to active so a worker owns it. A queued (or
	// legacy pending) job is always claimable; an active job is claimable
	// only when its heartbeat is stale (updated_at older than
	// timeout_seconds), i.e. the previous worker timed out. Successful
	// claims increment attempts and stamp started_at.
	Claim(ctx context.Context, id string, now time.Time) (Job, error)
}

// jobColumns is the column list shared by Postgres scans.
const jobColumns = `id, type, document_id, status, progress, current_step, payload, result, last_error, attempts, max_retries, timeout_seconds, unique_key, created_at, updated_at, started_at, completed_at`

// scanJob scans a jobs row in jobColumns order.
func scanJob(scanner interface {
	Scan(dest ...any) error
}) (Job, error) {
	var j Job
	var documentID sql.NullString
	var currentStep sql.NullString
	var payload []byte
	var result []byte
	var lastError sql.NullString
	var uniqueKey sql.NullString
	var startedAt sql.NullTime
	var completedAt sql.NullTime
	if err := scanner.Scan(
		&j.ID,
		(*string)(&j.Type),
		&documentID,
		(*string)(&j.Status),
		&j.Progress,
		&currentStep,
		&payload,
		&result,
		&lastError,
		&j.Attempts,
		&j.MaxRetries,
		&j.TimeoutSeconds,
		&uniqueKey,
		&j.CreatedAt,
		&j.UpdatedAt,
		&startedAt,
		&completedAt,
	); err != nil {
		return Job{}, err
	}
	if documentID.Valid {
		v := documentID.String
		j.DocumentID = &v
	}
	if currentStep.Valid {
		j.CurrentStep = currentStep.String
	}
	j.Payload = cloneRaw(payload)
	if len(result) > 0 {
		j.Result = cloneRaw(result)
	}
	if lastError.Valid {
		j.LastError = lastError.String
	}
	if uniqueKey.Valid {
		v := uniqueKey.String
		j.UniqueKey = &v
	}
	if startedAt.Valid {
		v := startedAt.Time
		j.StartedAt = &v
	}
	if completedAt.Valid {
		v := completedAt.Time
		j.CompletedAt = &v
	}
	return j, nil
}

func cloneRaw(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage(`{}`)
	}
	out := make([]byte, len(b))
	copy(out, b)
	return json.RawMessage(out)
}

func strptr(s string) *string { return &s }

// newJobID generates a random UUIDv4 hex string (8-4-4-4-12) without
// adding a third-party dependency.
func newJobID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("jobs: rand: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ---- Postgres implementation ----

// PostgresStore persists jobs in PostgreSQL (migration 008).
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore creates a Postgres-backed job store.
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

// Enqueue inserts a job, deduplicating on unique_key: when a non-terminal
// job already holds the key, it is returned unchanged.
func (s *PostgresStore) Enqueue(ctx context.Context, params EnqueueParams) (Job, error) {
	if !ValidJobType(params.Type) {
		return Job{}, fmt.Errorf("jobs: unknown job type %q", params.Type)
	}
	params = params.WithDefaults()

	if params.UniqueKey != nil && *params.UniqueKey != "" {
		const dedup = `SELECT ` + jobColumns + ` FROM jobs WHERE unique_key = $1 AND status NOT IN ('completed', 'failed') ORDER BY created_at DESC LIMIT 1`
		if j, err := scanJob(s.db.QueryRowContext(ctx, dedup, *params.UniqueKey)); err == nil {
			return j, nil
		} else if err != sql.ErrNoRows {
			return Job{}, fmt.Errorf("jobs: dedup lookup: %w", err)
		}
	}

	var documentID any
	if params.DocumentID != nil {
		documentID = *params.DocumentID
	}
	var uniqueKey any
	if params.UniqueKey != nil && *params.UniqueKey != "" {
		uniqueKey = *params.UniqueKey
	}

	const insert = `INSERT INTO jobs (id, type, document_id, status, progress, current_step, payload, max_retries, timeout_seconds, unique_key)
VALUES ($1, $2, $3, $4, 0, '', $5, $6, $7, $8)
ON CONFLICT (unique_key) DO NOTHING
RETURNING ` + jobColumns
	j, err := scanJob(s.db.QueryRowContext(ctx, insert,
		newJobID(), string(params.Type), documentID, string(StatusQueued),
		string(params.Payload), params.MaxRetries, params.TimeoutSeconds, uniqueKey))
	if err == nil {
		return j, nil
	}
	if err != sql.ErrNoRows {
		return Job{}, fmt.Errorf("jobs: enqueue: %w", err)
	}
	// Lost a race on unique_key: return the winner (even if terminal, the
	// row now exists and is the canonical record for the key).
	const winner = `SELECT ` + jobColumns + ` FROM jobs WHERE unique_key = $1 ORDER BY created_at DESC LIMIT 1`
	w, werr := scanJob(s.db.QueryRowContext(ctx, winner, *params.UniqueKey))
	if werr != nil {
		return Job{}, fmt.Errorf("jobs: enqueue conflict lookup: %w", werr)
	}
	return w, nil
}

// Get returns a single job by ID.
func (s *PostgresStore) Get(ctx context.Context, id string) (Job, error) {
	j, err := scanJob(s.db.QueryRowContext(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE id = $1`, id))
	if err == sql.ErrNoRows {
		return Job{}, apperr.ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("jobs: get: %w", err)
	}
	return j, nil
}

// ListByDocument returns jobs for one document, newest first.
func (s *PostgresStore) ListByDocument(ctx context.Context, documentID string) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE document_id = $1 ORDER BY created_at DESC`, documentID)
	if err != nil {
		return nil, fmt.Errorf("jobs: list by document: %w", err)
	}
	defer rows.Close()
	out := make([]Job, 0)
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("jobs: scan: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jobs: rows: %w", err)
	}
	return out, nil
}

// UpdateStatus sets the job status; completed/failed also stamps completed_at.
func (s *PostgresStore) UpdateStatus(ctx context.Context, id string, status JobStatus, lastError string) (Job, error) {
	if !ValidStatus(status) {
		return Job{}, fmt.Errorf("jobs: unknown status %q", status)
	}
	var completedAt any
	if IsTerminal(status) {
		completedAt = time.Now().UTC()
	}
	var j Job
	if IsTerminal(status) {
		j, err := scanJob(s.db.QueryRowContext(ctx,
			`UPDATE jobs SET status = $1, last_error = $2, completed_at = $3 WHERE id = $4 RETURNING `+jobColumns,
			string(status), lastError, completedAt, id))
		if err == sql.ErrNoRows {
			return Job{}, apperr.ErrNotFound
		}
		if err != nil {
			return Job{}, fmt.Errorf("jobs: update status: %w", err)
		}
		return j, nil
	}
	j, err := scanJob(s.db.QueryRowContext(ctx,
		`UPDATE jobs SET status = $1, last_error = $2 WHERE id = $3 RETURNING `+jobColumns,
		string(status), lastError, id))
	if err == sql.ErrNoRows {
		return Job{}, apperr.ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("jobs: update status: %w", err)
	}
	return j, nil
}

// UpdateProgress sets progress (clamped to 0-100) and the current step.
func (s *PostgresStore) UpdateProgress(ctx context.Context, id string, progress int, step ProgressStep) (Job, error) {
	if !ValidStep(step) {
		return Job{}, fmt.Errorf("jobs: unknown step %q", step)
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	j, err := scanJob(s.db.QueryRowContext(ctx,
		`UPDATE jobs SET progress = $1, current_step = $2 WHERE id = $3 RETURNING `+jobColumns,
		progress, string(step), id))
	if err == sql.ErrNoRows {
		return Job{}, apperr.ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("jobs: update progress: %w", err)
	}
	return j, nil
}

// Claim marks a job active when it is queued/pending, or when an active
// job's heartbeat is stale (updated_at older than its timeout_seconds),
// which means the previous worker timed out.
func (s *PostgresStore) Claim(ctx context.Context, id string, now time.Time) (Job, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	j, err := scanJob(s.db.QueryRowContext(ctx,
		`UPDATE jobs SET status = 'active', attempts = attempts + 1,
			started_at = COALESCE(started_at, $2), updated_at = $2
		WHERE id = $1 AND (
			status IN ('queued', 'pending')
			OR (status = 'active' AND updated_at < $2 - make_interval(secs => timeout_seconds))
		) RETURNING `+jobColumns, id, now.UTC()))
	if err == nil {
		return j, nil
	}
	if err != sql.ErrNoRows {
		return Job{}, fmt.Errorf("jobs: claim: %w", err)
	}
	if _, gerr := s.Get(ctx, id); gerr != nil {
		return Job{}, gerr
	}
	return Job{}, ErrJobNotClaimable
}

// ---- In-memory implementation (tests, no database required) ----

// MemoryStore is an in-memory Store with identical dedup and claim
// semantics. Safe for concurrent use.
type MemoryStore struct {
	mu    sync.Mutex
	jobs  map[string]Job
	byKey map[string]string
	order []string
	nowFn func() time.Time
}

// NewMemoryStore creates an empty in-memory job store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		jobs:  make(map[string]Job),
		byKey: make(map[string]string),
		nowFn: func() time.Time { return time.Now().UTC() },
	}
}

// SetNowFunc overrides the clock (tests).
func (s *MemoryStore) SetNowFunc(fn func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nowFn = fn
}

func (s *MemoryStore) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn().UTC()
	}
	return time.Now().UTC()
}

// Enqueue creates a job, returning the existing non-terminal job on
// unique_key collisions.
func (s *MemoryStore) Enqueue(_ context.Context, params EnqueueParams) (Job, error) {
	if !ValidJobType(params.Type) {
		return Job{}, fmt.Errorf("jobs: unknown job type %q", params.Type)
	}
	params = params.WithDefaults()

	s.mu.Lock()
	defer s.mu.Unlock()

	if params.UniqueKey != nil && *params.UniqueKey != "" {
		if id, ok := s.byKey[*params.UniqueKey]; ok {
			if existing, ok := s.jobs[id]; ok && !IsTerminal(existing.Status) {
				return existing, nil
			}
		}
	}
	now := s.now()
	j := Job{
		ID:             newJobID(),
		Type:           params.Type,
		DocumentID:     params.DocumentID,
		Status:         StatusQueued,
		Payload:        cloneRaw(params.Payload),
		MaxRetries:     params.MaxRetries,
		TimeoutSeconds: params.TimeoutSeconds,
		UniqueKey:      params.UniqueKey,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	s.jobs[j.ID] = j
	s.order = append(s.order, j.ID)
	if j.UniqueKey != nil && *j.UniqueKey != "" {
		s.byKey[*j.UniqueKey] = j.ID
	}
	return j, nil
}

// Get returns a single job by ID.
func (s *MemoryStore) Get(_ context.Context, id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, apperr.ErrNotFound
	}
	return j, nil
}

// ListByDocument returns jobs for one document, newest first.
func (s *MemoryStore) ListByDocument(_ context.Context, documentID string) ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Job, 0)
	for i := len(s.order) - 1; i >= 0; i-- {
		j := s.jobs[s.order[i]]
		if j.DocumentID != nil && *j.DocumentID == documentID {
			out = append(out, j)
		}
	}
	return out, nil
}

// UpdateStatus sets the job status; terminal states stamp completed_at.
func (s *MemoryStore) UpdateStatus(_ context.Context, id string, status JobStatus, lastError string) (Job, error) {
	if !ValidStatus(status) {
		return Job{}, fmt.Errorf("jobs: unknown status %q", status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, apperr.ErrNotFound
	}
	j.Status = status
	j.LastError = lastError
	j.UpdatedAt = s.now()
	if IsTerminal(status) {
		now := s.now()
		j.CompletedAt = &now
		if j.Progress < 100 && status == StatusCompleted {
			j.Progress = 100
		}
	}
	s.jobs[id] = j
	return j, nil
}

// UpdateProgress sets progress (clamped to 0-100) and the current step.
func (s *MemoryStore) UpdateProgress(_ context.Context, id string, progress int, step ProgressStep) (Job, error) {
	if !ValidStep(step) {
		return Job{}, fmt.Errorf("jobs: unknown step %q", step)
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, apperr.ErrNotFound
	}
	j.Progress = progress
	j.CurrentStep = string(step)
	j.UpdatedAt = s.now()
	s.jobs[id] = j
	return j, nil
}

// Claim transitions a queued job to active, or reclaims a stale active
// job whose heartbeat (updated_at) is older than its timeout_seconds.
func (s *MemoryStore) Claim(_ context.Context, id string, now time.Time) (Job, error) {
	if now.IsZero() {
		now = s.now()
	}
	now = now.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, apperr.ErrNotFound
	}
	claimable := j.Status.Normalized() == StatusQueued
	if j.Status == StatusActive {
		timeout := time.Duration(j.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = DefaultTimeoutSeconds * time.Second
		}
		if now.Sub(j.UpdatedAt) > timeout {
			claimable = true
		}
	}
	if !claimable {
		return Job{}, ErrJobNotClaimable
	}
	j.Status = StatusActive
	j.Attempts++
	j.UpdatedAt = now
	if j.StartedAt == nil {
		started := now
		j.StartedAt = &started
	}
	s.jobs[id] = j
	return j, nil
}

// Compile-time checks.
var (
	_ Store = (*PostgresStore)(nil)
	_ Store = (*MemoryStore)(nil)
)
