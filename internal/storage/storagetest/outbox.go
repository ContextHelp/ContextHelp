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
	c := &outboxCase{drv: drv, ob: p.Outbox(), ctx: context.Background(), at: fixtureTime()}

	t.Run("empty outbox", c.outboxEmpty)
	t.Run("append assigns seq, uuidv7 id, stable instance id", c.outboxAppend)
	t.Run("since is strictly after the cursor and honours the limit", c.outboxSince)
	t.Run("mutations write their event in their own transaction", c.outboxMutations)
	t.Run("prune keeps the newest row, records the floor, refuses stale cursors", c.outboxPrune)
	t.Run("prune spares rows younger than the cutoff", c.outboxPruneSpares)
}

// outboxCase is the state the outbox subtests share: they run in order
// on one database.
type outboxCase struct {
	drv           storage.StorageDriver
	ob            storage.OutboxStore
	ctx           context.Context
	at            time.Time
	first, second storage.OutboxEvent
}

func (c *outboxCase) since(t *testing.T, after int64, limit int) []storage.OutboxRow {
	t.Helper()
	rows, err := c.ob.Since(c.ctx, after, limit)
	if err != nil {
		t.Fatalf("Since(%d): %v", after, err)
	}
	return rows
}

func (c *outboxCase) outboxEmpty(t *testing.T) {
	ctx, ob, drv, at, since := c.ctx, c.ob, c.drv, c.at, c.since
	_, _, _, _, _ = ctx, ob, drv, at, since
	if head, err := ob.Head(ctx); err != nil || head != 0 {
		t.Fatalf("Head = %d, %v; want 0", head, err)
	}
	if old, err := ob.Oldest(ctx); err != nil || old != 0 {
		t.Fatalf("Oldest = %d, %v; want 0", old, err)
	}
	if rows := since(t, 0, 0); len(rows) != 0 {
		t.Fatalf("Since(0) = %v, want none", rows)
	}

}

func (c *outboxCase) outboxAppend(t *testing.T) {
	ctx, ob, drv, at, since := c.ctx, c.ob, c.drv, c.at, c.since
	_, _, _, _, _ = ctx, ob, drv, at, since
	c.first = storage.OutboxEvent{Topic: "ctxt.runtime.object.updated", Source: "svc", Origin: "api",
		Actor: "p1", ObjectID: "o1", Payload: map[string]any{"object_id": "o1"}, OccurredAt: at}
	c.second = storage.OutboxEvent{Topic: "ctxt.runtime.object.deleted", Source: "svc", ObjectID: "o2",
		Payload: json.RawMessage(`{"object_id":"o2"}`), OccurredAt: at.Add(time.Second)}
	for _, ev := range []*storage.OutboxEvent{&c.first, &c.second} {
		if err := ob.Append(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	if c.first.Seq <= 0 || c.second.Seq <= c.first.Seq {
		t.Fatalf("seq %d then %d, want increasing", c.first.Seq, c.second.Seq)
	}
	id, err := uuid.Parse(c.first.ID)
	if err != nil || id.Version() != 7 {
		t.Fatalf("id %q is not a UUIDv7: %v", c.first.ID, err)
	}
	if c.first.InstanceID == "" || c.first.InstanceID != c.second.InstanceID {
		t.Fatalf("instance ids %q, %q, want one stable id", c.first.InstanceID, c.second.InstanceID)
	}
	rows := since(t, 0, 0)
	if len(rows) != 2 || rows[0].Seq != c.first.Seq || rows[1].Seq != c.second.Seq {
		t.Fatalf("rows %+v, want seqs %d, %d", rows, c.first.Seq, c.second.Seq)
	}
	c.checkFirstRow(t, rows[0])
}

// checkFirstRow pins that a stored row carries the event appended first.
func (c *outboxCase) checkFirstRow(t *testing.T, r storage.OutboxRow) {
	t.Helper()
	if r.ID != c.first.ID || r.Topic != c.first.Topic || r.Origin != "api" || r.Actor != "p1" ||
		r.ObjectID != "o1" || !r.OccurredAt.Equal(c.at) || r.InstanceID != c.first.InstanceID {
		t.Fatalf("row %+v does not carry the event %+v", r, c.first)
	}
	var pl map[string]string
	if err := json.Unmarshal(r.Payload, &pl); err != nil || pl["object_id"] != "o1" {
		t.Fatalf("payload %s: %v", r.Payload, err)
	}
}

func (c *outboxCase) outboxSince(t *testing.T) {
	ctx, ob, drv, at, since := c.ctx, c.ob, c.drv, c.at, c.since
	_, _, _, _, _ = ctx, ob, drv, at, since
	rows := since(t, c.first.Seq, 0)
	if len(rows) != 1 || rows[0].Seq != c.second.Seq {
		t.Fatalf("Since(first) = %+v, want only second", rows)
	}
	if rows := since(t, 0, 1); len(rows) != 1 || rows[0].Seq != c.first.Seq {
		t.Fatalf("Since(0, 1) = %+v, want only first", rows)
	}
	if rows := since(t, c.second.Seq, 0); len(rows) != 0 {
		t.Fatalf("Since(head) = %+v, want none", rows)
	}

}

func (c *outboxCase) outboxMutations(t *testing.T) {
	ctx, ob, drv, at, since := c.ctx, c.ob, c.drv, c.at, c.since
	_, _, _, _, _ = ctx, ob, drv, at, since
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

}

func (c *outboxCase) outboxPrune(t *testing.T) {
	ctx, ob, drv, at, since := c.ctx, c.ob, c.drv, c.at, c.since
	_, _, _, _, _ = ctx, ob, drv, at, since
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
	if _, err := ob.Since(ctx, c.first.Seq, 0); !errors.Is(err, storage.ErrOutboxExpired) {
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
		t.Fatalf("c.second Prune = %d, %v; want 0", n, err)
	}
	// New rows keep counting up: a pruned seq is never reused.
	next := storage.OutboxEvent{Topic: "ctxt.runtime.object.updated", Source: "svc", Payload: 1}
	if err := ob.Append(ctx, &next); err != nil || next.Seq <= head {
		t.Fatalf("Append after prune: seq %d, %v; want > %d", next.Seq, err, head)
	}

}

func (c *outboxCase) outboxPruneSpares(t *testing.T) {
	ctx, ob, drv, at, since := c.ctx, c.ob, c.drv, c.at, c.since
	_, _, _, _, _ = ctx, ob, drv, at, since
	a := storage.OutboxEvent{Topic: "ctxt.runtime.object.updated", Source: "svc", Payload: 1, OccurredAt: time.Now().Add(-48 * time.Hour)}
	b := storage.OutboxEvent{Topic: "ctxt.runtime.object.updated", Source: "svc", Payload: 2, OccurredAt: time.Now()}
	d := storage.OutboxEvent{Topic: "ctxt.runtime.object.updated", Source: "svc", Payload: 3, OccurredAt: time.Now()}
	for _, e := range []*storage.OutboxEvent{&a, &b, &d} {
		if err := ob.Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ob.Prune(ctx, time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows := since(t, a.Seq, 0)
	if len(rows) != 2 || rows[0].Seq != b.Seq || rows[1].Seq != d.Seq {
		t.Fatalf("rows after a = %+v, want b and c", rows)
	}

}

func mustHead(t *testing.T, ob storage.OutboxStore) int64 {
	t.Helper()
	h, err := ob.Head(context.Background())
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	return h
}
