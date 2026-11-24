package storagetest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// RunGovernanceConformance exercises HoldStore, TombstoneStore, and
// ObligationStore across SQLite and Postgres drivers.
func RunGovernanceConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	t.Run("holds", func(t *testing.T) { testHoldStore(t, drv) })
	t.Run("tombstones", func(t *testing.T) { testTombstoneStore(t, drv) })
	t.Run("obligations", func(t *testing.T) { testObligationStore(t, drv) })
}

func testHoldStore(t *testing.T, drv storage.StorageDriver) {
	holds := drv.Holds()
	if holds == nil {
		t.Fatal("Holds() returned nil")
	}
	objID := "obj-hold-conf-1"
	srcKey := "bos://core.documents/documents_01h455vb4pex5vsknk084sn02q"
	t0 := time.Date(2026, 11, 24, 1, 0, 0, 0, time.UTC)

	seedStackedHolds(t, holds, objID, srcKey, t0)
	verifyStackedHoldRelease(t, holds, objID, t0)
}

func seedStackedHolds(t *testing.T, holds storage.HoldStore, objID, srcKey string, t0 time.Time) {
	t.Helper()
	ctx := context.Background()

	if held, err := holds.IsHeld(ctx, objID); err != nil || held {
		t.Fatalf("fresh object IsHeld = (%v, %v), want (false, nil)", held, err)
	}
	for _, h := range []*storage.ObjectHold{
		{ObjectID: objID, SourceKey: srcKey, HoldID: "legal_holds_1", Source: "bos", Reason: "audit freeze", PlacedBy: "authority:bos", PlacedAt: t0},
		// Idempotent duplicate on same (object_id, hold_id).
		{ObjectID: objID, SourceKey: srcKey, HoldID: "legal_holds_1", Source: "bos", PlacedBy: "authority:bos", PlacedAt: t0.Add(time.Minute)},
		// Second concurrent hold stacks on same object.
		{ObjectID: objID, SourceKey: srcKey, HoldID: "legal_holds_2", Source: "bos", Reason: "dispute", PlacedBy: "authority:bos", PlacedAt: t0.Add(2 * time.Minute)},
	} {
		if err := holds.Place(ctx, h); err != nil {
			t.Fatalf("Place %s: %v", h.HoldID, err)
		}
	}

	active, err := holds.ListActive(ctx, objID)
	if err != nil || len(active) != 2 {
		t.Fatalf("ListActive = (%d, %v), want 2", len(active), err)
	}
	byKey, err := holds.ListActiveBySourceKey(ctx, srcKey)
	if err != nil || len(byKey) != 2 {
		t.Fatalf("ListActiveBySourceKey = (%d, %v), want 2", len(byKey), err)
	}
	he := &storage.HoldError{ObjectID: objID, Holds: active}
	if !errors.Is(he, storage.ErrObjectHeld) || !strings.Contains(he.Error(), "legal_holds_1") || !strings.Contains(he.Error(), "bos") {
		t.Fatalf("HoldError formatting/wrap wrong: %q", he.Error())
	}
}

