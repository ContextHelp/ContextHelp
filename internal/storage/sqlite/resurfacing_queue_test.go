package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func setupResurfacingTest(t *testing.T) (*Driver, *ResurfacingQueueStore) {
	t.Helper()
	d := newTestDriver(t)
	return d, d.resurfacing
}

func newResurfacingEntry(id, objectID, profileID string, score float64) *storage.ResurfacingEntry {
	return &storage.ResurfacingEntry{
		ID:        id,
		ObjectID:  objectID,
		ProfileID: profileID,
		Score:     score,
		Reason:    "tag_overlap",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
}

func TestResurfacingQueue_UpsertAndList(t *testing.T) {
	d, s := setupResurfacingTest(t)
	ctx := context.Background()

	// Need a real object to satisfy the FK constraint.
	seedResurfacingObjects(t, d, "obj-1", "obj-2")

	e1 := newResurfacingEntry("e1", "obj-1", "research", 0.8)
	e2 := newResurfacingEntry("e2", "obj-2", "research", 0.6)

	if err := s.Upsert(ctx, e1); err != nil {
		t.Fatalf("upsert e1: %v", err)
	}
	if err := s.Upsert(ctx, e2); err != nil {
		t.Fatalf("upsert e2: %v", err)
	}

	entries, err := s.List(ctx, storage.ResurfacingFilter{ProfileID: "research"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	// Ordered by score DESC.
	if entries[0].Score < entries[1].Score {
		t.Errorf("want descending score; got %f, %f", entries[0].Score, entries[1].Score)
	}
}

func TestResurfacingQueue_Upsert_Updates(t *testing.T) {
	d, s := setupResurfacingTest(t)
	ctx := context.Background()

	seedResurfacingObjects(t, d, "obj-x")

	e := newResurfacingEntry("upd-1", "obj-x", "dev", 0.5)
	if err := s.Upsert(ctx, e); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Re-upsert with higher score.
	e.Score = 0.9
	e.Reason = "entity_overlap"
	if err := s.Upsert(ctx, e); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	entries, err := s.List(ctx, storage.ResurfacingFilter{ProfileID: "dev"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].Score != 0.9 {
		t.Errorf("want score 0.9, got %f", entries[0].Score)
	}
	if entries[0].Reason != "entity_overlap" {
		t.Errorf("want reason entity_overlap, got %q", entries[0].Reason)
	}
}

func TestResurfacingQueue_FilterUnseenOnly(t *testing.T) {
	d, s := setupResurfacingTest(t)
	ctx := context.Background()

	seedResurfacingObjects(t, d, "obj-a", "obj-b")

	e1 := newResurfacingEntry("f-1", "obj-a", "prof", 0.7)
	e2 := newResurfacingEntry("f-2", "obj-b", "prof", 0.7)

	if err := s.Upsert(ctx, e1); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(ctx, e2); err != nil {
		t.Fatal(err)
	}

	// Mark e1 as surfaced.
	if err := s.MarkSurfaced(ctx, "f-1", time.Now()); err != nil {
		t.Fatalf("mark surfaced: %v", err)
	}

	unseen, err := s.List(ctx, storage.ResurfacingFilter{ProfileID: "prof", UnseenOnly: true})
	if err != nil {
		t.Fatalf("list unseen: %v", err)
	}
	if len(unseen) != 1 {
		t.Fatalf("want 1 unseen, got %d", len(unseen))
	}
	if unseen[0].ID != "f-2" {
		t.Errorf("want unseen entry f-2, got %q", unseen[0].ID)
	}
}

func TestResurfacingQueue_Dismiss(t *testing.T) {
	d, s := setupResurfacingTest(t)
	ctx := context.Background()

	seedResurfacingObjects(t, d, "obj-z")

	e := newResurfacingEntry("d-1", "obj-z", "prof2", 0.6)
	if err := s.Upsert(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := s.Dismiss(ctx, "d-1", time.Now()); err != nil {
		t.Fatalf("dismiss: %v", err)
	}

	unseen, err := s.List(ctx, storage.ResurfacingFilter{ProfileID: "prof2", UnseenOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(unseen) != 0 {
		t.Errorf("want 0 unseen after dismiss, got %d", len(unseen))
	}
}

func TestResurfacingQueue_DeleteByProfile(t *testing.T) {
	d, s := setupResurfacingTest(t)
	ctx := context.Background()

	seedResurfacingObjects(t, d, "obj-p1")

	e := newResurfacingEntry("dp-1", "obj-p1", "delete-me", 0.5)
	if err := s.Upsert(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteByProfile(ctx, "delete-me"); err != nil {
		t.Fatalf("delete by profile: %v", err)
	}

	entries, err := s.List(ctx, storage.ResurfacingFilter{ProfileID: "delete-me"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("want 0 entries after delete, got %d", len(entries))
	}
}

func TestResurfacingQueue_MinScore(t *testing.T) {
	d, s := setupResurfacingTest(t)
	ctx := context.Background()

	seedResurfacingObjects(t, d, "obj-s1", "obj-s2")

	if err := s.Upsert(ctx, newResurfacingEntry("ms-1", "obj-s1", "scored", 0.8)); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(ctx, newResurfacingEntry("ms-2", "obj-s2", "scored", 0.3)); err != nil {
		t.Fatal(err)
	}

	entries, err := s.List(ctx, storage.ResurfacingFilter{ProfileID: "scored", MinScore: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry above min_score=0.5, got %d", len(entries))
	}
	if entries[0].Score < 0.5 {
		t.Errorf("want score >= 0.5, got %f", entries[0].Score)
	}
}

// seedResurfacingObjects creates minimal objects so FK constraints are satisfied.
func seedResurfacingObjects(t *testing.T, d *Driver, ids ...string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range ids {
		obj := &storage.KnowledgeObject{
			ID:        id,
			Type:      "text",
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := d.objects.Create(ctx, obj); err != nil {
			t.Fatalf("seed object %s: %v", id, err)
		}
	}
}
