package storagetest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ChangeOrderConformance pins KnowledgeObject.ChangeSeq and
// ObjectFilter.ChangedAfter: the store stamps ChangeSeq on every insert
// and every update, in write order, whatever updated_at or ChangeSeq the
// caller passed; ChangedAfter lists objects strictly after a ChangeSeq in
// that order; and paging by the last object's ChangeSeq visits every
// object once.
func ChangeOrderConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	objs := drv.Objects()
	base := fixtureTime()
	seeded := seedChangeOrder(t, objs, base)
	written := []string{"co-z", "co-a", "co-imported", "co-b"}

	list := func(t *testing.T, after int64, limit int) []*storage.KnowledgeObject {
		t.Helper()
		got, _, err := objs.List(ctx, storage.ObjectFilter{Status: "all", ChangedAfter: &after, Limit: limit})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		return got
	}
	ids := objectIDs
	seqOf := func(t *testing.T, id string) int64 {
		t.Helper()
		o, err := objs.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		return o.ChangeSeq
	}

	t.Run("inserts are stamped in write order, caller values ignored", func(t *testing.T) {
		all := list(t, 0, 0)
		if got := ids(all); !slices.Equal(got, written) {
			t.Fatalf("change order %v, want write order %v", got, written)
		}
		for i, o := range all {
			if o.ChangeSeq <= 0 || o.ChangeSeq >= 1000-int64(seeded) {
				t.Errorf("%s ChangeSeq = %d: want a store-stamped value, not the caller's", o.ID, o.ChangeSeq)
			}
			if i > 0 && o.ChangeSeq <= all[i-1].ChangeSeq {
				t.Errorf("%s ChangeSeq %d not after %s's %d", o.ID, o.ChangeSeq, all[i-1].ID, all[i-1].ChangeSeq)
			}
			if got := seqOf(t, o.ID); got != o.ChangeSeq {
				t.Errorf("Get(%s).ChangeSeq = %d, List says %d", o.ID, got, o.ChangeSeq)
			}
		}
	})

	t.Run("after a position keeps only later writes", func(t *testing.T) {
		after := seqOf(t, "co-a")
		if got, want := ids(list(t, after, 0)), written[2:]; !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("paging by ChangeSeq visits each object once", func(t *testing.T) {
		var got []string
		var cur int64
		for range len(written) + 1 {
			page := list(t, cur, 3)
			if len(page) == 0 {
				break
			}
			got = append(got, ids(page)...)
			cur = page[len(page)-1].ChangeSeq
		}
		if !slices.Equal(got, written) {
			t.Errorf("paged %v, want %v", got, written)
		}
	})

	t.Run("an update moves the object last, whatever its updated_at", func(t *testing.T) {
		last := seqOf(t, "co-b")
		o, err := objs.Get(ctx, "co-z")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		o.RawContent = "edited"
		o.UpdatedAt = time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
		o.ChangeSeq = 1
		if err := objs.Update(ctx, o); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got := ids(list(t, last, 0)); !slices.Equal(got, []string{"co-z"}) {
			t.Errorf("changed after %d = %v, want [co-z]", last, got)
		}

		last = seqOf(t, "co-z")
		if err := objs.TransitionStatus(ctx, "co-a", "inbox", "active", base); err != nil {
			t.Fatalf("TransitionStatus: %v", err)
		}
		if got := ids(list(t, last, 0)); !slices.Equal(got, []string{"co-a"}) {
			t.Errorf("changed after %d = %v, want [co-a]", last, got)
		}
	})
}

// WatermarkConformance pins WatermarkStore: a missing row reads as 0,
// and a stored ChangeSeq round-trips per federation name.
func WatermarkConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	wm := drv.Watermarks()

	got, err := wm.GetWatermark(ctx, "wm-missing")
	if err != nil {
		t.Fatalf("GetWatermark missing: %v", err)
	}
	if got != 0 {
		t.Errorf("missing watermark = %d, want 0", got)
	}

	for name, seq := range map[string]int64{"wm-a": 7, "wm-b": 42} {
		if err := wm.SetWatermark(ctx, name, seq); err != nil {
			t.Fatalf("SetWatermark %s: %v", name, err)
		}
	}
	if err := wm.SetWatermark(ctx, "wm-b", 1<<40); err != nil {
		t.Fatalf("SetWatermark wm-b again: %v", err)
	}
	for name, want := range map[string]int64{"wm-a": 7, "wm-b": 1 << 40} {
		got, err := wm.GetWatermark(ctx, name)
		if err != nil {
			t.Fatalf("GetWatermark %s: %v", name, err)
		}
		if got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
}

// seedChangeOrder creates co-z, co-a, co-imported and co-b, in that order.
// updated_at runs backwards from base, as an import or a federated row from
// a slower clock carries; the caller also passes ChangeSeq values (1000
// down) the store must not keep. It returns how many objects it wrote.
func seedChangeOrder(t *testing.T, objs storage.ObjectStore, base time.Time) int {
	t.Helper()
	seed := []struct {
		id string
		at time.Time
	}{
		{"co-z", base},
		{"co-a", base.Add(-time.Hour)},
		{"co-imported", time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"co-b", base.Add(-2 * time.Hour)},
	}
	for i, o := range seed {
		if err := objs.Create(context.Background(), &storage.KnowledgeObject{
			ID: o.id, Type: "note", RawContent: o.id, Status: "inbox",
			CreatedAt: o.at, UpdatedAt: o.at, ChangeSeq: int64(1000 - i),
		}); err != nil {
			t.Fatalf("Create %s: %v", o.id, err)
		}
	}
	return len(seed)
}

// objectIDs lists the IDs of objs in order.
func objectIDs(objs []*storage.KnowledgeObject) []string {
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.ID)
	}
	return out
}
