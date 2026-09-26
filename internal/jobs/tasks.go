package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
// cancelled when the pool stops (cause ErrPoolStopped), when an operator
// cancels the job (cause ErrJobCancelled), or when the job's lease is lost
// (cause ErrLeaseLost). A handler must therefore be resumable: on shutdown
// the job stays running, unleased, and the next start's crash recovery
// requeues it.
//
// Exclusivity: several pools (dpkms processes) may share one database. The
// pool running a task job holds a lease on it, renewed every leaseTTL/3.
// Stale and crash recovery leave a leased job alone, so no other pool can
// requeue and run it. A process that dies holding a lease leaves the job
// running until the lease runs out; stale recovery then requeues it.
//
// Settlement: nil error completes the job; a pipeline.Permanent error fails
// it without retry; any other error retries it up to its max_retries.
type TaskHandler func(ctx context.Context, job *storage.Job) (result string, err error)

// jobCancelled is the status JobStore.Cancel writes.
const jobCancelled storage.JobStatus = "cancelled"

// defaultTaskLeaseTTL is how long a task job's lease lasts unrenewed. The
// running pool renews it every third of that.
const defaultTaskLeaseTTL = 30 * time.Second

var (
	// ErrJobCancelled is the context cause a task handler sees when the job
	// row was cancelled while it ran.
	ErrJobCancelled = errors.New("jobs: job cancelled")
	// ErrPoolStopped is the context cause a task handler sees when the
	// worker pool stops.
	ErrPoolStopped = errors.New("jobs: worker pool stopped")
	// ErrLeaseLost is the context cause a task handler sees when its lease
	// could not be renewed because the job was requeued (the lease ran out
	// first) and may now be claimed by another worker.
	ErrLeaseLost = errors.New("jobs: task lease lost")
)

// taskClaim is the claim a running task renews its lease under. When the
// pool re-acquires a job it is still running, the running handler takes
// over the new claim.
type taskClaim struct {
	mu    sync.Mutex
	token string
}

func (c *taskClaim) get() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

func (c *taskClaim) set(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

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
	settle := context.WithoutCancel(acquireCtx)

	// A task this pool is still running is acquired again only after its
	// lease ran out (renewals failing) and stale recovery requeued it. The
	// running handler takes over the new claim and settles the job.
	claim := &taskClaim{token: job.Claim}
	if v, running := p.inflight.LoadOrStore(job.ID, claim); running {
		if c, ok := v.(*taskClaim); ok {
			c.set(job.Claim)
		}
		if _, err := p.queue.ExtendLease(settle, job.ID, job.Claim, p.leaseTTL); err != nil {
			slog.Warn("jobs: extend task lease", "job", job.ID, "err", err)
		}
		slog.Debug("jobs: task already running in this pool; leaving it to the running handler", "job", job.ID)
		return
	}
	defer p.inflight.Delete(job.ID)

	held, err := p.queue.ExtendLease(settle, job.ID, job.Claim, p.leaseTTL)
	if err != nil {
		p.retryOrFail(settle, job.ID, fmt.Sprintf("lease: %s", err))
		return
	}
	if !held {
		// Requeued or cancelled between acquire and lease; its next
		// claimant runs it.
		slog.Info("jobs: task no longer claimed by this pool; not running it", "job", job.ID, "type", job.Type)
		return
	}

	ctx, cancel := context.WithCancelCause(context.WithoutCancel(acquireCtx))
	defer cancel(nil)
	stopWatch := p.watchTask(acquireCtx, ctx, cancel, job.ID, claim)
	defer stopWatch()

	start := time.Now()
	result, err := h(ctx, job)

	switch cause := context.Cause(ctx); {
	case err == nil:
		if cerr := p.queue.Complete(settle, job.ID, result); cerr != nil {
			slog.Warn("jobs: mark task completed", "job", job.ID, "err", cerr)
		}
		p.emitTaskCompleted(settle, job.ID, time.Since(start).Milliseconds())
	case errors.Is(cause, ErrJobCancelled):
		slog.Info("jobs: task stopped: job cancelled", "job", job.ID, "type", job.Type)
	case errors.Is(cause, ErrLeaseLost):
		// Requeued after the lease ran out; its next claimant settles it.
		slog.Warn("jobs: task stopped: lease lost", "job", job.ID, "type", job.Type)
	case errors.Is(cause, ErrPoolStopped):
		// Left running on purpose, unleased: the next start's crash
		// recovery requeues it without spending a retry.
		if rerr := p.queue.ReleaseLease(settle, job.ID, claim.get()); rerr != nil {
			slog.Warn("jobs: release task lease", "job", job.ID, "err", rerr)
		}
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

// watchTask renews the job's lease every leaseTTL/3 and cancels ctx with
// ErrPoolStopped when the pool stops, with ErrJobCancelled when the job
// row is cancelled, and with ErrLeaseLost when the lease cannot be renewed
// because the job is no longer running under this pool's claim. The
// returned func stops the watcher.
func (p *WorkerPool) watchTask(acquireCtx, ctx context.Context, cancel context.CancelCauseFunc, jobID string, claim *taskClaim) func() {
	interval := p.pollInterval
	if interval <= 0 {
		interval = time.Second
	}
	done := make(chan struct{})
	go func() {
		poll := time.NewTicker(interval)
		defer poll.Stop()
		renew := time.NewTicker(p.leaseTTL / 3)
		defer renew.Stop()
		cancelled := func() bool {
			job, err := p.queue.Get(ctx, jobID)
			return err == nil && job.Status == jobCancelled
		}
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-acquireCtx.Done():
				cancel(ErrPoolStopped)
				return
			case <-poll.C:
				if cancelled() {
					cancel(ErrJobCancelled)
					return
				}
			case <-renew.C:
				token := claim.get()
				held, err := p.queue.ExtendLease(ctx, jobID, token, p.leaseTTL)
				switch {
				case err != nil:
					// The lease may still be live; the next tick retries.
					slog.Warn("jobs: renew task lease", "job", jobID, "err", err)
				case held, claim.get() != token:
					// Renewed, or this pool re-acquired the job meanwhile
					// and the next tick renews under the new claim.
				case cancelled():
					cancel(ErrJobCancelled)
					return
				default:
					cancel(ErrLeaseLost)
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
