package sqlite

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestUpdate_ConcurrentWriters_NoLockErrors updates indexed objects from
// many goroutines while others append audit rows, as a request that
// rewrites an entity page does while the security emitter records the
// previous request's auth failure. Every call must succeed.
//
// Update opens with the FTS delete. That statement reads the content row
// under a read snapshot before FTS5 writes its shadow tables; if another
// writer commits in between, the upgrade fails with SQLITE_BUSY at once
// and busy_timeout never gets a chance to wait.
func TestUpdate_ConcurrentWriters_NoLockErrors(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	const n = 16
	objs := make([]*storage.KnowledgeObject, n)
	for i := range objs {
		objs[i] = makeObject(fmt.Sprintf("obj-update-concurrent-%d", i), "note")
		if err := d.Objects().Create(ctx, objs[i]); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	start := make(chan struct{})
	for i, obj := range objs {
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			obj.RawContent = "rewritten content " + obj.ID
			if err := d.Objects().Update(ctx, obj); err != nil {
				errs <- fmt.Errorf("update %s: %w", obj.ID, err)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			if err := d.AuditLog().Append(ctx, &storage.AuditEntry{
				ID: fmt.Sprintf("audit-update-concurrent-%d", i), EventType: "auth_failure",
				Payload: map[string]any{}, CreatedAt: time.Now().UTC(),
			}); err != nil {
				errs <- fmt.Errorf("audit append: %w", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	failed := 0
	for err := range errs {
		if failed == 0 {
			t.Errorf("%v", err)
		}
		failed++
	}
	if failed > 0 {
		t.Fatalf("%d/%d concurrent writes failed", failed, 2*n)
	}

	for _, obj := range objs {
		got, err := d.Objects().Get(ctx, obj.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.RawContent != "rewritten content "+obj.ID {
			t.Errorf("%s raw_content: got %q", obj.ID, got.RawContent)
		}
	}
}
