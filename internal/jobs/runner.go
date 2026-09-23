// Package jobs implements Phase 19 background-job state: job types,
// statuses, progress steps, persistent storage, queue adapters, and the
// worker-side execution runner (timeout, progress, retry, logging).
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ProgressFunc reports pipeline progress for a running job. The Runner wires
// it to Store.UpdateProgress so domain code (e.g. the ExtractionService
// adapter in cmd/worker) stays decoupled from job persistence.
type ProgressFunc func(ctx context.Context, progress int, step ProgressStep) error

// Processor executes one job's domain work. It is implemented by cmd/worker
// with the ExtractionService pipeline steps; defining the interface here
// avoids an import cycle (this package cannot import extraction, while the
// worker imports both).
type Processor interface {
	// ProcessDocument runs the full document pipeline
	// (extract -> questions -> classify -> embeddings).
	ProcessDocument(ctx context.Context, job Job, report ProgressFunc) error
	// ExtractQuestions runs the page/question extraction stage only.
	ExtractQuestions(ctx context.Context, job Job, report ProgressFunc) error
	// ClassifyQuestions runs the topic classification stage only.
	ClassifyQuestions(ctx context.Context, job Job, report ProgressFunc) error
	// GenerateEmbeddings runs the embedding stage only.
	GenerateEmbeddings(ctx context.Context, job Job, report ProgressFunc) error
}

// ErrNoQueuedJobs is returned by ClaimNext when no job is eligible to run.
var ErrNoQueuedJobs = errors.New("jobs: no queued jobs")

const (
	// defaultRetryBackoff is the base delay between inline retry attempts;
	// the actual delay grows exponentially per attempt (see BackoffForAttempt).
	defaultRetryBackoff = time.Second
	// maxRetryBackoff caps the exponential growth.
	maxRetryBackoff = 60 * time.Second
	// defaultPollInterval is how often the DB poll loop looks for work.
	defaultPollInterval = 2 * time.Second
	// pollCandidateLimit bounds how many candidate rows one poll iteration scans.
	pollCandidateLimit = 25
)

// BackoffForAttempt returns the retry delay before attempt number n
// (0-based): base * 2^n, capped at maxRetryBackoff. A non-positive base
// falls back to defaultRetryBackoff.
func BackoffForAttempt(base time.Duration, n int) time.Duration {
	if base <= 0 {
		base = defaultRetryBackoff
	}
	if n < 0 {
		n = 0
	}
	d := base
	for i := 0; i < n; i++ {
		d *= 2
		if d >= maxRetryBackoff {
			return maxRetryBackoff
		}
	}
	if d > maxRetryBackoff {
		return maxRetryBackoff
	}
	return d
}

// timeoutFor resolves the per-job execution timeout, falling back to the
// package default when the job carries no positive timeout.
func timeoutFor(job Job) time.Duration {
	if job.TimeoutSeconds > 0 {
		return time.Duration(job.TimeoutSeconds) * time.Second
	}
	return DefaultTimeoutSeconds * time.Second
}

// executionsFor bounds the inline executions for one claimed job: a fresh
// claim (Attempts == 1) may run up to MaxRetries times total. Jobs claimed
// by an older worker (higher Attempts) get fewer remaining executions.
func executionsFor(job Job) int {
	maxRetries := job.MaxRetries
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}
	remaining := maxRetries - job.Attempts + 1
	if remaining < 1 {
		remaining = 1
	}
	return remaining
}

// Runner executes claimed jobs through a Processor with timeout context,
// progress callbacks, retry with backoff, and structured logging. It is safe
// for concurrent use by multiple workers sharing one Store.
type Runner struct {
	store        Store
	processor    Processor
	logger       *slog.Logger
	retryBackoff time.Duration
	pollInterval time.Duration
}

// NewRunner creates a Runner. A nil logger falls back to slog.Default().
func NewRunner(store Store, processor Processor, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		store:        store,
		processor:    processor,
		logger:       logger,
		retryBackoff: defaultRetryBackoff,
		pollInterval: defaultPollInterval,
	}
}

// WithRetryBackoff sets the base delay between inline retry attempts.
func (r *Runner) WithRetryBackoff(d time.Duration) *Runner {
	if d > 0 {
		r.retryBackoff = d
	}
	return r
}

