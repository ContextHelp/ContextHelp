//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func outboxEv(n int) *storage.OutboxEvent {
	return &storage.OutboxEvent{
		Topic: "ctxt.object.created", Source: "test", ObjectID: fmt.Sprintf("o-%d", n),
		Payload: map[string]int{"n": n}, OccurredAt: time.Now(),
	}
}

// An append in flight holds the outbox lock until it commits: a second
// append blocks, readers see neither, and seq order is commit order.
func TestOutbox_AppendHoldsLockUntilCommit(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()
	ob := drv.Outbox()
	// Seed one row so instance_meta exists: its insert would otherwise
	// serialize the appends and mask a missing outbox lock.
	if err := ob.Append(ctx, outboxEv(0)); err != nil {
		t.Fatal(err)
	}
	base, err := ob.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}

	written, release := make(chan struct{}), make(chan struct{})
	firstDone := make(chan error, 1)
	first := outboxEv(1)
	go func() {
		firstDone <- drv.AppendOutboxHeldForTest(ctx, first, func() { close(written); <-release })
	}()
	<-written

	secondDone := make(chan error, 1)
	second := outboxEv(2)
	go func() { secondDone <- ob.Append(ctx, second) }()

	select {
	case err := <-secondDone:
		t.Fatalf("second append finished while the first was uncommitted (err=%v)", err)
	case <-time.After(500 * time.Millisecond):
	}
	if rows, err := ob.Since(ctx, base, 10); err != nil || len(rows) != 0 {
		t.Fatalf("Since while first uncommitted = %d rows, err %v; want none", len(rows), err)
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second append: %v", err)
	}
	if second.Seq <= first.Seq {
		t.Fatalf("seq order != commit order: first=%d second=%d", first.Seq, second.Seq)
	}
	rows, err := ob.Since(ctx, base, 10)
	if err != nil || len(rows) != 2 || rows[0].Seq != first.Seq || rows[1].Seq != second.Seq {
		t.Fatalf("Since = %+v, err %v; want [first second]", rows, err)
	}
}

// Concurrent appenders against a cursor-following reader: the reader
// never skips a late-committing lower seq, so it ends with every row, in
// order, exactly once.
func TestOutbox_ConcurrentAppendsNoSkippedSeq(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()
	ob := drv.Outbox()
	const writers, per = 8, 25

	var wg sync.WaitGroup
	errs := make(chan error, writers*per)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				if err := ob.Append(ctx, outboxEv(w*per+i)); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	var seen []int64
	var cursor int64
	drain := func() {
		for {
			rows, err := ob.Since(ctx, cursor, 7)
			if err != nil {
				t.Errorf("Since(%d): %v", cursor, err)
				return
			}
			if len(rows) == 0 {
				return
			}
			for _, r := range rows {
				seen = append(seen, r.Seq)
				cursor = r.Seq
			}
		}
	}
	for finished := false; !finished; {
		select {
		case <-done:
			finished = true
		default:
		}
		drain()
	}
	drain()
	close(errs)
	for err := range errs {
		t.Fatalf("append: %v", err)
	}

	all, err := ob.Since(ctx, 0, writers*per+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != writers*per || len(seen) != len(all) {
		t.Fatalf("rows=%d seen=%d want %d each", len(all), len(seen), writers*per)
	}
	for i := range all {
		if seen[i] != all[i].Seq {
			t.Fatalf("reader skipped or reordered at %d: saw %d, stored %d", i, seen[i], all[i].Seq)
		}
		if i > 0 && all[i].Seq <= all[i-1].Seq {
			t.Fatalf("seq not strictly increasing at %d", i)
		}
	}
}
