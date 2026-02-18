package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func makeJob(id string) *storage.Job {
	now := time.Now().Truncate(time.Second)
	return &storage.Job{
		ID:         id,
		Type:       "ingest:text",
		Status:     storage.JobPending,
		Payload:    "content for " + id,
		Pipeline:   "text.short",
		Source:     "cli",
		MaxRetries: 3,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestQueueEnqueueAndGet(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	job := makeJob("job-1")
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	got, err := q.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "job-1" {
		t.Errorf("ID: got %q", got.ID)
	}
}

func TestQueueAcquireNext(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	j1 := makeJob("job-1")
	j1.CreatedAt = time.Now().Add(-2 * time.Second).Truncate(time.Second)
	q.Enqueue(ctx, j1)

	j2 := makeJob("job-2")
	j2.CreatedAt = time.Now().Add(-1 * time.Second).Truncate(time.Second)
	q.Enqueue(ctx, j2)

	acquired, err := q.AcquireNext(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if acquired.ID != "job-1" {
		t.Errorf("expected job-1, got %q", acquired.ID)
	}
	if acquired.Status != storage.JobRunning {
		t.Errorf("status: got %q", acquired.Status)
	}
}

func TestQueueAcquireNextEmpty(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())

	acquired, err := q.AcquireNext(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if acquired != nil {
		t.Errorf("expected nil, got %v", acquired)
	}
}

func TestQueueComplete(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	q.Enqueue(ctx, makeJob("job-1"))
	q.AcquireNext(ctx)

	if err := q.Complete(ctx, "job-1", "result-obj"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	got, _ := q.Get(ctx, "job-1")
	if got.Status != storage.JobCompleted {
		t.Errorf("status: got %q", got.Status)
	}
	if got.ResultID != "result-obj" {
		t.Errorf("result_id: got %q", got.ResultID)
	}
}

func TestQueueFail(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	q.Enqueue(ctx, makeJob("job-1"))
	q.AcquireNext(ctx)

	if err := q.Fail(ctx, "job-1", "broke"); err != nil {
		t.Fatalf("fail: %v", err)
	}

	got, _ := q.Get(ctx, "job-1")
	if got.Status != storage.JobFailed {
		t.Errorf("status: got %q", got.Status)
	}
}

func TestQueueRetry(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	q.Enqueue(ctx, makeJob("job-1"))
	q.AcquireNext(ctx)
	q.Fail(ctx, "job-1", "error")

	if err := q.Retry(ctx, "job-1"); err != nil {
		t.Fatalf("retry: %v", err)
	}

	got, _ := q.Get(ctx, "job-1")
	if got.Status != storage.JobPending {
		t.Errorf("status: got %q", got.Status)
	}
	if got.RetryCount != 1 {
		t.Errorf("retry_count: got %d", got.RetryCount)
	}
}

func TestQueueRetryMaxExceeded(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	job := makeJob("job-1")
	job.MaxRetries = 1
	q.Enqueue(ctx, job)
	q.AcquireNext(ctx)
	q.Fail(ctx, "job-1", "error")
	q.Retry(ctx, "job-1") // retry_count -> 1, equals max_retries

	q.AcquireNext(ctx)
	q.Fail(ctx, "job-1", "error again")

	err := q.Retry(ctx, "job-1")
	if err == nil {
		t.Fatal("expected error for max retries exceeded")
	}
}

func TestQueueRecoverStale(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	q.Enqueue(ctx, makeJob("job-1"))
	q.AcquireNext(ctx)

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

func TestQueueListByStatus(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	q.Enqueue(ctx, makeJob("job-1"))
	q.Enqueue(ctx, makeJob("job-2"))
	q.Enqueue(ctx, makeJob("job-3"))
	q.AcquireNext(ctx) // job-1 -> running

	jobs, total, err := q.List(ctx, storage.JobFilter{Status: storage.JobPending})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 {
		t.Errorf("total: got %d, want 2", total)
	}
	if len(jobs) != 2 {
		t.Errorf("count: got %d", len(jobs))
	}
}
