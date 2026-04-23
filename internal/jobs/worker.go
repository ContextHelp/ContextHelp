package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// FanOutFunc is called after a successful ingest with the new object ID.
// It runs fan-out enrichment (cross-reference edges, audit log).
// Returning an error is logged but does not fail the ingest job.
type FanOutFunc func(ctx context.Context, objectID string) error

// WorkerPool runs pipeline jobs from the queue.
type WorkerPool struct {
	queue        *Queue
	pipelines    pipeline.Registry
	store        storage.StorageDriver
	bus          events.Bus
	workers      int
	staleTimeout time.Duration
	pollInterval time.Duration
	drainTimeout time.Duration
	maxHops      int
	maxRetries   int
	fanOut       FanOutFunc
}

// NewWorkerPool creates a worker pool.
func NewWorkerPool(queue *Queue, pipelines pipeline.Registry, store storage.StorageDriver, workers int, bus events.Bus, cfg config.JobsConfig) *WorkerPool {
	return &WorkerPool{
		queue:        queue,
		pipelines:    pipelines,
		store:        store,
		bus:          bus,
		workers:      workers,
		staleTimeout: cfg.StaleTimeout,
		pollInterval: cfg.PollInterval,
		drainTimeout: cfg.DrainTimeout,
		maxHops:      cfg.MaxHops,
		maxRetries:   cfg.MaxRetries,
	}
}

// SetFanOut installs a post-ingest fan-out callback.
func (p *WorkerPool) SetFanOut(fn FanOutFunc) { p.fanOut = fn }

// Start runs the worker pool until the context is cancelled.
// On cancellation, in-flight jobs are given drainTimeout to finish before
// returning. Pass a drainTimeout of 0 to use the default (30s).
func (p *WorkerPool) Start(ctx context.Context) error {
	drain := p.drainTimeout
	if drain == 0 {
		drain = 30 * time.Second
	}

	g, acquireCtx := errgroup.WithContext(ctx)

	// drainCtx: jobs started before shutdown get up to drain seconds to finish.
	// It is not tied to acquireCtx so cancelling acquireCtx does not abort them.
	// The timeout starts when Start is called; in practice jobs are short-lived
	// so the deadline is only reached on hangs.
	drainCtx, drainCancel := context.WithTimeout(context.Background(), drain)
	defer drainCancel()

	// Stale recovery goroutine — stops acquiring when ctx is cancelled.
	g.Go(func() error { return p.recoverStaleLoop(acquireCtx) })

	// Worker goroutines — stop acquiring on ctx cancel but finish in-flight
	// jobs using drainCtx (bounded by drain timeout).
	for i := 0; i < p.workers; i++ {
		g.Go(func() error { return p.workerLoop(acquireCtx, drainCtx) })
	}

	return g.Wait()
}

func (p *WorkerPool) workerLoop(acquireCtx, jobCtx context.Context) error {
	for {
		select {
		case <-acquireCtx.Done():
			return nil
		default:
			job, err := p.queue.AcquireNext(acquireCtx)
			if err != nil || job == nil {
				select {
				case <-acquireCtx.Done():
					return nil
				case <-time.After(p.pollInterval):
				}
				continue
			}
			// Use jobCtx so in-flight work is not cancelled immediately on
			// shutdown — serve.go cancels jobCtx after drain timeout.
			p.process(jobCtx, job)
		}
	}
}

// runSteps executes all steps in a pipeline on draft, skipping any step that
// returns ErrDelegate and failing on any other error.
func (p *WorkerPool) runSteps(ctx context.Context, pipe *pipeline.Pipeline, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	var err error
	for _, step := range pipe.Steps {
		draft, err = step.Run(ctx, draft)
		if err != nil {
			if errors.Is(err, pipeline.ErrDelegate) {
				slog.Debug("jobs: step delegated, skipping", "step", step.Name())
				continue
			}
			return nil, fmt.Errorf("step %s: %w", step.Name(), err)
		}
	}
	return draft, nil
}

