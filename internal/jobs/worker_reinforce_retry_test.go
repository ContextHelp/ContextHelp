package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// flakyReinforceDriver fails the first Reinforce call with a lock error,
// as SQLite does under write contention, then delegates.
type flakyReinforceDriver struct {
	storage.StorageDriver
	failures atomic.Int32
}

func (d *flakyReinforceDriver) Objects() storage.ObjectStore {
	return &flakyReinforceObjects{ObjectStore: d.StorageDriver.Objects(), d: d}
}

type flakyReinforceObjects struct {
	storage.ObjectStore
	d *flakyReinforceDriver
}

func (o *flakyReinforceObjects) Reinforce(ctx context.Context, hash string, merge *storage.KnowledgeObject) (string, error) {
	if o.d.failures.Add(-1) >= 0 {
		return "", errors.New("reinforce: update: database is locked")
	}
	return o.ObjectStore.Reinforce(ctx, hash, merge)
}

// runUntilDone runs pool until job id reaches completed or failed.
func runUntilDone(t *testing.T, pool *WorkerPool, q *Queue, id string) *storage.Job {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		for {
			got, _ := q.Get(ctx, id)
			if got != nil && (got.Status == storage.JobCompleted || got.Status == storage.JobFailed) {
				cancel()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	pool.Start(ctx)
	got, err := q.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get job %s: %v", id, err)
	}
	return got
}

func TestReinforceTransientErrorRetries(t *testing.T) {
	driver := &flakyReinforceDriver{StorageDriver: storageutil.NewTestDriver(t)}
	q := NewQueue(driver.Jobs())
	pool := NewWorkerPool(q, builtins.Registry(), driver, 1, nil, defaultTestJobsCfg())

	first := makeJob("job-reinforce-first")
	first.Payload = "duplicate capture body"
	if err := q.Enqueue(context.Background(), first); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	orig := runUntilDone(t, pool, q, first.ID)
	if orig.Status != storage.JobCompleted {
		t.Fatalf("first job: got %q (err=%q), want completed", orig.Status, orig.Error)
	}

	// The duplicate takes the reinforce path; its first attempt hits the lock.
	driver.failures.Store(1)
	dup := makeJob("job-reinforce-dup")
	dup.Payload = first.Payload
	if err := q.Enqueue(context.Background(), dup); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	got := runUntilDone(t, pool, q, dup.ID)

	if got.Status != storage.JobCompleted {
		t.Fatalf("duplicate job: got %q (err=%q), want completed after retry", got.Status, got.Error)
	}
	if got.RetryCount != 1 {
		t.Errorf("retry_count: got %d, want 1", got.RetryCount)
	}
	if got.ResultID != orig.ResultID {
		t.Errorf("result_id: got %q, want reinforced object %q", got.ResultID, orig.ResultID)
	}
}
