package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func makeEntity(slug, namespace string) *storage.Entity {
	now := time.Now().Truncate(time.Second)
	return &storage.Entity{
		Slug:        slug,
		Title:       "Title for " + slug,
		Description: "Description",
		Namespace:   namespace,
		Aliases:     []string{slug + "-alias"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func TestUpsertAndGet(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	e := makeEntity("ui.layout", "ui")
	if err := d.Entities().Upsert(ctx, e); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := d.Entities().Get(ctx, "ui.layout")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Slug != "ui.layout" {
		t.Errorf("Slug: got %q", got.Slug)
	}
	if got.Namespace != "ui" {
		t.Errorf("Namespace: got %q", got.Namespace)
	}
}

func TestUpsertUpdate(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	e := makeEntity("ui.layout", "ui")
	d.Entities().Upsert(ctx, e)

	e.Title = "Updated Title"
	e.UpdatedAt = time.Now().Truncate(time.Second)
	d.Entities().Upsert(ctx, e)

	got, _ := d.Entities().Get(ctx, "ui.layout")
	if got.Title != "Updated Title" {
		t.Errorf("Title after upsert: got %q", got.Title)
	}
}

func TestResolve(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	e := makeEntity("ui.layout", "ui")
	e.Aliases = []string{"layout", "ui-layout"}
	d.Entities().Upsert(ctx, e)

	// Resolve by alias.
	got, err := d.Entities().Resolve(ctx, "layout")
	if err != nil {
		t.Fatalf("resolve by alias: %v", err)
	}
	if got.Slug != "ui.layout" {
		t.Errorf("Slug: got %q", got.Slug)
	}
}

func TestEntityList(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Entities().Upsert(ctx, makeEntity("ui.layout", "ui"))
	d.Entities().Upsert(ctx, makeEntity("ui.color", "ui"))
	d.Entities().Upsert(ctx, makeEntity("api.auth", "api"))

	entities, err := d.Entities().List(ctx, storage.EntityFilter{Namespace: "ui"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entities) != 2 {
		t.Errorf("count: got %d, want 2", len(entities))
	}
}