// processWithHops runs the initial pipeline and follows any hop requests
// (draft.Metadata["next_pipeline"]) up to maxHops total runs.
func (p *WorkerPool) processWithHops(ctx context.Context, job *storage.Job) (*storage.KnowledgeObject, error) {
	pipe, err := p.pipelines.Get(job.Pipeline)
	if err != nil {
		return nil, fmt.Errorf("pipeline: %w", err)
	}

	draft := &storage.KnowledgeObject{
		ID:         uuid.New().String(),
		RawContent: job.Payload,
		Pipeline:   job.Pipeline,
		Source:     job.Source,
		CreatedAt:  time.Now(),
	}

	draft, err = p.runSteps(ctx, pipe, draft)
	if err != nil {
		return nil, err
	}

	for hop := 1; hop < p.maxHops; hop++ {
		if draft.Metadata == nil {
			break
		}
		next, ok := draft.Metadata["next_pipeline"].(string)
		if !ok || next == "" {
			break
		}
		delete(draft.Metadata, "next_pipeline")

		nextPipe, err := p.pipelines.Get(next)
		if err != nil {
			slog.Debug("jobs: hop pipeline not found, stopping hops", "pipeline", next)
			break
		}
		draft, err = p.runSteps(ctx, nextPipe, draft)
		if err != nil {
			return nil, err
		}
	}

	return draft, nil
}

func (p *WorkerPool) process(ctx context.Context, job *storage.Job) {
	draft, err := p.processWithHops(ctx, job)
	if err != nil {
		var perm *pipeline.PermanentError
		if errors.As(err, &perm) {
			slog.Debug("jobs: permanent error, skipping retry", "job", job.ID, "err", err)
			p.queue.Fail(ctx, job.ID, err.Error())
			p.emitFailed(ctx, job.ID, err.Error())
			return
		}
		// Attempt retry for transient errors; fall back to Fail on exhaustion.
		if retryErr := p.queue.Retry(ctx, job.ID); retryErr != nil {
			p.queue.Fail(ctx, job.ID, err.Error())
			p.emitFailed(ctx, job.ID, err.Error())
		}
		return
	}

	draft.UpdatedAt = time.Now()
	draft.ContentHash = storageutil.ContentHash(draft.RawContent, draft.Source)

	existing, err := p.store.Objects().GetByContentHash(ctx, draft.ContentHash)
	if err == nil && existing != nil {
		existingID, err := p.store.Objects().Reinforce(ctx, draft.ContentHash, draft)
		if err != nil {
			p.queue.Fail(ctx, job.ID, fmt.Sprintf("reinforce: %s", err))
			p.emitFailed(ctx, job.ID, fmt.Sprintf("reinforce: %s", err))
			return
		}
		p.queue.Complete(ctx, job.ID, existingID)
		p.emitCompleted(ctx, job.ID, existingID)
		return
	}

	draft.ReinforcementCount = 1

	if err := p.store.Objects().Create(ctx, draft); err != nil {
		// Unique constraint race: another worker inserted the same hash concurrently.
		// The other worker's insert may not be visible yet (WAL delay), so retry
		// with short backoff before giving up.
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			backoffs := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond}
			var existingID string
			var rerr error
			for i, delay := range backoffs {
				existingID, rerr = p.store.Objects().Reinforce(ctx, draft.ContentHash, draft)
				if rerr == nil {
					break
				}
				if !errors.Is(rerr, sql.ErrNoRows) {
					break
				}
				slog.Debug("jobs: reinforce after race retry",
					"attempt", i+1,
					"delay", delay,
					"hash", draft.ContentHash,
				)
				// Don't sleep after the last attempt.
				if i >= len(backoffs)-1 {
					break
				}
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					rerr = ctx.Err()
				case <-timer.C:
				}
				if rerr != nil {
					break
				}
			}
			if rerr != nil {
				p.queue.Fail(ctx, job.ID, fmt.Sprintf("reinforce after race: %s", rerr))
				p.emitFailed(ctx, job.ID, fmt.Sprintf("reinforce after race: %s", rerr))
				return
			}
			p.queue.Complete(ctx, job.ID, existingID)
			p.emitCompleted(ctx, job.ID, existingID)
			return
		}
		p.queue.Fail(ctx, job.ID, fmt.Sprintf("store: %s", err))
		p.emitFailed(ctx, job.ID, fmt.Sprintf("store: %s", err))
		return
	}

	// Write edges for mentions (ADR-049).
	// Any edge write failure rolls back by failing the job; the object has
	// already been persisted but no edges exist yet, so the job is retryable
	// and a subsequent run will re-create the object (content-hash dedup) and
	// re-attempt edge writes.
	for _, mention := range draft.Mentions {
		edge := &storage.Edge{
			ID:        uuid.New().String(),
			FromType:  "object",
			FromID:    draft.ID,
			ToType:    "entity",
			ToID:      mention.String(),
			EdgeType:  "mentions",
			Weight:    1.0,
			CreatedAt: time.Now(),
		}
		if err := p.store.Edges().Create(ctx, edge); err != nil {
			reason := fmt.Sprintf("edge write: mention %s: %s", mention.String(), err)
			p.queue.Fail(ctx, job.ID, reason)
			p.emitFailed(ctx, job.ID, reason)
			return
		}
	}

	p.emitObjectCreated(ctx, draft.ID)
	p.queue.Complete(ctx, job.ID, draft.ID)
	p.emitCompleted(ctx, job.ID, draft.ID)

	// Post-ingest fan-out enrichment (bidirectional edges, audit log).
	if p.fanOut != nil && !strings.HasSuffix(job.Type, ":nofanout") {
		if err := p.fanOut(ctx, draft.ID); err != nil {
			slog.Warn("jobs: fan-out enrichment failed", "object", draft.ID, "err", err)
		}
	}

	// Fan out per-item jobs if the pipeline staged items for enqueueing.
	p.fanOutItems(ctx, draft)
}

