package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestMigration013_ColumnsExist verifies the three new columns are present after init.
func TestMigration013_ColumnsExist(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	for _, col := range []string{"content_status", "version_hash", "registry_url"} {
		var name string
		err := d.db.QueryRowContext(ctx,
			"SELECT name FROM pragma_table_info('entities') WHERE name = ?", col,
		).Scan(&name)
		if err != nil {
			t.Fatalf("column %q not found after migration 013: %v", col, err)
		}
	}
}

// TestUpsertThin_StoresThinRecord verifies UpsertThin persists a thin entity.
func TestUpsertThin_StoresThinRecord(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	e := &storage.Entity{
		Slug:        "ai.transformer",
		Title:       "Transformer Architecture",
		Namespace:   "ai",
		VersionHash: "abc123",
		RegistryURL: "https://registry.example.com",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := d.Entities().UpsertThin(ctx, e); err != nil {
		t.Fatalf("UpsertThin: %v", err)
	}

	got, err := d.Entities().Get(ctx, "ai.transformer")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ContentStatus != storage.ContentStatusThin {
		t.Errorf("ContentStatus: got %q, want %q", got.ContentStatus, storage.ContentStatusThin)
	}
	if got.VersionHash != "abc123" {
		t.Errorf("VersionHash: got %q, want %q", got.VersionHash, "abc123")
	}
	if got.RegistryURL != "https://registry.example.com" {
		t.Errorf("RegistryURL: got %q", got.RegistryURL)
	}
	if got.Description != "" {
		t.Errorf("Description should be empty for thin entity, got %q", got.Description)
	}
}

// TestUpsertThin_DoesNotOverwriteFull verifies that a thin upsert does not clobber a full record.
func TestUpsertThin_DoesNotOverwriteFull(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)

	// Insert a full entity first.
	full := &storage.Entity{
		Slug:          "ai.transformer",
		Title:         "Transformer Architecture",
		Namespace:     "ai",
		Description:   "Full description here",
		ContentStatus: storage.ContentStatusFull,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := d.Entities().Upsert(ctx, full); err != nil {
		t.Fatalf("Upsert full: %v", err)
	}

	// Attempt a thin upsert — must not overwrite.
	thin := &storage.Entity{
		Slug:        "ai.transformer",
		Title:       "Transformer Architecture (thin)",
		Namespace:   "ai",
		VersionHash: "newHash",
		RegistryURL: "https://registry.example.com",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := d.Entities().UpsertThin(ctx, thin); err != nil {
		t.Fatalf("UpsertThin: %v", err)
	}

	got, err := d.Entities().Get(ctx, "ai.transformer")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Full record must still be intact.
	if got.ContentStatus != storage.ContentStatusFull {
		t.Errorf("ContentStatus: got %q, want %q", got.ContentStatus, storage.ContentStatusFull)
	}
	if got.Description != "Full description here" {
		t.Errorf("Description overwritten: got %q", got.Description)
	}
}

// TestSetContentStatus verifies the status transition helpers.
func TestSetContentStatus(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	e := &storage.Entity{
		Slug:        "api.auth",
		Title:       "Auth",
		Namespace:   "api",
		VersionHash: "v1",
		RegistryURL: "https://registry.example.com",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := d.Entities().UpsertThin(ctx, e); err != nil {
		t.Fatalf("UpsertThin: %v", err)
	}

	// Transition to pending_pull.
	if err := d.Entities().SetContentStatus(ctx, "api.auth", storage.ContentStatusPendingPull); err != nil {
		t.Fatalf("SetContentStatus pending_pull: %v", err)
	}
	got, _ := d.Entities().Get(ctx, "api.auth")
	if got.ContentStatus != storage.ContentStatusPendingPull {
		t.Errorf("ContentStatus: got %q, want %q", got.ContentStatus, storage.ContentStatusPendingPull)
	}

	// Transition to full.
	if err := d.Entities().SetContentStatus(ctx, "api.auth", storage.ContentStatusFull); err != nil {
		t.Fatalf("SetContentStatus full: %v", err)
	}
	got, _ = d.Entities().Get(ctx, "api.auth")
	if got.ContentStatus != storage.ContentStatusFull {
		t.Errorf("ContentStatus: got %q, want %q", got.ContentStatus, storage.ContentStatusFull)
	}
}

// TestEntityList_FilterByContentStatus verifies filtering entities by content_status.
func TestEntityList_FilterByContentStatus(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)

	thin1 := &storage.Entity{Slug: "ai.bert", Title: "BERT", Namespace: "ai",
		RegistryURL: "https://r.example.com", CreatedAt: now, UpdatedAt: now}
	thin2 := &storage.Entity{Slug: "ai.gpt", Title: "GPT", Namespace: "ai",
		RegistryURL: "https://r.example.com", CreatedAt: now, UpdatedAt: now}
	fullE := makeEntity("local.concept", "local")

	d.Entities().UpsertThin(ctx, thin1)
	d.Entities().UpsertThin(ctx, thin2)
	d.Entities().Upsert(ctx, fullE)

	thinList, err := d.Entities().List(ctx, storage.EntityFilter{ContentStatus: storage.ContentStatusThin})
	if err != nil {
		t.Fatalf("list thin: %v", err)
	}
	if len(thinList) != 2 {
		t.Errorf("thin count: got %d, want 2", len(thinList))
	}

	fullList, err := d.Entities().List(ctx, storage.EntityFilter{ContentStatus: storage.ContentStatusFull})
	if err != nil {
		t.Fatalf("list full: %v", err)
	}
	if len(fullList) != 1 {
		t.Errorf("full count: got %d, want 1", len(fullList))
	}
}
