package federation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// errEntityStore is an unreadable entity store.
var errEntityStore = errors.New("entity store unreadable")

// failingEntities fails Get for the slug bad and delegates the rest.
type failingEntities struct {
	storage.EntityStore
	bad string
}

func (f failingEntities) Get(ctx context.Context, slug string) (*storage.Entity, error) {
	if slug == f.bad {
		return nil, errEntityStore
	}
	return f.EntityStore.Get(ctx, slug)
}

// entityFaultDriver is a source whose entity store fails Get for bad.
type entityFaultDriver struct {
	storage.StorageDriver
	bad string
}

func (d entityFaultDriver) Entities() storage.EntityStore {
	return failingEntities{EntityStore: d.StorageDriver.Entities(), bad: d.bad}
}

// mentionSource seeds one object mentioning the entities "known" (stored)
// and "dangling" (never stored); a non-empty bad adds a mention of an
// entity whose Get fails.
func mentionSource(t *testing.T, bad string) storage.StorageDriver {
	t.Helper()
	ctx := context.Background()
	src := seedSource(t, "m-1")
	now := time.Now().UTC()
	if err := src.Entities().Upsert(ctx, &storage.Entity{
		Slug: "known", Title: "Known", Namespace: "person", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	slugs := []string{"known", "dangling"}
	if bad != "" {
		slugs = append(slugs, bad)
	}
	for _, slug := range slugs {
		if err := src.Edges().Create(ctx, &storage.Edge{
			ID: "edge-" + slug, FromType: "object", FromID: "m-1",
			ToType: "entity", ToID: slug, EdgeType: "mentions", Weight: 1, CreatedAt: now,
		}); err != nil {
			t.Fatalf("seed edge to %s: %v", slug, err)
		}
	}
	return entityFaultDriver{StorageDriver: src, bad: bad}
}

// entityPusher records the entity slugs of every Push.
type entityPusher struct {
	fakePusher
	entities [][]string
}

func (p *entityPusher) Push(ctx context.Context, objects []storage.KnowledgeObject, edges []storage.Edge, entities []storage.Entity) error {
	slugs := make([]string, 0, len(entities))
	for _, e := range entities {
		slugs = append(slugs, e.Slug)
	}
	p.entities = append(p.entities, slugs)
	return p.fakePusher.Push(ctx, objects, edges, entities)
}

// A mention edge whose entity is missing at the source is pushed
// without it; the entities that exist travel with the batch.
func TestSync_SkipsMissingMentionedEntity(t *testing.T) {
	src := mentionSource(t, "")
	p := &entityPusher{fakePusher: fakePusher{name: "t"}}

	if _, err := Sync(context.Background(), src, p, 0); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(p.entities) != 1 || len(p.entities[0]) != 1 || p.entities[0][0] != "known" {
		t.Errorf("entities pushed = %v; want [[known]]", p.entities)
	}
}

// An entity the source cannot read fails the batch: nothing is pushed,
// the watermark stays, and the error names the entity, so the next tick
// retries instead of shipping mention edges without their targets.
func TestSync_EntityStoreErrorFailsBatch(t *testing.T) {
	src := mentionSource(t, "unreadable")
	p := &entityPusher{fakePusher: fakePusher{name: "t"}}

	n, err := Sync(context.Background(), src, p, 0)
	if !errors.Is(err, errEntityStore) {
		t.Fatalf("Sync err = %v; want wrapping %v", err, errEntityStore)
	}
	if want := `"unreadable"`; !strings.Contains(err.Error(), want) {
		t.Errorf("Sync err = %q; want it to name %s", err, want)
	}
	if n != 0 || p.Calls() != 0 {
		t.Errorf("pushed %d objects in %d calls; want none", n, p.Calls())
	}
	if wm, _ := p.Watermark(context.Background()); wm != 0 {
		t.Errorf("watermark = %d; want 0", wm)
	}
}
