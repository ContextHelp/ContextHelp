package sqlite

import (
	"context"
	"sync"
	"testing"
)

// TestReinforce_ConcurrentSameHash_NoLockErrors reinforces one row from many
// goroutines at once, as a fan-out burst of duplicate jobs does. Every call
// must succeed and every increment must land.
//
// A deferred transaction that reads before it writes holds a read snapshot;
// if another writer commits first, the upgrade to a write lock fails with
// SQLITE_BUSY at once and busy_timeout never gets a chance to wait.
func TestReinforce_ConcurrentSameHash_NoLockErrors(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj := makeObject("obj-reinforce-concurrent", "note")
	obj.ContentHash = "hash-reinforce-concurrent"
	obj.ReinforcementCount = 1
	if err := d.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}

	const n = 32
	var wg sync.WaitGroup
	errs := make(chan error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := d.Objects().Reinforce(ctx, obj.ContentHash, makeObject("merge", "note")); err != nil {
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	failed := 0
	for err := range errs {
		if failed == 0 {
			t.Errorf("Reinforce: %v", err)
		}
		failed++
	}
	if failed > 0 {
		t.Fatalf("%d/%d concurrent Reinforce calls failed", failed, n)
	}

	got, err := d.Objects().Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ReinforcementCount != 1+n {
		t.Errorf("reinforcement_count: got %d, want %d", got.ReinforcementCount, 1+n)
	}
}
