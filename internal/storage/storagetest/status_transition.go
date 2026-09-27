package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// StatusTransitionConformance pins ObjectStore.TransitionStatus: the
// status moves, and updated_at with it, only when the object is in the
// expected status; a missing object or one in any other status is
// storage.ErrNotFound and leaves the row untouched.
func StatusTransitionConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	created := fixtureTime()
	moved := created.Add(time.Hour)
	for _, o := range []struct{ id, status string }{
		{"st-inbox", "inbox"},
		{"st-active", "active"},
		{"st-discarded", "discarded"},
	} {
		if err := drv.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: o.id, Type: "note", RawContent: o.id, Status: o.status,
			CreatedAt: created, UpdatedAt: created,
		}); err != nil {
			t.Fatalf("Create %s: %v", o.id, err)
		}
	}
	check := func(t *testing.T, id, wantStatus string, wantUpdated time.Time) {
		t.Helper()
		got, err := drv.Objects().Get(ctx, id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if got.Status != wantStatus {
			t.Errorf("%s status = %q, want %q", id, got.Status, wantStatus)
		}
		if !got.UpdatedAt.Equal(wantUpdated) {
			t.Errorf("%s updated_at = %s, want %s", id, got.UpdatedAt, wantUpdated)
		}
	}

	t.Run("moves from the expected status", func(t *testing.T) {
		if err := drv.Objects().TransitionStatus(ctx, "st-inbox", "inbox", "active", moved); err != nil {
			t.Fatalf("TransitionStatus: %v", err)
		}
		check(t, "st-inbox", "active", moved)
	})
	t.Run("refuses any other status", func(t *testing.T) {
		for id, status := range map[string]string{"st-active": "active", "st-discarded": "discarded"} {
			err := drv.Objects().TransitionStatus(ctx, id, "inbox", "discarded", moved)
			if !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("%s: err = %v, want storage.ErrNotFound", id, err)
			}
			check(t, id, status, created)
		}
	})
	t.Run("refuses a missing object", func(t *testing.T) {
		err := drv.Objects().TransitionStatus(ctx, "st-missing", "inbox", "active", moved)
		if !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("err = %v, want storage.ErrNotFound", err)
		}
	})
}
