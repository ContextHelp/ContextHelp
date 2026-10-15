package storagetest

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ProximityDeleteConformance pins that deleting an object removes every
// object_proximity pair it is part of, on either side of the pair, and
// leaves the pairs of the objects that remain.
func ProximityDeleteConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	for _, id := range []string{"prox-a", "prox-b", "prox-c"} {
		if err := drv.Objects().Create(ctx, searchFixtureObject(id, "note", "proximity fixture "+id)); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	ps := drv.Proximity()
	// prox-b sits on the object_b side of (a,b) and the object_a side of (b,c).
	for _, pair := range [][2]string{{"prox-a", "prox-b"}, {"prox-b", "prox-c"}, {"prox-a", "prox-c"}} {
		if err := ps.Put(ctx, &storage.ProximityScore{
			ObjectA: pair[0], ObjectB: pair[1], Score: 0.5, ComputedAt: fixtureTime(),
		}); err != nil {
			t.Fatalf("put %v: %v", pair, err)
		}
	}

	if err := drv.Objects().Delete(ctx, "prox-b"); err != nil {
		t.Fatalf("delete prox-b: %v", err)
	}

	for _, pair := range [][2]string{{"prox-a", "prox-b"}, {"prox-b", "prox-c"}} {
		got, err := ps.Get(ctx, pair[0], pair[1])
		if err != nil {
			t.Fatalf("get %v: %v", pair, err)
		}
		if got != nil {
			t.Errorf("pair %v survived the delete of prox-b", pair)
		}
	}
	got, err := ps.Get(ctx, "prox-a", "prox-c")
	if err != nil || got == nil {
		t.Errorf("pair (prox-a, prox-c) = %v, %v; want it kept", got, err)
	}
}
