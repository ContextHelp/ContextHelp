package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
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

func TestEnqueueAndGet(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	job := makeJob("job-1")
	if err := d.Jobs().Create(ctx, job); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := d.Jobs().Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "job-1" {
		t.Errorf("ID: got %q", got.ID)
	}
	if got.Status != storage.JobPending {
		t.Errorf("Status: got %q", got.Status)
	}
}

func TestAcquireNext(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	// Create two jobs with slightly different timestamps.
	j1 := makeJob("job-1")
	j1.CreatedAt = time.Now().Add(-2 * time.Second).Truncate(time.Second)
	d.Jobs().Create(ctx, j1)

	j2 := makeJob("job-2")
	j2.CreatedAt = time.Now().Add(-1 * time.Second).Truncate(time.Second)
	d.Jobs().Create(ctx, j2)

	acquired, err := d.Jobs().AcquireNext(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if acquired == nil {
		t.Fatal("expected job, got nil")
	}
	if acquired.ID != "job-1" {
		t.Errorf("expected job-1, got %q", acquired.ID)
	}
	if acquired.Status != storage.JobRunning {
		t.Errorf("status: got %q, want running", acquired.Status)
	}
}

func TestAcquireNextEmpty(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	acquired, err := d.Jobs().AcquireNext(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if acquired != nil {
		t.Errorf("expected nil, got %v", acquired)
	}
}

func TestJobComplete(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Jobs().Create(ctx, makeJob("job-1"))
	d.Jobs().AcquireNext(ctx)

	if err := d.Jobs().Complete(ctx, "job-1", "obj-result"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	got, _ := d.Jobs().Get(ctx, "job-1")
	if got.Status != storage.JobCompleted {
		t.Errorf("status: got %q", got.Status)
	}
	if got.ResultID != "obj-result" {
		t.Errorf("result_id: got %q", got.ResultID)
	}
}

func TestJobFail(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Jobs().Create(ctx, makeJob("job-1"))
	d.Jobs().AcquireNext(ctx)

	if err := d.Jobs().Fail(ctx, "job-1", "something broke"); err != nil {
		t.Fatalf("fail: %v", err)
	}

	got, _ := d.Jobs().Get(ctx, "job-1")
	if got.Status != storage.JobFailed {
		t.Errorf("status: got %q", got.Status)
	}
	if got.Error != "something broke" {
		t.Errorf("error: got %q", got.Error)
	}
}

func TestJobRetry(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Jobs().Create(ctx, makeJob("job-1"))
	d.Jobs().AcquireNext(ctx)
	d.Jobs().Fail(ctx, "job-1", "error")

	if err := d.Jobs().Retry(ctx, "job-1"); err != nil {
		t.Fatalf("retry: %v", err)
	}

	got, _ := d.Jobs().Get(ctx, "job-1")
	if got.Status != storage.JobPending {
		t.Errorf("status: got %q", got.Status)
	}
	if got.RetryCount != 1 {
		t.Errorf("retry_count: got %d, want 1", got.RetryCount)
	}
}

func TestJobList(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Jobs().Create(ctx, makeJob("job-1"))
	d.Jobs().Create(ctx, makeJob("job-2"))
	j3 := makeJob("job-3")
	d.Jobs().Create(ctx, j3)
	d.Jobs().AcquireNext(ctx) // makes one running

	jobs, total, err := d.Jobs().List(ctx, storage.JobFilter{Status: storage.JobPending})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 {
		t.Errorf("total pending: got %d, want 2", total)
	}
	if len(jobs) != 2 {
		t.Errorf("count: got %d", len(jobs))
	}
}

func TestRecoverStale(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Jobs().Create(ctx, makeJob("job-1"))
	d.Jobs().AcquireNext(ctx)

	// Recover with 0 timeout means all running jobs are stale.
	n, err := d.Jobs().RecoverStale(ctx, 0)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if n != 1 {
		t.Errorf("recovered: got %d, want 1", n)
	}

	got, _ := d.Jobs().Get(ctx, "job-1")
	if got.Status != storage.JobPending {
		t.Errorf("status: got %q, want pending", got.Status)
	}
}
