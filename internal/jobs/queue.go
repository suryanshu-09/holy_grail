package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Enqueuer persists a job and hands it to a backend for execution.
// MemoryEnqueuer is the default: it works without Redis. AsynqEnqueuer
// (asynq.go) forwards to an optional Asynq-compatible client.
type Enqueuer interface {
	Enqueue(ctx context.Context, params EnqueueParams) (Job, error)
}

// Handler processes one claimed job inline. It is used by tests and by
// deployments that run without a Redis-backed worker.
type Handler func(ctx context.Context, job Job) error

// MemoryEnqueuer stores jobs via a Store and records every dispatch in
// memory so tests can assert on queue behavior without Redis. When
// Handler is non-nil, the job is also executed inline (synchronously);
// a nil handler means dispatch-only, mirroring an external worker setup.
type MemoryEnqueuer struct {
	store   Store
	handler Handler

	mu        sync.Mutex
	delivered []Job
}

// NewMemoryEnqueuer creates an enqueuer backed by store. Pass a nil
// handler for dispatch-only mode.
func NewMemoryEnqueuer(store Store, handler Handler) *MemoryEnqueuer {
	return &MemoryEnqueuer{store: store, handler: handler}
}

// Enqueue persists the job (with unique_key dedup) and records the
// dispatch. Inline execution runs only for newly-active claims; dedup
// hits are returned without re-execution.
func (q *MemoryEnqueuer) Enqueue(ctx context.Context, params EnqueueParams) (Job, error) {
	job, err := q.store.Enqueue(ctx, params)
	if err != nil {
		return Job{}, err
	}
	q.mu.Lock()
	q.delivered = append(q.delivered, job)
	q.mu.Unlock()
	if q.handler != nil && job.Status.Normalized() == StatusQueued && job.Attempts == 0 {
		if herr := q.handler(ctx, job); herr != nil {
			return job, herr
		}
	}
	return job, nil
}

// Delivered returns a snapshot of all jobs dispatched through this
// enqueuer, in order.
func (q *MemoryEnqueuer) Delivered() []Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Job, len(q.delivered))
	copy(out, q.delivered)
	return out
}

// Reset clears the dispatch history (tests).
func (q *MemoryEnqueuer) Reset() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.delivered = nil
}

var _ Enqueuer = (*MemoryEnqueuer)(nil)

// ---- Asynq task payload builders (no Redis/Asynq dependency) ----

// AsynqQueue is the default queue name used when bridging to Asynq.
const AsynqQueue = "default"

// AsynqTaskType returns the Asynq task type name for a job type.
func AsynqTaskType(t JobType) string {
	return "holy_grail:" + string(t)
}

// JobTypeFromTaskType parses an Asynq task type name back into a JobType.
func JobTypeFromTaskType(taskType string) (JobType, error) {
	name := strings.TrimPrefix(taskType, "holy_grail:")
	t := JobType(name)
	if !ValidJobType(t) {
		return "", fmt.Errorf("jobs: unknown task type %q", taskType)
	}
	return t, nil
}

// TaskEnvelope is the JSON payload handed to the worker. It carries the
// persistent job ID so the worker can load state, report progress, and
// apply timeout/retry semantics via the Store.
type TaskEnvelope struct {
	JobID      string          `json:"job_id"`
	Type       JobType         `json:"type"`
	DocumentID *string         `json:"document_id,omitempty"`
	Payload    json.RawMessage `json:"payload"`
}

// BuildTaskPayload serializes a job into an Asynq-ready (type, payload)
// pair without importing the Asynq library, keeping this package
// compilable and testable without Redis running.
func BuildTaskPayload(job Job) (taskType string, payload []byte, err error) {
	if !ValidJobType(job.Type) {
		return "", nil, fmt.Errorf("jobs: unknown job type %q", job.Type)
	}
	env := TaskEnvelope{
		JobID:      job.ID,
		Type:       job.Type,
		DocumentID: job.DocumentID,
		Payload:    cloneRaw(job.Payload),
	}
	payload, err = json.Marshal(env)
	if err != nil {
		return "", nil, fmt.Errorf("jobs: marshal task payload: %w", err)
	}
	return AsynqTaskType(job.Type), payload, nil
}

// ParseTaskPayload decodes an Asynq (type, payload) pair back into an
// envelope, validating that the type names agree.
func ParseTaskPayload(taskType string, payload []byte) (TaskEnvelope, error) {
	var env TaskEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return TaskEnvelope{}, fmt.Errorf("jobs: unmarshal task payload: %w", err)
	}
	want, err := JobTypeFromTaskType(taskType)
	if err != nil {
		return TaskEnvelope{}, err
	}
	if env.Type != want {
		return TaskEnvelope{}, fmt.Errorf("jobs: task type %q mismatches payload type %q", taskType, env.Type)
	}
	if env.JobID == "" {
		return TaskEnvelope{}, fmt.Errorf("jobs: task payload missing job_id")
	}
	return env, nil
}
