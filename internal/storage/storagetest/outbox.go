package storagetest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// OutboxProvider is the optional driver surface OutboxConformance needs.
type OutboxProvider interface {
	Outbox() storage.OutboxStore
}

// OutboxConformance pins the event outbox (ADR-075 §4): rows carry a
// monotonic seq, a UUIDv7 id and a stable instance id; a mutation writes
// the event attached to its context in its own transaction and nothing
// when it fails; Since replays in seq order and refuses a cursor behind
// the retention floor; Prune keeps the newest row and Head survives it.
func OutboxConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	p, ok := drv.(OutboxProvider)
	if !ok {
		t.Fatalf("driver %T has no Outbox()", drv)
	}
	ob := p.Outbox()
	ctx := context.Background()
	at := fixtureTime()

	since := func(t *testing.T, after int64, limit int) []storage.OutboxRow {
		t.Helper()
		rows, err := ob.Since(ctx, after, limit)
		if err != nil {
			t.Fatalf("Since(%d): %v", after, err)
		}
		return rows
	}

	t.Run("empty outbox", func(t *testing.T) {
		if head, err := ob.Head(ctx); err != nil || head != 0 {
			t.Fatalf("Head = %d, %v; want 0", head, err)
		}
		if old, err := ob.Oldest(ctx); err != nil || old != 0 {
			t.Fatalf("Oldest = %d, %v; want 0", old, err)
		}
		if rows := since(t, 0, 0); len(rows) != 0 {
			t.Fatalf("Since(0) = %v, want none", rows)
		}
	})

	var first, second storage.OutboxEvent
	t.Run("append assigns seq, uuidv7 id, stable instance id", func(t *testing.T) {
		first = storage.OutboxEvent{Topic: "ctxt.runtime.object.updated", Source: "svc", Origin: "api",
			Actor: "p1", ObjectID: "o1", Payload: map[string]any{"object_id": "o1"}, OccurredAt: at}
		second = storage.OutboxEvent{Topic: "ctxt.runtime.object.deleted", Source: "svc", ObjectID: "o2",
			Payload: json.RawMessage(`{"object_id":"o2"}`), OccurredAt: at.Add(time.Second)}
		if err := ob.Append(ctx, &first); err != nil {
			t.Fatal(err)
		}
		if err := ob.Append(ctx, &second); err != nil {
			t.Fatal(err)
		}
		if first.Seq <= 0 || second.Seq <= first.Seq {
			t.Fatalf("seq %d then %d, want increasing", first.Seq, second.Seq)
		}
		id, err := uuid.Parse(first.ID)
		if err != nil || id.Version() != 7 {
			t.Fatalf("id %q is not a UUIDv7: %v", first.ID, err)
		}
		if first.InstanceID == "" || first.InstanceID != second.InstanceID {
			t.Fatalf("instance ids %q, %q, want one stable id", first.InstanceID, second.InstanceID)
		}
		rows := since(t, 0, 0)
		if len(rows) != 2 || rows[0].Seq != first.Seq || rows[1].Seq != second.Seq {
			t.Fatalf("rows %+v, want seqs %d, %d", rows, first.Seq, second.Seq)
		}
		r := rows[0]
		if r.ID != first.ID || r.Topic != first.Topic || r.Origin != "api" || r.Actor != "p1" ||
			r.ObjectID != "o1" || !r.OccurredAt.Equal(at) || r.InstanceID != first.InstanceID {
			t.Fatalf("row %+v does not carry the event %+v", r, first)
		}
		var pl map[string]string
		if err := json.Unmarshal(r.Payload, &pl); err != nil || pl["object_id"] != "o1" {
			t.Fatalf("payload %s: %v", r.Payload, err)
		}
	})

	t.Run("since is strictly after the cursor and honours the limit", func(t *testing.T) {
		rows := since(t, first.Seq, 0)
		if len(rows) != 1 || rows[0].Seq != second.Seq {
			t.Fatalf("Since(first) = %+v, want only second", rows)
		}
		if rows := since(t, 0, 1); len(rows) != 1 || rows[0].Seq != first.Seq {
			t.Fatalf("Since(0, 1) = %+v, want only first", rows)
		}
		if rows := since(t, second.Seq, 0); len(rows) != 0 {
			t.Fatalf("Since(head) = %+v, want none", rows)
		}
	})

	t.Run("mutations write their event in their own transaction", func(t *testing.T) {
		objs := drv.Objects()
		base := mustHead(t, ob)
		attach := func(topic, id string) (context.Context, *storage.OutboxEvent) {
			ev := &storage.OutboxEvent{Topic: topic, Source: "svc", ObjectID: id, Payload: map[string]string{"object_id": id}}
			return storage.WithOutboxEvent(ctx, ev), ev
		}
		obj := &storage.KnowledgeObject{ID: "ob-1", Type: "note", RawContent: "x", Status: "active", CreatedAt: at, UpdatedAt: at}

		cctx, cev := attach("ctxt.runtime.object.ingested", "ob-1")
		if err := objs.Create(cctx, obj); err != nil {
			t.Fatal(err)
		}
		if cev.Seq <= base {
			t.Fatalf("Create left seq %d, want > %d", cev.Seq, base)
		}

		// A create that fails writes nothing.
		dctx, dev := attach("ctxt.runtime.object.ingested", "ob-1")
		dup := *obj
		if err := objs.Create(dctx, &dup); !errors.Is(err, storage.ErrAlreadyExists) {
			t.Fatalf("duplicate Create = %v, want ErrAlreadyExists", err)
		}
		if dev.Seq != 0 || mustHead(t, ob) != cev.Seq {
			t.Fatalf("failed Create wrote a row (event seq %d, head %d)", dev.Seq, mustHead(t, ob))
		}

		obj.RawContent = "y"
		uctx, uev := attach("ctxt.runtime.object.updated", "ob-1")
		if err := objs.Update(uctx, obj); err != nil {
			t.Fatal(err)
		}
		if uev.Seq <= cev.Seq {
			t.Fatalf("Update seq %d, want > %d", uev.Seq, cev.Seq)
		}

		// An update of a missing object writes nothing.
		mctx, mev := attach("ctxt.runtime.object.updated", "ob-missing")
		if err := objs.Update(mctx, &storage.KnowledgeObject{ID: "ob-missing", Type: "note", Status: "active", CreatedAt: at, UpdatedAt: at}); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("Update missing = %v, want ErrNotFound", err)
		}
		if mev.Seq != 0 || mustHead(t, ob) != uev.Seq {
			t.Fatalf("failed Update wrote a row")
		}

		xctx, xev := attach("ctxt.runtime.object.deleted", "ob-1")
		if err := objs.Delete(xctx, "ob-1"); err != nil {
			t.Fatal(err)
		}
		if xev.Seq <= uev.Seq {
			t.Fatalf("Delete seq %d, want > %d", xev.Seq, uev.Seq)
		}
		nctx, nev := attach("ctxt.runtime.object.deleted", "ob-1")
		if err := objs.Delete(nctx, "ob-1"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("Delete missing = %v, want ErrNotFound", err)
		}
		if nev.Seq != 0 || mustHead(t, ob) != xev.Seq {
			t.Fatalf("failed Delete wrote a row")
		}

		// A mutation with no event attached writes no row.
		plain := &storage.KnowledgeObject{ID: "ob-2", Type: "note", RawContent: "z", Status: "active", CreatedAt: at, UpdatedAt: at}
		if err := objs.Create(ctx, plain); err != nil {
			t.Fatal(err)
		}
		if mustHead(t, ob) != xev.Seq {
			t.Fatalf("Create without an event wrote a row")
		}
	})

	t.Run("prune keeps the newest row, records the floor, refuses stale cursors", func(t *testing.T) {
		head := mustHead(t, ob)
		n, err := ob.Prune(ctx, time.Now().Add(time.Hour)) // everything is older
		if err != nil || n <= 0 {
			t.Fatalf("Prune = %d, %v; want some rows", n, err)
		}
		rows := since(t, head-1, 0)
		if len(rows) != 1 || rows[0].Seq != head {
			t.Fatalf("after prune, rows from head-1 = %+v, want only head %d", rows, head)
		}
		if got := mustHead(t, ob); got != head {
			t.Fatalf("Head after prune = %d, want %d", got, head)
		}
		if old, _ := ob.Oldest(ctx); old != head {
			t.Fatalf("Oldest = %d, want %d", old, head)
		}
		if _, err := ob.Since(ctx, first.Seq, 0); !errors.Is(err, storage.ErrOutboxExpired) {
			t.Fatalf("Since(pruned cursor) = %v, want ErrOutboxExpired", err)
		} else {
			var ee *storage.OutboxExpiredError
			if !errors.As(err, &ee) || ee.Oldest != head {
				t.Fatalf("expired error %v, want Oldest %d", err, head)
			}
		}
		if _, err := ob.Since(ctx, head, 0); err != nil {
			t.Fatalf("Since(head) after prune: %v", err)
		}
		if n, err := ob.Prune(ctx, time.Now().Add(time.Hour)); err != nil || n != 0 {
			t.Fatalf("second Prune = %d, %v; want 0", n, err)
		}
		// New rows keep counting up: a pruned seq is never reused.
		next := storage.OutboxEvent{Topic: "ctxt.runtime.object.updated", Source: "svc", Payload: 1}
		if err := ob.Append(ctx, &next); err != nil || next.Seq <= head {
			t.Fatalf("Append after prune: seq %d, %v; want > %d", next.Seq, err, head)
		}
	})

	t.Run("prune spares rows younger than the cutoff", func(t *testing.T) {
		a := storage.OutboxEvent{Topic: "ctxt.runtime.object.updated", Source: "svc", Payload: 1, OccurredAt: time.Now().Add(-48 * time.Hour)}
		b := storage.OutboxEvent{Topic: "ctxt.runtime.object.updated", Source: "svc", Payload: 2, OccurredAt: time.Now()}
		c := storage.OutboxEvent{Topic: "ctxt.runtime.object.updated", Source: "svc", Payload: 3, OccurredAt: time.Now()}
		for _, e := range []*storage.OutboxEvent{&a, &b, &c} {
			if err := ob.Append(ctx, e); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := ob.Prune(ctx, time.Now().Add(-24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		rows := since(t, a.Seq, 0)
		if len(rows) != 2 || rows[0].Seq != b.Seq || rows[1].Seq != c.Seq {
			t.Fatalf("rows after a = %+v, want b and c", rows)
		}
	})
}

func mustHead(t *testing.T, ob storage.OutboxStore) int64 {
	t.Helper()
	h, err := ob.Head(context.Background())
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	return h
}
