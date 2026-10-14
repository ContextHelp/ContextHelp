package storagetest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ChangeOrderConformance pins ObjectFilter.ChangedAfter and ChangedUntil:
// objects come back in change order (updated_at as an instant, then id
// byte-wise) strictly after the cursor, whatever UTC offset they were
// stamped in, and paging by the last object's cursor visits every object
// once — including ties on updated_at split across pages.
func ChangeOrderConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	edt := time.FixedZone("EDT", -4*3600)
	base := fixtureTime()
	// Stamped in EDT, t1 is the latest instant; a text compare would put
	// it first. "B" sorts before "a" byte-wise but after it in most
	// linguistic collations.
	seed := []struct {
		id string
		at time.Time
	}{
		{"co-z", base},
		{"co-a", base.Add(time.Second)},
		{"co-B", base.Add(time.Second)},
		{"co-c", base.Add(time.Second)},
		{"co-t1", base.Add(time.Hour).In(edt)},
	}
	for _, o := range seed {
		if err := drv.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: o.id, Type: "note", RawContent: o.id, Status: "inbox",
			CreatedAt: o.at, UpdatedAt: o.at,
		}); err != nil {
			t.Fatalf("Create %s: %v", o.id, err)
		}
	}
	all := []string{"co-z", "co-B", "co-a", "co-c", "co-t1"}

	list := func(t *testing.T, f storage.ObjectFilter) []string {
		t.Helper()
		f.Status = "all"
		objs, _, err := drv.Objects().List(ctx, f)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		ids := make([]string, 0, len(objs))
		for _, o := range objs {
			ids = append(ids, o.ID)
		}
		return ids
	}

	t.Run("zero cursor lists every object in change order", func(t *testing.T) {
		if got := list(t, storage.ObjectFilter{ChangedAfter: &storage.ChangeCursor{}}); !slices.Equal(got, all) {
			t.Errorf("got %v, want %v", got, all)
		}
	})
	t.Run("after a tie keeps the rest of the tie", func(t *testing.T) {
		c := storage.ChangeCursor{UpdatedAt: base.Add(time.Second).In(edt), ID: "co-B"}
		want := []string{"co-a", "co-c", "co-t1"}
		if got := list(t, storage.ObjectFilter{ChangedAfter: &c}); !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("until drops later changes", func(t *testing.T) {
		until := base.Add(time.Minute)
		want := all[:4]
		got := list(t, storage.ObjectFilter{ChangedAfter: &storage.ChangeCursor{}, ChangedUntil: &until})
		if !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("paging by cursor visits each object once", func(t *testing.T) {
		var got []string
		cur := storage.ChangeCursor{}
		for range len(all) + 1 {
			objs, _, err := drv.Objects().List(ctx, storage.ObjectFilter{
				Status: "all", ChangedAfter: &cur, Limit: 2,
			})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(objs) == 0 {
				break
			}
			for _, o := range objs {
				got = append(got, o.ID)
			}
			cur = storage.ChangeCursorOf(objs[len(objs)-1])
		}
		if !slices.Equal(got, all) {
			t.Errorf("paged %v, want %v", got, all)
		}
	})
}

// WatermarkConformance pins WatermarkStore: a missing row reads as the
// zero position, and a stored position round-trips per federation name.
func WatermarkConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	wm := drv.Watermarks()

	got, err := wm.GetWatermark(ctx, "wm-missing")
	if err != nil {
		t.Fatalf("GetWatermark missing: %v", err)
	}
	if !got.UpdatedAt.IsZero() || got.ID != "" {
		t.Errorf("missing watermark = %+v, want zero", got)
	}

	a := storage.ChangeCursor{UpdatedAt: fixtureTime(), ID: "obj-a"}
	b := storage.ChangeCursor{UpdatedAt: fixtureTime().Add(time.Hour), ID: "obj-b"}
	for name, pos := range map[string]storage.ChangeCursor{"wm-a": a, "wm-b": b} {
		if err := wm.SetWatermark(ctx, name, pos); err != nil {
			t.Fatalf("SetWatermark %s: %v", name, err)
		}
	}
	moved := storage.ChangeCursor{UpdatedAt: b.UpdatedAt.Add(time.Minute), ID: "obj-c"}
	if err := wm.SetWatermark(ctx, "wm-b", moved); err != nil {
		t.Fatalf("SetWatermark wm-b again: %v", err)
	}
	for name, want := range map[string]storage.ChangeCursor{"wm-a": a, "wm-b": moved} {
		got, err := wm.GetWatermark(ctx, name)
		if err != nil {
			t.Fatalf("GetWatermark %s: %v", name, err)
		}
		if !got.UpdatedAt.Equal(want.UpdatedAt) || got.ID != want.ID {
			t.Errorf("%s = %+v, want %+v", name, got, want)
		}
	}
}
