package jobs

import (
	"context"
	"fmt"
)

// AsynqClient is the minimal task-publish contract the bridge needs. It
// deliberately mirrors (but does not import) hibiken/asynq so this package
// compiles and unit-tests without Redis or the Asynq module:
//
//	realClient := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
//	bridge := jobs.NewAsynqEnqueuer(store, asynqAdapter{client: realClient})
//
// where asynqAdapter adapts *asynq.Client.Enqueue to this interface in the
// worker app (which owns the Asynq dependency, not this package).
type AsynqClient interface {
	EnqueueTask(ctx context.Context, taskType string, payload []byte, opts AsynqTaskOptions) (taskID string, err error)
}

// AsynqTaskOptions carries the publish options the bridge forwards.
type AsynqTaskOptions struct {
	// Queue is the Asynq queue name (default AsynqQueue).
	Queue string
	// TaskID sets Asynq TaskID for at-most-once semantics; empty = auto.
	TaskID string
	// UniqueKey maps to the Store dedup key for observability.
	UniqueKey string
	// MaxRetry mirrors Asynq's per-task retry count.
	MaxRetry int
	// TimeoutSeconds mirrors Asynq's per-task timeout.
	TimeoutSeconds int
}

// AsynqEnqueuer persists jobs in the Store and publishes the Asynq task
// envelope via an optional client. A nil client is valid: jobs are still
// persisted (and claimable by any worker), so the package works with no
// Redis running.
type AsynqEnqueuer struct {
	store  Store
	client AsynqClient
	queue  string
}

// NewAsynqEnqueuer creates the bridge. client may be nil (persist-only
// mode); queue defaults to AsynqQueue when empty.
func NewAsynqEnqueuer(store Store, client AsynqClient, queue string) *AsynqEnqueuer {
	if queue == "" {
		queue = AsynqQueue
	}
	return &AsynqEnqueuer{store: store, client: client, queue: queue}
}

// Enqueue persists the job (with unique_key dedup) and, when a client is
// configured, publishes the task envelope. The publish uses a deterministic
// TaskID derived from the unique key (or job ID), so redeliveries of a
// dedup hit collapse into the same Asynq task when the adapter sets the
// Asynq Unique option.
func (q *AsynqEnqueuer) Enqueue(ctx context.Context, params EnqueueParams) (Job, error) {
	job, err := q.store.Enqueue(ctx, params)
	if err != nil {
		return Job{}, err
	}
	if q.client == nil {
		return job, nil
	}
	taskType, payload, err := BuildTaskPayload(job)
	if err != nil {
		return job, err
	}
	opts := AsynqTaskOptions{
		Queue:          q.queue,
		MaxRetry:       job.MaxRetries,
		TimeoutSeconds: job.TimeoutSeconds,
	}
	if job.UniqueKey != nil {
		opts.UniqueKey = *job.UniqueKey
		opts.TaskID = taskType + ":" + *job.UniqueKey
	} else {
		opts.TaskID = taskType + ":" + job.ID
	}
	if _, err := q.client.EnqueueTask(ctx, taskType, payload, opts); err != nil {
		return job, fmt.Errorf("jobs: asynq publish: %w", err)
	}
	return job, nil
}

var _ Enqueuer = (*AsynqEnqueuer)(nil)
