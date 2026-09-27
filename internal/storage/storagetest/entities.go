package storagetest

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ThinEntityConformance pins the thin-entity round trip every driver must
// honour: a row stored by UpsertThin (registry thin sync, mentions) reads
// back through Get, List and Resolve with empty metadata, and a later full
// Upsert over it replaces it. A thin row whose stored metadata is not a JSON
// object fails every entity read, so any search graph that mentions it fails.
func ThinEntityConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	entities := drv.Entities()

	thin := &storage.Entity{
		Slug:        "thin.mentioned",
		Title:       "Mentioned",
		Namespace:   "thin",
		Aliases:     []string{"thin-alias"},
		VersionHash: "v1",
		RegistryURL: "https://registry.example/thin",
		CreatedAt:   fixtureTime(),
		UpdatedAt:   fixtureTime(),
	}
	if err := entities.UpsertThin(ctx, thin); err != nil {
		t.Fatalf("UpsertThin: %v", err)
	}

	t.Run("Get", func(t *testing.T) {
		got, err := entities.Get(ctx, thin.Slug)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		assertThinEntity(t, got)
	})

	t.Run("List", func(t *testing.T) {
		got, err := entities.List(ctx, storage.EntityFilter{ContentStatus: storage.ContentStatusThin})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("List(thin) returned %d entities, want 1", len(got))
		}
		assertThinEntity(t, got[0])
	})

	t.Run("ResolveByAlias", func(t *testing.T) {
		got, err := entities.Resolve(ctx, "thin-alias")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		assertThinEntity(t, got)
	})

	t.Run("FullUpsertOverThin", func(t *testing.T) {
		full := *thin
		full.Description = "Now pulled in full"
		full.Metadata = map[string]any{"source": "pull"}
		full.ContentStatus = storage.ContentStatusFull
		if err := entities.Upsert(ctx, &full); err != nil {
			t.Fatalf("Upsert over thin: %v", err)
		}
		got, err := entities.Get(ctx, thin.Slug)
		if err != nil {
			t.Fatalf("Get after full Upsert: %v", err)
		}
		if got.ContentStatus != storage.ContentStatusFull {
			t.Errorf("ContentStatus = %q, want %q", got.ContentStatus, storage.ContentStatusFull)
		}
		if got.Description != full.Description {
			t.Errorf("Description = %q, want %q", got.Description, full.Description)
		}
		if got.Metadata["source"] != "pull" || len(got.Metadata) != 1 {
			t.Errorf("Metadata = %v, want map[source:pull]", got.Metadata)
		}

		// A thin re-sync never clobbers the full record.
		if err := entities.UpsertThin(ctx, thin); err != nil {
			t.Fatalf("UpsertThin over full: %v", err)
		}
		got, err = entities.Get(ctx, thin.Slug)
		if err != nil {
			t.Fatalf("Get after thin re-sync: %v", err)
		}
		if got.ContentStatus != storage.ContentStatusFull || got.Metadata["source"] != "pull" {
			t.Errorf("thin re-sync clobbered full record: status=%q metadata=%v", got.ContentStatus, got.Metadata)
		}
	})
}

func assertThinEntity(t *testing.T, got *storage.Entity) {
	t.Helper()
	if got.Slug != "thin.mentioned" || got.Title != "Mentioned" || got.Namespace != "thin" {
		t.Errorf("entity = %s/%q/%s, want thin.mentioned/\"Mentioned\"/thin", got.Slug, got.Title, got.Namespace)
	}
	if got.ContentStatus != storage.ContentStatusThin {
		t.Errorf("ContentStatus = %q, want %q", got.ContentStatus, storage.ContentStatusThin)
	}
	if len(got.Metadata) != 0 {
		t.Errorf("Metadata = %v, want empty", got.Metadata)
	}
	if got.VersionHash != "v1" || got.RegistryURL != "https://registry.example/thin" {
		t.Errorf("provenance = %q/%q, want v1/https://registry.example/thin", got.VersionHash, got.RegistryURL)
	}
}
