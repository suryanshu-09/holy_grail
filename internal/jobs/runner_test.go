package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

// fakeProcessor records calls and replays scripted errors per job type.
type fakeProcessor struct {
	calls    []JobType
	progress []ProgressStep
	failures map[JobType][]error
}

func (f *fakeProcessor) record(job Job, step ProgressStep, report ProgressFunc) error {
	f.calls = append(f.calls, job.Type)
	if report != nil {
		_ = report(context.Background(), 10, step)
		f.progress = append(f.progress, step)
	}
	if errs := f.failures[job.Type]; len(errs) > 0 {
		f.failures[job.Type] = errs[1:]
		return errs[0]
	}
	return nil
}

func (f *fakeProcessor) ProcessDocument(ctx context.Context, job Job, report ProgressFunc) error {
	return f.record(job, StepExtracting, report)
}

func (f *fakeProcessor) ExtractQuestions(ctx context.Context, job Job, report ProgressFunc) error {
	return f.record(job, StepQuestions, report)
}

func (f *fakeProcessor) ClassifyQuestions(ctx context.Context, job Job, report ProgressFunc) error {
	return f.record(job, StepClassifying, report)
}

func (f *fakeProcessor) GenerateEmbeddings(ctx context.Context, job Job, report ProgressFunc) error {
	return f.record(job, StepEmbeddings, report)
}

func testRunner(store Store, proc Processor) *Runner {
	return NewRunner(store, proc, slog.Default()).
		WithRetryBackoff(time.Millisecond).
		WithPollInterval(time.Millisecond)
}

func enqueueDocJob(t *testing.T, store Store, typ JobType) Job {
	t.Helper()
	doc := "doc-123"
	job, err := store.Enqueue(context.Background(), EnqueueParams{Type: typ, DocumentID: &doc})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return job
}

func TestRunnerDispatchesAllJobTypes(t *testing.T) {
	ctx := context.Background()
	for _, typ := range []JobType{TypeProcessDocument, TypeExtractQuestions, TypeClassifyQuestions, TypeGenerateEmbeddings} {
		store := NewMemoryStore()
		proc := &fakeProcessor{}
		r := testRunner(store, proc)
		job := enqueueDocJob(t, store, typ)
		if err := r.Run(ctx, job); err != nil {
			t.Fatalf("%s: Run: %v", typ, err)
		}
		if len(proc.calls) != 1 || proc.calls[0] != typ {
			t.Fatalf("%s: calls = %v, want [%s]", typ, proc.calls, typ)
		}
		got, _ := store.Get(ctx, job.ID)
		if got.Status != StatusCompleted {
			t.Fatalf("%s: status = %q, want completed", typ, got.Status)
		}
		if got.Progress != 100 {
			t.Fatalf("%s: progress = %d, want 100", typ, got.Progress)
		}
	}
}

func TestRunnerRetriesThenSucceeds(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	proc := &fakeProcessor{failures: map[JobType][]error{
		TypeClassifyQuestions: {errors.New("boom"), errors.New("boom again")},
	}}
	r := testRunner(store, proc)
	job := enqueueDocJob(t, store, TypeClassifyQuestions)
	if err := r.Run(ctx, job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(proc.calls) != 3 {
		t.Fatalf("calls = %d, want 3 (2 failures + success)", len(proc.calls))
	}
	got, _ := store.Get(ctx, job.ID)
	if got.Status != StatusCompleted {
		t.Fatalf("status = %q, want completed", got.Status)
	}
}

func TestRunnerExhaustsRetriesAndFails(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	proc := &fakeProcessor{failures: map[JobType][]error{
		TypeGenerateEmbeddings: {errors.New("nope"), errors.New("nope"), errors.New("nope"), errors.New("nope"), errors.New("nope"), errors.New("nope")},
	}}
	r := testRunner(store, proc)
	job := enqueueDocJob(t, store, TypeGenerateEmbeddings)
	if err := r.Run(ctx, job); err == nil {
		t.Fatal("Run: want error, got nil")
	}
	if len(proc.calls) != DefaultMaxRetries {
		t.Fatalf("calls = %d, want %d", len(proc.calls), DefaultMaxRetries)
	}
	got, _ := store.Get(ctx, job.ID)
	if got.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.LastError == "" {
		t.Fatal("LastError empty after failure")
	}
}

func TestRunnerTimeoutCancelsProcessor(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	blocker := &blockingProcessor{release: make(chan struct{})}
	r := testRunner(store, blocker)
	params := EnqueueParams{Type: TypeProcessDocument, TimeoutSeconds: 1}
	params = params.WithDefaults()
	params.TimeoutSeconds = 1
	job, err := store.Enqueue(ctx, params)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	start := time.Now()
	if err := r.Run(ctx, job); err == nil {
		t.Fatal("Run: want timeout error, got nil")
	}
	// One attempt with a 1s timeout plus fast backoff: should finish well
	// under 5 * timeout (5 default executions * 1s each at most).
	if elapsed := time.Since(start); elapsed > 6*time.Second {
		t.Fatalf("Run took %v, timeout not enforced", elapsed)
	}
	close(blocker.release)
	got, _ := store.Get(ctx, job.ID)
	if got.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
}

// blockingProcessor blocks until release is closed or ctx is done.
type blockingProcessor struct {
	release chan struct{}
}

func (b *blockingProcessor) wait(ctx context.Context, job Job, report ProgressFunc) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.release:
		return nil
	}
}

