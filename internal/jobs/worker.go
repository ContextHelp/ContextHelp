package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// WorkerPool runs pipeline jobs from the queue.
type WorkerPool struct {
	queue        *Queue
	pipelines    pipeline.Registry
	store        storage.StorageDriver
	workers      int
	staleTimeout time.Duration
	pollInterval time.Duration
}

// NewWorkerPool creates a worker pool.
func NewWorkerPool(queue *Queue, pipelines pipeline.Registry, store storage.StorageDriver, workers int) *WorkerPool {
	return &WorkerPool{
		queue:        queue,
		pipelines:    pipelines,
		store:        store,
		workers:      workers,
		staleTimeout: 30 * time.Minute,
		pollInterval: 500 * time.Millisecond,
	}
}

// Start runs the worker pool until the context is cancelled.
func (p *WorkerPool) Start(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	// Stale recovery goroutine.
	g.Go(func() error { return p.recoverStaleLoop(ctx) })

	// Worker goroutines.
	for i := 0; i < p.workers; i++ {
		g.Go(func() error { return p.workerLoop(ctx) })
	}

	return g.Wait()
}

func (p *WorkerPool) workerLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			job, err := p.queue.AcquireNext(ctx)
			if err != nil || job == nil {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(p.pollInterval):
				}
				continue
			}
			p.process(ctx, job)
		}
	}
}

func (p *WorkerPool) process(ctx context.Context, job *storage.Job) {
	pipe, err := p.pipelines.Get(job.Pipeline)
	if err != nil {
		p.queue.Fail(ctx, job.ID, fmt.Sprintf("pipeline: %s", err))
		return
	}

	draft := &storage.KnowledgeObject{
		ID:         uuid.New().String(),
		RawContent: job.Payload,
		Pipeline:   job.Pipeline,
		Source:     job.Source,
		CreatedAt:  time.Now(),
	}

	for _, step := range pipe.Steps {
		draft, err = step.Run(ctx, draft)
		if err != nil {
			p.queue.Fail(ctx, job.ID, fmt.Sprintf("step %s: %s", step.Name(), err))
			return
		}
	}

	draft.UpdatedAt = time.Now()

	if err := p.store.Objects().Create(ctx, draft); err != nil {
		p.queue.Fail(ctx, job.ID, fmt.Sprintf("store: %s", err))
		return
	}

	// Write edges for mentions (ADR-049).
	for _, mention := range draft.Mentions {
		edge := &storage.Edge{
			ID:        uuid.New().String(),
			FromType:  "object",
			FromID:    draft.ID,
			ToType:    "entity",
			ToID:      mention,
			EdgeType:  "mentions",
			Weight:    1.0,
			CreatedAt: time.Now(),
		}
		p.store.Edges().Create(ctx, edge)
	}

	p.queue.Complete(ctx, job.ID, draft.ID)
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