func verifyStackedHoldRelease(t *testing.T, holds storage.HoldStore, objID string, t0 time.Time) {
	t.Helper()
	ctx := context.Background()

	// Release first hold: object MUST stay held by second hold.
	ok, err := holds.Release(ctx, objID, "legal_holds_1", "authority:bos", "audit closed", t0.Add(3*time.Minute))
	if err != nil || !ok {
		t.Fatalf("Release h1 = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := holds.Release(ctx, objID, "legal_holds_1", "authority:bos", "repeat", t0.Add(4*time.Minute)); err != nil || ok {
		t.Fatalf("repeat Release h1 = (%v, %v), want (false, nil)", ok, err)
	}
	if held, err := holds.IsHeld(ctx, objID); err != nil || !held {
		t.Fatalf("after releasing 1 of 2 holds: IsHeld = (%v, %v), want (true, nil)", held, err)
	}

	filtered, err := holds.ListAllActive(ctx, storage.HoldFilter{HoldID: "legal_holds_2", Source: "bos"})
	if err != nil || len(filtered) != 1 || filtered[0].HoldID != "legal_holds_2" {
		t.Fatalf("ListAllActive filter = (%+v, %v), want 1 match", filtered, err)
	}

	// Release second hold: object is now unheld.
	if ok, err := holds.Release(ctx, objID, "legal_holds_2", "authority:bos", "dispute settled", t0.Add(5*time.Minute)); err != nil || !ok {
		t.Fatalf("Release h2 = (%v, %v), want (true, nil)", ok, err)
	}
	if held, err := holds.IsHeld(ctx, objID); err != nil || held {
		t.Fatalf("after releasing all holds: IsHeld = (%v, %v), want (false, nil)", held, err)
	}
}

func testTombstoneStore(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	ts := drv.Tombstones()
	if ts == nil {
		t.Fatal("Tombstones() returned nil")
	}

	key := "bos://core.documents/documents_erased_1"
	if exists, err := ts.Exists(ctx, key); err != nil || exists {
		t.Fatalf("fresh Exists = (%v, %v), want (false, nil)", exists, err)
	}
	t0 := time.Date(2026, 11, 24, 2, 0, 0, 0, time.UTC)
	if err := ts.Put(ctx, &storage.ErasureTombstone{
		SourceKey:    key,
		Source:       "bos",
		ObligationID: "ob-erase-1",
		ErasedAt:     t0,
		ErasedBy:     "authority:bos",
	}); err != nil {
		t.Fatalf("Put tombstone: %v", err)
	}
	// Idempotent put.
	if err := ts.Put(ctx, &storage.ErasureTombstone{SourceKey: key, Source: "bos", ErasedAt: t0.Add(time.Minute)}); err != nil {
		t.Fatalf("idempotent Put tombstone: %v", err)
	}
	if exists, err := ts.Exists(ctx, key); err != nil || !exists {
		t.Fatalf("Exists after Put = (%v, %v), want (true, nil)", exists, err)
	}
	got, err := ts.Get(ctx, key)
	if err != nil || got.ObligationID != "ob-erase-1" {
		t.Fatalf("Get tombstone = (%+v, %v)", got, err)
	}
	if _, err := ts.Get(ctx, "bos://core.documents/missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("missing tombstone err = %v, want ErrNotFound", err)
	}
	list, err := ts.List(ctx, 10, 0)
	if err != nil || len(list) == 0 {
		t.Fatalf("List tombstones = (%d, %v)", len(list), err)
	}
}

func testObligationStore(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	obs := drv.Obligations()
	if obs == nil {
		t.Fatal("Obligations() returned nil")
	}

	if _, err := obs.Get(ctx, "ob-missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("missing obligation err = %v, want ErrNotFound", err)
	}

	rec := &storage.ObligationRecord{
		ID:            "peer_obligation_1",
		Kind:          "hold",
		RequestSHA256: "sha256:aaa",
		State:         "applied",
		StatusJSON:    `{"id":"peer_obligation_1","state":"applied"}`,
	}
	if err := obs.Save(ctx, rec); err != nil {
		t.Fatalf("Save obligation: %v", err)
	}
	got, err := obs.Get(ctx, rec.ID)
	if err != nil || got.State != "applied" || got.RequestSHA256 != "sha256:aaa" {
		t.Fatalf("Get obligation = (%+v, %v)", got, err)
	}
	// Same id + same request hash updates cleanly.
	rec.StatusJSON = `{"id":"peer_obligation_1","state":"applied","v":2}`
	if err := obs.Save(ctx, rec); err != nil {
		t.Fatalf("idempotent Save obligation: %v", err)
	}
	// Same id + DIFFERENT request hash returns ErrObligationIDReused.
	dup := &storage.ObligationRecord{
		ID:            rec.ID,
		Kind:          "hold",
		RequestSHA256: "sha256:bbb",
		State:         "applied",
		StatusJSON:    `{}`,
	}
	if err := obs.Save(ctx, dup); !errors.Is(err, storage.ErrObligationIDReused) {
		t.Fatalf("Save with different hash err = %v, want ErrObligationIDReused", err)
	}
}
