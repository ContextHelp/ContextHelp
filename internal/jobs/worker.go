package jobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

const maxHops = 5

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

// runSteps executes all steps in a pipeline on draft, skipping any step that
// returns ErrDelegate and failing on any other error.
func (p *WorkerPool) runSteps(ctx context.Context, pipe *pipeline.Pipeline, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	var err error
	for _, step := range pipe.Steps {
		draft, err = step.Run(ctx, draft)
		if err != nil {
			if errors.Is(err, pipeline.ErrDelegate) {
				log.Printf("jobs: step %q delegated, skipping", step.Name())
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

	for hop := 1; hop < maxHops; hop++ {
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
			log.Printf("jobs: hop pipeline %q not found, stopping hops", next)
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
		p.queue.Fail(ctx, job.ID, err.Error())
		return
	}

	draft.UpdatedAt = time.Now()
	draft.ContentHash = storageutil.ContentHash(draft.RawContent, draft.Source)

	existing, err := p.store.Objects().GetByContentHash(ctx, draft.ContentHash)
	if err == nil && existing != nil {
		existingID, err := p.store.Objects().Reinforce(ctx, draft.ContentHash, draft)
		if err != nil {
			p.queue.Fail(ctx, job.ID, fmt.Sprintf("reinforce: %s", err))
			return
		}
		p.queue.Complete(ctx, job.ID, existingID)
		return
	}

	draft.ReinforcementCount = 1

	if err := p.store.Objects().Create(ctx, draft); err != nil {
		// Unique constraint race: another worker inserted the same hash concurrently.
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			existingID, rerr := p.store.Objects().Reinforce(ctx, draft.ContentHash, draft)
			if rerr != nil {
				p.queue.Fail(ctx, job.ID, fmt.Sprintf("reinforce after race: %s", rerr))
				return
			}
			p.queue.Complete(ctx, job.ID, existingID)
			return
		}
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