func (b *blockingProcessor) ProcessDocument(ctx context.Context, job Job, report ProgressFunc) error {
	return b.wait(ctx, job, report)
}

func (b *blockingProcessor) ExtractQuestions(ctx context.Context, job Job, report ProgressFunc) error {
	return b.wait(ctx, job, report)
}

func (b *blockingProcessor) ClassifyQuestions(ctx context.Context, job Job, report ProgressFunc) error {
	return b.wait(ctx, job, report)
}

func (b *blockingProcessor) GenerateEmbeddings(ctx context.Context, job Job, report ProgressFunc) error {
	return b.wait(ctx, job, report)
}

func TestRunnerSkipsDuplicateClaim(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	proc := &fakeProcessor{}
	r := testRunner(store, proc)
	job := enqueueDocJob(t, store, TypeProcessDocument)
	if _, err := store.Claim(ctx, job.ID, time.Now().UTC()); err != nil {
		t.Fatalf("claim: %v", err)
	}
	// Second Run cannot claim the fresh active job: skipped without execution.
	if err := r.Run(ctx, job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(proc.calls) != 0 {
		t.Fatalf("calls = %d, want 0 (duplicate skipped)", len(proc.calls))
	}
}

func TestRunnerRunByIDSkipsTerminal(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	proc := &fakeProcessor{}
	r := testRunner(store, proc)
	job := enqueueDocJob(t, store, TypeProcessDocument)
	if _, err := store.UpdateStatus(ctx, job.ID, StatusCompleted, ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := r.RunByID(ctx, job.ID); err != nil {
		t.Fatalf("RunByID: %v", err)
	}
	if len(proc.calls) != 0 {
		t.Fatalf("calls = %d, want 0 (terminal skipped)", len(proc.calls))
	}
}

func TestClaimNextOldestFirst(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	first := enqueueDocJob(t, store, TypeProcessDocument)
	second := enqueueDocJob(t, store, TypeExtractQuestions)
	claimed, err := ClaimNext(ctx, store, time.Now().UTC())
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed.ID != first.ID {
		t.Fatalf("claimed = %s, want oldest %s", claimed.ID, first.ID)
	}
	claimed2, err := ClaimNext(ctx, store, time.Now().UTC())
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed2.ID != second.ID {
		t.Fatalf("claimed = %s, want %s", claimed2.ID, second.ID)
	}
	if _, err := ClaimNext(ctx, store, time.Now().UTC()); !errors.Is(err, ErrNoQueuedJobs) {
		t.Fatalf("ClaimNext empty: err = %v, want ErrNoQueuedJobs", err)
	}
}

func TestClaimNextReclaimsStaleActive(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	job := enqueueDocJob(t, store, TypeProcessDocument)
	stale := time.Now().UTC().Add(-time.Duration(job.TimeoutSeconds+10) * time.Second)
	store.SetNowFunc(func() time.Time { return stale })
	if _, err := store.Claim(ctx, job.ID, stale); err != nil {
		t.Fatalf("claim: %v", err)
	}
	store.SetNowFunc(func() time.Time { return time.Now().UTC() })
	claimed, err := ClaimNext(ctx, store, time.Now().UTC())
	if err != nil {
		t.Fatalf("ClaimNext stale: %v", err)
	}
	if claimed.ID != job.ID {
		t.Fatalf("claimed = %s, want stale %s", claimed.ID, job.ID)
	}
}

func TestBackoffForAttempt(t *testing.T) {
	base := 100 * time.Millisecond
	if got := BackoffForAttempt(base, 0); got != base {
		t.Fatalf("attempt 0 = %v, want %v", got, base)
	}
	if got := BackoffForAttempt(base, 2); got != 400*time.Millisecond {
		t.Fatalf("attempt 2 = %v, want 400ms", got)
	}
	if got := BackoffForAttempt(base, 30); got != maxRetryBackoff {
		t.Fatalf("attempt 30 = %v, want capped %v", got, maxRetryBackoff)
	}
	if got := BackoffForAttempt(0, 0); got != defaultRetryBackoff {
		t.Fatalf("zero base = %v, want default %v", got, defaultRetryBackoff)
	}
}
