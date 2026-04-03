package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// permanentStep always returns a PermanentError.
type permanentStep struct {
	pipeline.BaseContract
}

func (s *permanentStep) Name() string { return "permanent-fail" }
func (s *permanentStep) Run(_ context.Context, _ *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return nil, pipeline.Permanent(errors.New("no such host"))
}

// transientStep always returns a transient (retryable) error.
type transientStep struct {
	pipeline.BaseContract
}

func (s *transientStep) Name() string { return "transient-fail" }
func (s *transientStep) Run(_ context.Context, _ *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return nil, errors.New("connection reset")
}

func TestPermanentErrorSkipsRetry(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())

	pipes := pipeline.NewRegistry()
	pipes.Register("test.permanent", &pipeline.Pipeline{
		PipelineName: "test.permanent",
		Steps:        []pipeline.PipelineStep{&permanentStep{}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := makeJob("job-perm-1")
	job.Pipeline = "test.permanent"
	job.MaxRetries = 3
	q.Enqueue(ctx, job)

	pool := NewWorkerPool(q, pipes, driver, 1, nil, config.JobsConfig{
		PollInterval: 50 * time.Millisecond,
		StaleTimeout: 30 * time.Minute,
		MaxRetries:   3,
		MaxHops:      5,
	})

	go func() {
		for {
			got, _ := q.Get(ctx, "job-perm-1")
			if got != nil && got.Status == storage.JobFailed {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	got, _ := q.Get(context.Background(), "job-perm-1")
	if got.Status != storage.JobFailed {
		t.Fatalf("status: got %q, want failed", got.Status)
	}
	// Permanent errors should not have been retried at all.
	if got.RetryCount != 0 {
		t.Errorf("retry_count: got %d, want 0 (permanent errors skip retry)", got.RetryCount)
	}
}

func TestTransientErrorUsesRetry(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())

	pipes := pipeline.NewRegistry()
	pipes.Register("test.transient", &pipeline.Pipeline{
		PipelineName: "test.transient",
		Steps:        []pipeline.PipelineStep{&transientStep{}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := makeJob("job-trans-1")
	job.Pipeline = "test.transient"
	job.MaxRetries = 3
	q.Enqueue(ctx, job)

	pool := NewWorkerPool(q, pipes, driver, 1, nil, config.JobsConfig{
		PollInterval: 50 * time.Millisecond,
		StaleTimeout: 30 * time.Minute,
		MaxRetries:   3,
		MaxHops:      5,
	})

	// Let the pool run until the job has been retried and eventually failed.
	go func() {
		for {
			got, _ := q.Get(ctx, "job-trans-1")
			if got != nil && got.Status == storage.JobFailed {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	got, _ := q.Get(context.Background(), "job-trans-1")
	if got.Status != storage.JobFailed {
		t.Fatalf("status: got %q, want failed", got.Status)
	}
	// Transient errors should have used all retries before failing.
	if got.RetryCount < 1 {
		t.Errorf("retry_count: got %d, want >= 1 (transient errors should retry)", got.RetryCount)
	}
}
