//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
)

// TestPgMigration16_BackfillsTextContent migrates a schema-15 database
// (no objects.text_content) holding objects: each row's text_content is
// backfilled with projection.BodyText of what was stored, which before 16
// is always raw_content.
func TestPgMigration16_BackfillsTextContent(t *testing.T) {
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn)
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	ctx := context.Background()
	if err := drv.MigrateThroughForTest(ctx, 15); err != nil {
		t.Fatalf("migrate through 15: %v", err)
	}
	db := drv.DB()
	if pgColumnExists(t, db, "objects", "text_content") {
		t.Fatal("schema 15 already has objects.text_content")
	}
	seeds := map[string]string{"with-raw": "raw body", "empty": ""}
	for id, raw := range seeds {
		if _, err := db.Exec(`
			INSERT INTO objects (id, type, raw_content, created_at, updated_at)
			VALUES ($1, 'note', $2, NOW(), NOW())`, id, raw); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for id, raw := range seeds {
		obj, err := drv.Objects().Get(ctx, id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		if obj.TextContent != raw || obj.RawContent != raw {
			t.Errorf("%s: text_content=%q raw_content=%q, want both %q", id, obj.TextContent, obj.RawContent, raw)
		}
	}
	if n := pgScalar(t, drv, `SELECT MAX(version) FROM schema_version`); n != pgdrv.LatestSchemaVersionForTest() {
		t.Errorf("ledger at %d, want %d", n, pgdrv.LatestSchemaVersionForTest())
	}
}

// TestPgObjects_TextContentReadPaths pins that every object read path
// hydrates the stored text_content, and that Update and Reinforce write it.
func TestPgObjects_TextContentReadPaths(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()
	objs := drv.Objects()
	now := time.Now()

	obj := &storage.KnowledgeObject{
		ID: "tc", Type: "note", RawContent: "raw quokka", TextContent: "extracted quokka",
		ContentHash: "tc-hash", CreatedAt: now, UpdatedAt: now,
	}
	if err := objs.Create(ctx, obj); err != nil {
		t.Fatalf("Create: %v", err)
	}
	assertPgTextContentEverywhere(t, objs, "quokka", "extracted quokka")

	obj.TextContent = "rewritten wombat"
	obj.UpdatedAt = time.Now()
	if err := objs.Update(ctx, obj); err != nil {
		t.Fatalf("Update: %v", err)
	}
	assertPgTextContentEverywhere(t, objs, "wombat", "rewritten wombat")

	// Reinforce with new raw content and no extracted text stores the
	// raw body as text_content (projection.BodyText), matching SQLite.
	if _, err := objs.Reinforce(ctx, "tc-hash", &storage.KnowledgeObject{RawContent: "reinforced body"}); err != nil {
		t.Fatalf("Reinforce: %v", err)
	}
	got, err := objs.Get(ctx, "tc")
	if err != nil {
		t.Fatalf("Get after Reinforce: %v", err)
	}
	if got.TextContent != "reinforced body" {
		t.Errorf("text_content after Reinforce = %q, want %q", got.TextContent, "reinforced body")
	}
}

// assertPgTextContentEverywhere reads object "tc" back through Get, List,
// ListBySQL and FTSSearch (by term) and checks each carries want.
func assertPgTextContentEverywhere(t *testing.T, objs storage.ObjectStore, term, want string) {
	t.Helper()
	ctx := context.Background()
	got, err := objs.Get(ctx, "tc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TextContent != want {
		t.Errorf("Get TextContent = %q, want %q", got.TextContent, want)
	}
	listed, _, err := objs.List(ctx, storage.ObjectFilter{})
	if err != nil || len(listed) != 1 {
		t.Fatalf("List: %d objects, %v", len(listed), err)
	}
	if listed[0].TextContent != want {
		t.Errorf("List TextContent = %q, want %q", listed[0].TextContent, want)
	}
	bySQL, _, err := objs.ListBySQL(ctx, "id = $1", []any{"tc"}, 10, 0)
	if err != nil || len(bySQL) != 1 {
		t.Fatalf("ListBySQL: %d objects, %v", len(bySQL), err)
	}
	if bySQL[0].TextContent != want {
		t.Errorf("ListBySQL TextContent = %q, want %q", bySQL[0].TextContent, want)
	}
	hits, err := objs.FTSSearch(ctx, term, storage.ObjectFilter{})
	if err != nil || len(hits) != 1 {
		t.Fatalf("FTSSearch(%q): %d hits, %v", term, len(hits), err)
	}
	if hits[0].TextContent != want {
		t.Errorf("FTSSearch TextContent = %q, want %q", hits[0].TextContent, want)
	}
}
