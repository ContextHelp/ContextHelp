package storagetest

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// JobRetryConformance pins JobStore.Retry and Requeue. Retry is the
// operator's: it requeues failed jobs only (a missing job is ErrNotFound,
// any other status or no retries left is ErrJobNotRetryable), so a running
// job is never given to a second worker. Requeue is the worker's: it
// requeues a running job only under the claim it holds (ErrClaimLost
// otherwise).
func JobRetryConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	jobs := drv.Jobs()
	now := fixtureTime()
	mk := func(id string, max int) {
		t.Helper()
		if err := jobs.Create(ctx, &storage.Job{
			ID: id, Type: "ingest:text", Status: storage.JobPending, Payload: id,
			Pipeline: "text.short", Source: "cli", MaxRetries: max, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	status := func(id string) storage.JobStatus {
		t.Helper()
		j, err := jobs.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		return j.Status
	}
	acquire := func(want string) *storage.Job {
		t.Helper()
		j, err := jobs.AcquireNext(ctx)
		if err != nil || j == nil || j.ID != want {
			t.Fatalf("AcquireNext = %+v, %v; want %s", j, err, want)
		}
		return j
	}

	t.Run("retry refuses pending running completed cancelled", func(t *testing.T) {
		mk("jr-pending", 3)
		if err := jobs.Retry(ctx, "jr-pending"); !errors.Is(err, storage.ErrJobNotRetryable) {
			t.Errorf("pending: %v, want ErrJobNotRetryable", err)
		}
		acquire("jr-pending")
		if err := jobs.Retry(ctx, "jr-pending"); !errors.Is(err, storage.ErrJobNotRetryable) {
			t.Errorf("running: %v, want ErrJobNotRetryable", err)
		}
		if got := status("jr-pending"); got != storage.JobRunning {
			t.Errorf("running job moved to %q", got)
		}
		if err := jobs.Complete(ctx, "jr-pending", "res"); err != nil {
			t.Fatal(err)
		}
		if err := jobs.Retry(ctx, "jr-pending"); !errors.Is(err, storage.ErrJobNotRetryable) {
			t.Errorf("completed: %v, want ErrJobNotRetryable", err)
		}
		mk("jr-cancel", 3)
		if err := jobs.Cancel(ctx, "jr-cancel"); err != nil {
			t.Fatal(err)
		}
		if err := jobs.Retry(ctx, "jr-cancel"); !errors.Is(err, storage.ErrJobNotRetryable) {
			t.Errorf("cancelled: %v, want ErrJobNotRetryable", err)
		}
	})

	t.Run("retry missing is not found", func(t *testing.T) {
		if err := jobs.Retry(ctx, "jr-nope"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("missing: %v, want ErrNotFound", err)
		}
	})

	t.Run("retry requeues failed until retries are used up", func(t *testing.T) {
		mk("jr-failed", 1)
		acquire("jr-failed")
		if err := jobs.Fail(ctx, "jr-failed", "boom"); err != nil {
			t.Fatal(err)
		}
		if err := jobs.Retry(ctx, "jr-failed"); err != nil {
			t.Fatalf("failed job: %v", err)
		}
		acquire("jr-failed")
		if err := jobs.Fail(ctx, "jr-failed", "boom"); err != nil {
			t.Fatal(err)
		}
		if err := jobs.Retry(ctx, "jr-failed"); !errors.Is(err, storage.ErrJobNotRetryable) {
			t.Errorf("exhausted: %v, want ErrJobNotRetryable", err)
		}
	})

	t.Run("requeue only under the held claim", func(t *testing.T) {
		mk("jr-claim", 3)
		held := acquire("jr-claim")
		if err := jobs.Requeue(ctx, "jr-claim", "someone-else"); !errors.Is(err, storage.ErrClaimLost) {
			t.Errorf("foreign claim: %v, want ErrClaimLost", err)
		}
		if got := status("jr-claim"); got != storage.JobRunning {
			t.Errorf("foreign requeue moved job to %q", got)
		}
		if err := jobs.Requeue(ctx, "jr-claim", held.Claim); err != nil {
			t.Fatalf("own claim: %v", err)
		}
		if got := status("jr-claim"); got != storage.JobPending {
			t.Errorf("requeued job is %q, want pending", got)
		}
		// The claim is void once requeued: a stale worker cannot requeue again.
		if err := jobs.Requeue(ctx, "jr-claim", held.Claim); !errors.Is(err, storage.ErrClaimLost) {
			t.Errorf("stale claim: %v, want ErrClaimLost", err)
		}
		if err := jobs.Cancel(ctx, "jr-claim"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("requeue out of retries", func(t *testing.T) {
		mk("jr-spent", 0)
		held := acquire("jr-spent")
		if err := jobs.Requeue(ctx, "jr-spent", held.Claim); !errors.Is(err, storage.ErrJobNotRetryable) {
			t.Errorf("exhausted: %v, want ErrJobNotRetryable", err)
		}
	})
}
