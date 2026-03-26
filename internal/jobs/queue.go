package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Queue wraps storage.JobStore with queue semantics.
type Queue struct {
	store storage.JobStore
}

// NewQueue creates a job queue backed by the given job store.
func NewQueue(store storage.JobStore) *Queue {
	return &Queue{store: store}
}

func (q *Queue) Enqueue(ctx context.Context, job *storage.Job) error {
	if job.Status == "" {
		job.Status = storage.JobPending
	}
	return q.store.Create(ctx, job)
}

func (q *Queue) AcquireNext(ctx context.Context) (*storage.Job, error) {
	return q.store.AcquireNext(ctx)
}

func (q *Queue) Complete(ctx context.Context, id string, resultID string) error {
	return q.store.Complete(ctx, id, resultID)
}

func (q *Queue) Fail(ctx context.Context, id string, errMsg string) error {
	return q.store.Fail(ctx, id, errMsg)
}

func (q *Queue) Retry(ctx context.Context, id string) error {
	// Check max retries before delegating.
	job, err := q.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.RetryCount >= job.MaxRetries {
		return fmt.Errorf("job %s: max retries (%d) exceeded", id, job.MaxRetries)
	}
	return q.store.Retry(ctx, id)
}

func (q *Queue) Get(ctx context.Context, id string) (*storage.Job, error) {
	return q.store.Get(ctx, id)
}

func (q *Queue) List(ctx context.Context, filter storage.JobFilter) ([]*storage.Job, int, error) {
	return q.store.List(ctx, filter)
}

func (q *Queue) RecoverStale(ctx context.Context, timeoutSeconds int64) (int, error) {
	return q.store.RecoverStale(ctx, timeoutSeconds)
}

// EnqueueIngestJob creates and enqueues an ingest job without requiring callers
// to import internal/storage directly.
func (q *Queue) EnqueueIngestJob(ctx context.Context, jobType, payload, pipeline, source string, maxRetries int) error {
	now := time.Now().Truncate(time.Second)
	job := &storage.Job{
		ID:         uuid.New().String(),
		Type:       jobType,
		Status:     storage.JobPending,
		Payload:    payload,
		Pipeline:   pipeline,
		Source:     source,
		MaxRetries: maxRetries,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	return q.Enqueue(ctx, job)
}
