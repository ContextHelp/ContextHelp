package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TaskHandler runs a task job: a job whose type names a registered handler
// instead of a pipeline (for example an embedding migration). The returned
// result is stored as the job's result_id.
//
// Task jobs can run far longer than an ingest, so the handler's context is
// cancelled when the pool stops (cause ErrPoolStopped) or when an operator
// cancels the job (cause ErrJobCancelled). A handler must therefore be
// resumable: on shutdown the job stays running and the next start's crash
// recovery requeues it.
//
// Settlement: nil error completes the job; a pipeline.Permanent error fails
// it without retry; any other error retries it up to its max_retries.
type TaskHandler func(ctx context.Context, job *storage.Job) (result string, err error)

// jobCancelled is the status JobStore.Cancel writes.
const jobCancelled storage.JobStatus = "cancelled"

var (
	// ErrJobCancelled is the context cause a task handler sees when the job
	// row was cancelled while it ran.
	ErrJobCancelled = errors.New("jobs: job cancelled")
	// ErrPoolStopped is the context cause a task handler sees when the
	// worker pool stops.
	ErrPoolStopped = errors.New("jobs: worker pool stopped")
)

// Handle registers h for jobs of jobType. Register before Start.
func (p *WorkerPool) Handle(jobType string, h TaskHandler) {
	if p.handlers == nil {
		p.handlers = map[string]TaskHandler{}
	}
	p.handlers[jobType] = h
}

// EnqueueTask enqueues a task job of jobType. Task jobs carry no pipeline,
// so the pipeline validator does not apply; the pool that acquires the job
// must have a handler for jobType.
func (q *Queue) EnqueueTask(ctx context.Context, jobType, payload string, maxRetries int) (*storage.Job, error) {
	if jobType == "" {
		return nil, errors.New("enqueue task: job type is required")
	}
	now := time.Now().Truncate(time.Second)
	job := &storage.Job{
		ID:         uuid.New().String(),
		Type:       jobType,
		Status:     storage.JobPending,
		Payload:    payload,
		MaxRetries: maxRetries,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := q.store.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("enqueue task %s: %w", jobType, err)
	}
	return job, nil
}

// runTask runs one task job. acquireCtx is the pool's acquire context: it
// ends at shutdown, which interrupts the handler.
func (p *WorkerPool) runTask(acquireCtx context.Context, job *storage.Job, h TaskHandler) {
	// Stale recovery requeues a job by its started_at, not by liveness, so
	// a long task this pool is still running can be acquired again. The
	// second acquisition only refreshed started_at; the running handler
	// settles the job.
	if _, running := p.inflight.LoadOrStore(job.ID, struct{}{}); running {
		slog.Debug("jobs: task already running in this pool; leaving it to the running handler", "job", job.ID)
		return
	}
	defer p.inflight.Delete(job.ID)

	ctx, cancel := context.WithCancelCause(context.WithoutCancel(acquireCtx))
	defer cancel(nil)
	stopWatch := p.watchTask(acquireCtx, ctx, cancel, job.ID)
	defer stopWatch()

	start := time.Now()
	result, err := h(ctx, job)

	settle := context.WithoutCancel(acquireCtx)
	switch cause := context.Cause(ctx); {
	case err == nil:
		if cerr := p.queue.Complete(settle, job.ID, result); cerr != nil {
			slog.Warn("jobs: mark task completed", "job", job.ID, "err", cerr)
		}
		p.emitTaskCompleted(settle, job.ID, time.Since(start).Milliseconds())
	case errors.Is(cause, ErrJobCancelled):
		slog.Info("jobs: task stopped: job cancelled", "job", job.ID, "type", job.Type)
	case errors.Is(cause, ErrPoolStopped):
		// Left running on purpose: the next start's crash recovery
		// requeues it without spending a retry.
		slog.Info("jobs: task interrupted by shutdown; resumes on next start", "job", job.ID, "type", job.Type)
	case pipeline.IsPermanent(err):
		if ferr := p.queue.Fail(settle, job.ID, err.Error()); ferr != nil {
			slog.Warn("jobs: mark task failed", "job", job.ID, "err", ferr)
		}
		p.emitFailed(settle, job.ID, err.Error())
	default:
		p.retryOrFail(settle, job.ID, err.Error())
	}
}

// watchTask cancels ctx with ErrPoolStopped when the pool stops and with
// ErrJobCancelled when the job row is cancelled. The returned func stops
// the watcher.
func (p *WorkerPool) watchTask(acquireCtx, ctx context.Context, cancel context.CancelCauseFunc, jobID string) func() {
	interval := p.pollInterval
	if interval <= 0 {
		interval = time.Second
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-acquireCtx.Done():
				cancel(ErrPoolStopped)
				return
			case <-ticker.C:
				job, err := p.queue.Get(ctx, jobID)
				if err == nil && job.Status == jobCancelled {
					cancel(ErrJobCancelled)
					return
				}
			}
		}
	}()
	return func() { close(done) }
}

// emitTaskCompleted publishes job.completed for a task job, which stores
// no object.
func (p *WorkerPool) emitTaskCompleted(ctx context.Context, jobID string, durationMs int64) {
	if p.bus == nil {
		return
	}
	ev, err := events.NewEvent(SourceWorkerPool, string(TopicJobCompleted), events.JobCompletedPayload{
		JobID:      jobID,
		DurationMs: durationMs,
	})
	if err != nil {
		return
	}
	if pubErr := p.bus.Publish(ctx, ev); pubErr != nil {
		slog.Warn("jobs: failed to publish job.completed event", "job", jobID, "err", pubErr)
	}
}
