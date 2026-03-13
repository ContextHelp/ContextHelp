package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestProcessJob(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := makeJob("job-1")
	job.Payload = "short text content"
	job.Pipeline = "text.short"
	q.Enqueue(ctx, job)

	pool := NewWorkerPool(q, pipes, driver, 1, nil)
	pool.pollInterval = 50 * time.Millisecond

	// Run pool in background, cancel after processing.
	go func() {
		for {
			got, _ := q.Get(ctx, "job-1")
			if got != nil && got.Status == storage.JobCompleted {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	got, _ := q.Get(context.Background(), "job-1")
	if got.Status != storage.JobCompleted {
		t.Fatalf("status: got %q, want completed", got.Status)
	}
	if got.ResultID == "" {
		t.Fatal("result_id should be set")
	}

	// Verify object was created.
	obj, err := driver.Objects().Get(context.Background(), got.ResultID)
	if err != nil {
		t.Fatalf("get object: %v", err)
	}
	if obj.Pipeline != "text.short" {
		t.Errorf("pipeline: got %q", obj.Pipeline)
	}
}

func TestProcessJobCreatesEdges(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())

	// Create a custom pipeline that adds mentions.
	pipes := pipeline.NewRegistry()
	pipes.Register("test.mentions", &pipeline.Pipeline{
		PipelineName: "test.mentions",
		Steps:        []pipeline.PipelineStep{&mentionStep{}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := makeJob("job-1")
	job.Pipeline = "test.mentions"
	q.Enqueue(ctx, job)

	pool := NewWorkerPool(q, pipes, driver, 1, nil)
	pool.pollInterval = 50 * time.Millisecond

	go func() {
		for {
			got, _ := q.Get(ctx, "job-1")
			if got != nil && got.Status == storage.JobCompleted {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	got, _ := q.Get(context.Background(), "job-1")
	if got.ResultID == "" {
		t.Fatal("result_id should be set")
	}

	edges, err := driver.Edges().ListFrom(context.Background(), "object", got.ResultID)
	if err != nil {
		t.Fatalf("list edges: %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("edges: got %d, want 1", len(edges))
	}
}

func TestProcessJobFailure(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	pipes := pipeline.NewRegistry() // empty registry — pipeline won't be found

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := makeJob("job-1")
	job.Pipeline = "nonexistent"
	q.Enqueue(ctx, job)

	pool := NewWorkerPool(q, pipes, driver, 1, nil)
	pool.pollInterval = 50 * time.Millisecond

	go func() {
		for {
			got, _ := q.Get(ctx, "job-1")
			if got != nil && got.Status == storage.JobFailed {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	got, _ := q.Get(context.Background(), "job-1")
	if got.Status != storage.JobFailed {
		t.Errorf("status: got %q, want failed", got.Status)
	}
}

func TestWorkerPoolShutdown(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	pipes := builtins.Registry()

	ctx, cancel := context.WithCancel(context.Background())
	pool := NewWorkerPool(q, pipes, driver, 2, nil)
	pool.pollInterval = 50 * time.Millisecond

	done := make(chan struct{})
	go func() {
		pool.Start(ctx)
		close(done)
	}()

	// Cancel immediately.
	cancel()

	select {
	case <-done:
		// Clean exit.
	case <-time.After(3 * time.Second):
		t.Fatal("pool did not shut down in time")
	}
}

func TestStaleRecovery(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	// Simulate a stale job: enqueue and acquire (making it "running").
	q.Enqueue(ctx, makeJob("job-1"))
	q.AcquireNext(ctx)

	// Recover with 0 timeout — all running jobs are stale.
	n, err := q.RecoverStale(ctx, 0)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if n != 1 {
		t.Errorf("recovered: got %d", n)
	}

	got, _ := q.Get(ctx, "job-1")
	if got.Status != storage.JobPending {
		t.Errorf("status: got %q", got.Status)
	}
}

// mentionStep is a test step that adds a mention to the draft.
type mentionStep struct {
	pipeline.BaseContract
}

func (s *mentionStep) Name() string { return "test-mention" }
func (s *mentionStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Mentions = []string{"@test.entity"}
	return draft, nil
}
