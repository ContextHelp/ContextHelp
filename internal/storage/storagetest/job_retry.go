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
	h := &jobRetryHarness{t: t, jobs: drv.Jobs()}
	t.Run("retry refuses pending running completed cancelled", h.retryRefusesNonFailed)
	t.Run("retry missing is not found", h.retryMissing)
	t.Run("retry requeues failed until retries are used up", h.retryFailedUntilSpent)
	t.Run("requeue only under the held claim", h.requeueUnderClaim)
	t.Run("requeue out of retries", h.requeueSpent)
}

// jobRetryHarness holds the store and the small helpers the subtests share.
type jobRetryHarness struct {
	t    *testing.T
	jobs storage.JobStore
}

func (h *jobRetryHarness) mk(id string, max int) {
	h.t.Helper()
	now := fixtureTime()
	if err := h.jobs.Create(context.Background(), &storage.Job{
		ID: id, Type: "ingest:text", Status: storage.JobPending, Payload: id,
		Pipeline: "text.short", Source: "cli", MaxRetries: max, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		h.t.Fatalf("Create %s: %v", id, err)
	}
}

func (h *jobRetryHarness) status(id string) storage.JobStatus {
	h.t.Helper()
	j, err := h.jobs.Get(context.Background(), id)
	if err != nil {
		h.t.Fatalf("Get %s: %v", id, err)
	}
	return j.Status
}

func (h *jobRetryHarness) acquire(want string) *storage.Job {
	h.t.Helper()
	j, err := h.jobs.AcquireNext(context.Background())
	if err != nil || j == nil || j.ID != want {
		h.t.Fatalf("AcquireNext = %+v, %v; want %s", j, err, want)
	}
	return j
}

func (h *jobRetryHarness) must(err error) {
	h.t.Helper()
	if err != nil {
		h.t.Fatal(err)
	}
}

func (h *jobRetryHarness) wantRetry(id, label string, want error) {
	h.t.Helper()
	if err := h.jobs.Retry(context.Background(), id); !errors.Is(err, want) {
		h.t.Errorf("%s: %v, want %v", label, err, want)
	}
}

func (h *jobRetryHarness) wantRequeue(id, claim, label string, want error) {
	h.t.Helper()
	if err := h.jobs.Requeue(context.Background(), id, claim); !errors.Is(err, want) {
		h.t.Errorf("%s: %v, want %v", label, err, want)
	}
}

func (h *jobRetryHarness) retryRefusesNonFailed(*testing.T) {
	ctx := context.Background()
	h.mk("jr-pending", 3)
	h.wantRetry("jr-pending", "pending", storage.ErrJobNotRetryable)
	h.acquire("jr-pending")
	h.wantRetry("jr-pending", "running", storage.ErrJobNotRetryable)
	if got := h.status("jr-pending"); got != storage.JobRunning {
		h.t.Errorf("running job moved to %q", got)
	}
	h.must(h.jobs.Complete(ctx, "jr-pending", "res"))
	h.wantRetry("jr-pending", "completed", storage.ErrJobNotRetryable)
	h.mk("jr-cancel", 3)
	h.must(h.jobs.Cancel(ctx, "jr-cancel"))
	h.wantRetry("jr-cancel", "cancelled", storage.ErrJobNotRetryable)
}

func (h *jobRetryHarness) retryMissing(*testing.T) {
	h.wantRetry("jr-nope", "missing", storage.ErrNotFound)
}

func (h *jobRetryHarness) retryFailedUntilSpent(*testing.T) {
	ctx := context.Background()
	h.mk("jr-failed", 1)
	h.acquire("jr-failed")
	h.must(h.jobs.Fail(ctx, "jr-failed", "boom"))
	h.must(h.jobs.Retry(ctx, "jr-failed"))
	h.acquire("jr-failed")
	h.must(h.jobs.Fail(ctx, "jr-failed", "boom"))
	h.wantRetry("jr-failed", "exhausted", storage.ErrJobNotRetryable)
}

func (h *jobRetryHarness) requeueUnderClaim(*testing.T) {
	ctx := context.Background()
	h.mk("jr-claim", 3)
	held := h.acquire("jr-claim")
	h.wantRequeue("jr-claim", "someone-else", "foreign claim", storage.ErrClaimLost)
	if got := h.status("jr-claim"); got != storage.JobRunning {
		h.t.Errorf("foreign requeue moved job to %q", got)
	}
	h.must(h.jobs.Requeue(ctx, "jr-claim", held.Claim))
	if got := h.status("jr-claim"); got != storage.JobPending {
		h.t.Errorf("requeued job is %q, want pending", got)
	}
	// The claim is void once requeued: a stale worker cannot requeue again.
	h.wantRequeue("jr-claim", held.Claim, "stale claim", storage.ErrClaimLost)
	h.must(h.jobs.Cancel(ctx, "jr-claim"))
}

func (h *jobRetryHarness) requeueSpent(*testing.T) {
	h.mk("jr-spent", 0)
	held := h.acquire("jr-spent")
	h.wantRequeue("jr-spent", held.Claim, "exhausted", storage.ErrJobNotRetryable)
}