// WithPollInterval sets how often RunPollLoop looks for queued jobs.
func (r *Runner) WithPollInterval(d time.Duration) *Runner {
	if d > 0 {
		r.pollInterval = d
	}
	return r
}

func (r *Runner) log() *slog.Logger {
	if r.logger == nil {
		return slog.Default()
	}
	return r.logger
}

// docIDValue extracts the document id for log fields (empty when unset).
func docIDValue(job Job) string {
	if job.DocumentID == nil {
		return ""
	}
	return *job.DocumentID
}

// reporter builds the ProgressFunc bound to one job, persisting progress via
// the Store. Persistence failures are returned so processors can decide
// whether they are fatal; the worker adapter treats them as best-effort.
func (r *Runner) reporter(jobID string) ProgressFunc {
	return func(ctx context.Context, progress int, step ProgressStep) error {
		_, err := r.store.UpdateProgress(ctx, jobID, progress, step)
		return err
	}
}

// dispatch routes a job to the Processor method for its type.
func (r *Runner) dispatch(ctx context.Context, job Job, report ProgressFunc) error {
	switch job.Type {
	case TypeProcessDocument:
		return r.processor.ProcessDocument(ctx, job, report)
	case TypeExtractQuestions:
		return r.processor.ExtractQuestions(ctx, job, report)
	case TypeClassifyQuestions:
		return r.processor.ClassifyQuestions(ctx, job, report)
	case TypeGenerateEmbeddings:
		return r.processor.GenerateEmbeddings(ctx, job, report)
	default:
		return fmt.Errorf("jobs: unknown job type %q", job.Type)
	}
}

// Run claims the job and executes it. A job that is already active with a
// fresh heartbeat or in a terminal state is skipped (nil) so duplicate
// deliveries (e.g. Asynq redelivery) are harmless.
func (r *Runner) Run(ctx context.Context, job Job) error {
	claimed, err := r.store.Claim(ctx, job.ID, time.Now().UTC())
	if err != nil {
		if errors.Is(err, ErrJobNotClaimable) {
			r.log().Info("job not claimable, skipping",
				"job_id", job.ID,
				"document_id", docIDValue(job),
				"operation", string(job.Type))
			return nil
		}
		return err
	}
	return r.runClaimed(ctx, claimed)
}

// RunByID loads a job and runs it, skipping terminal jobs. It is the entry
// point for queue handlers that receive only a job id (Asynq envelopes,
// poll-loop candidates).
func (r *Runner) RunByID(ctx context.Context, jobID string) error {
	job, err := r.store.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if IsTerminal(job.Status) {
		r.log().Info("job already terminal, skipping",
			"job_id", job.ID,
			"document_id", docIDValue(job),
			"operation", string(job.Type),
			"status", string(job.Status))
		return nil
	}
	return r.Run(ctx, job)
}

// runClaimed executes an already-claimed job with timeout context, progress
// callbacks, retry with backoff, and structured logging. Success marks the
// job completed; exhausted retries mark it failed with the last error.
func (r *Runner) runClaimed(ctx context.Context, job Job) error {
	start := time.Now()
	logger := r.log().With(
		"job_id", job.ID,
		"document_id", docIDValue(job),
		"operation", string(job.Type),
	)
	timeout := timeoutFor(job)
	executions := executionsFor(job)
	logger.Info("job started", "attempt", job.Attempts, "timeout", timeout.String())

	var lastErr error
	for attempt := 0; attempt < executions; attempt++ {
		if err := ctx.Err(); err != nil {
			lastErr = err
			break
		}
		opStart := time.Now()
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		err := r.dispatch(attemptCtx, job, r.reporter(job.ID))
		duration := time.Since(opStart)
		cancel()
		if err == nil {
			r.markCompleted(ctx, logger, job)
			logger.Info("job completed", "duration", time.Since(start).String())
			return nil
		}
		lastErr = err
		logger.Warn("job attempt failed",
			"attempt", attempt+1,
			"error", err.Error(),
			"duration", duration.String())
		if attempt < executions-1 {
			backoff := BackoffForAttempt(r.retryBackoff, attempt)
			logger.Info("job retrying", "attempt", attempt+2, "backoff", backoff.String())
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				lastErr = ctx.Err()
				attempt = executions // break outer loop
			case <-timer.C:
			}
		}
	}

	msg := ""
	if lastErr != nil {
		msg = lastErr.Error()
	}
	if _, uerr := r.store.UpdateStatus(ctx, job.ID, StatusFailed, msg); uerr != nil {
		logger.Error("job failed to record failure status", "error", uerr.Error())
	}
	logger.Error("job failed", "error", msg, "duration", time.Since(start).String())
	return lastErr
}