// fanOutItems enqueues per-item jobs from Metadata["items_to_enqueue"].
func (p *WorkerPool) fanOutItems(ctx context.Context, draft *storage.KnowledgeObject) {
	if draft.Metadata == nil {
		return
	}
	rawItems, ok := draft.Metadata["items_to_enqueue"]
	if !ok {
		return
	}
	items, ok := rawItems.([]map[string]any)
	if !ok || len(items) == 0 {
		return
	}
	now := time.Now().Truncate(time.Second)
	for _, item := range items {
		content, _ := item["content"].(string)
		link, _ := item["link"].(string)
		if content == "" {
			content = link
		}
		if content == "" {
			continue
		}
		source, _ := item["source"].(string)
		itemPipeline, _ := item["pipeline"].(string)
		if itemPipeline == "" {
			itemPipeline = "feed.ingest"
		}
		job := &storage.Job{
			ID:         uuid.New().String(),
			Type:       "ingest:feed_item",
			Status:     storage.JobPending,
			Payload:    content,
			Pipeline:   itemPipeline,
			Source:     source,
			MaxRetries: p.maxRetries,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := p.queue.Enqueue(ctx, job); err != nil {
			slog.Warn("jobs: fanOut enqueue failed", "err", err)
			continue
		}
		if p.bus != nil {
			if ev, err := events.NewEvent("worker.pool.fanout", "job.enqueued", job); err == nil {
				_ = p.bus.Publish(ctx, ev)
			}
		}
	}
}

func (p *WorkerPool) emitCompleted(ctx context.Context, jobID, resultID string) {
	if p.bus == nil {
		return
	}
	ev, err := events.NewEvent("worker.pool", "job.completed", map[string]string{
		"job_id":    jobID,
		"result_id": resultID,
	})
	if err == nil {
		_ = p.bus.Publish(ctx, ev)
	}
}

func (p *WorkerPool) emitFailed(ctx context.Context, jobID, reason string) {
	if p.bus == nil {
		return
	}
	ev, err := events.NewEvent("worker.pool", "job.failed", map[string]string{
		"job_id": jobID,
		"error":  reason,
	})
	if err == nil {
		_ = p.bus.Publish(ctx, ev)
	}
}

func (p *WorkerPool) emitObjectCreated(ctx context.Context, objectID string) {
	if p.bus == nil {
		return
	}
	ev, err := events.NewEvent("worker.pool", "object.created", map[string]string{
		"id": objectID,
	})
	if err == nil {
		_ = p.bus.Publish(ctx, ev)
	}
}

func (p *WorkerPool) recoverStaleLoop(ctx context.Context) error {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			p.queue.RecoverStale(ctx, int64(p.staleTimeout.Seconds()))
		}
	}
}