// markCompleted records job completion and best-effort sets progress to 100
// (preserving the last reported step) so the UI progress bar finishes.
func (r *Runner) markCompleted(ctx context.Context, logger *slog.Logger, job Job) {
	if _, err := r.store.UpdateStatus(ctx, job.ID, StatusCompleted, ""); err != nil {
		logger.Error("job failed to record completion", "error", err.Error())
		return
	}
	step := StepEmbeddings
	if current, err := r.store.Get(ctx, job.ID); err == nil {
		if ValidStep(ProgressStep(current.CurrentStep)) {
			step = ProgressStep(current.CurrentStep)
		}
		if current.Progress >= 100 {
			return
		}
	}
	if _, err := r.store.UpdateProgress(ctx, job.ID, 100, step); err != nil {
		logger.Warn("job failed to record final progress", "error", err.Error())
	}
}

// ClaimNext finds the oldest queued (or timeout-stale active) job and claims
// it for the caller. It returns ErrNoQueuedJobs when no job is eligible.
// It reuses Store.Claim so claim semantics stay identical to direct claims.
func ClaimNext(ctx context.Context, store Store, now time.Time) (Job, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ids, err := queuedCandidateIDs(ctx, store)
	if err != nil {
		return Job{}, err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return Job{}, err
		}
		claimed, err := store.Claim(ctx, id, now)
		if err == nil {
			return claimed, nil
		}
		if !errors.Is(err, ErrJobNotClaimable) {
			return Job{}, err
		}
	}
	return Job{}, ErrNoQueuedJobs
}

// queuedCandidateIDs lists candidate job ids (oldest first) for ClaimNext.
// It switches on the concrete Store so the poll loop works without extending
// the Store interface.
func queuedCandidateIDs(ctx context.Context, store Store) ([]string, error) {
	switch s := store.(type) {
	case *PostgresStore:
		rows, err := s.db.QueryContext(ctx,
			`SELECT id FROM jobs
			WHERE status IN ('queued', 'pending')
			   OR (status = 'active' AND updated_at < now() - make_interval(secs => timeout_seconds))
			ORDER BY created_at ASC LIMIT $1`, pollCandidateLimit)
		if err != nil {
			return nil, fmt.Errorf("jobs: list queued: %w", err)
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, fmt.Errorf("jobs: scan queued: %w", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("jobs: rows queued: %w", err)
		}
		return ids, nil
	case *MemoryStore:
		s.mu.Lock()
		defer s.mu.Unlock()
		var ids []string
		for _, id := range s.order {
			j, ok := s.jobs[id]
			if !ok || IsTerminal(j.Status) {
				continue
			}
			ids = append(ids, id)
			if len(ids) >= pollCandidateLimit {
				break
			}
		}
		return ids, nil
	default:
		return nil, fmt.Errorf("jobs: ClaimNext unsupported for store %T", store)
	}
}

// RunPollLoop continuously claims and executes queued jobs until ctx is
// cancelled. It is the Redis-free execution path used when REDIS_ADDR is
// unset. Idle iterations sleep for the poll interval.
func (r *Runner) RunPollLoop(ctx context.Context) error {
	interval := r.pollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	r.log().Info("poll loop started", "interval", interval.String())
	for {
		if err := ctx.Err(); err != nil {
			r.log().Info("poll loop stopped")
			return nil
		}
		job, err := ClaimNext(ctx, r.store, time.Now().UTC())
		if err != nil {
			if errors.Is(err, ErrNoQueuedJobs) {
				if sleep(ctx, interval) {
					return nil
				}
				continue
			}
			r.log().Warn("poll claim failed", "error", err.Error())
			if sleep(ctx, interval) {
				return nil
			}
			continue
		}
		if err := r.runClaimed(ctx, job); err != nil {
			// Already recorded as failed with structured logs; continue
			// polling for the next job.
			continue
		}
	}
}

// sleep waits for d or until ctx is done, reporting cancellation.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-timer.C:
		return false
	}
}
