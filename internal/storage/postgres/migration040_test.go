//go:build integration

package postgres_test

import (
	"context"
	"testing"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// TestPgMigration040_URLObjectType mirrors SQLite 059: fetched-page rows
// stored as text become url, every other row keeps its type.
func TestPgMigration040_URLObjectType(t *testing.T) {
	ctx := context.Background()
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(ctx) })
	if err := drv.MigrateThroughForTest(ctx, 39); err != nil {
		t.Fatalf("migrate through 39: %v", err)
	}
	db := drv.DB()
	for _, r := range []struct{ id, typ, pipeline, source string }{
		{"page", "text", "url.generic@v1", "https://example.com/a"},
		{"page-http", "text", "url.article@v0", "HTTP://example.com/b"},
		{"repo", "repo", "url.repo@v1", "https://github.com/o/r"},
		{"repo-as-text", "text", "url.repo@v1", "https://github.com/o/r2"},
		{"doc", "document", "url.generic@v1", "https://example.com/x.pdf"},
		{"note", "text", "text.short@v1", "https://example.com/c"},
		{"pasted", "text", "url.generic@v1", "cli"},
	} {
		if _, err := db.Exec(`INSERT INTO objects (id, type, pipeline, source, created_at, updated_at)
			VALUES ($1, $2, $3, $4, NOW(), NOW())`, r.id, r.typ, r.pipeline, r.source); err != nil {
			t.Fatalf("seed %s: %v", r.id, err)
		}
	}
	for i := 0; i < 2; i++ { // second pass: replay is a no-op
		if err := drv.Migrate(ctx); err != nil {
			t.Fatalf("Migrate #%d: %v", i, err)
		}
		want := map[string]string{
			"page": "url", "page-http": "url", "repo": "repo", "repo-as-text": "url",
			"doc": "document", "note": "text", "pasted": "url",
		}
		for id, typ := range want {
			var got string
			if err := db.QueryRow(`SELECT type FROM objects WHERE id = $1`, id).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != typ {
				t.Errorf("pass %d %s: type = %q, want %q", i, id, got, typ)
			}
		}
	}

	// Replay the statement itself (Migrate skips applied versions): no
	// row matches any more.
	res, err := db.Exec(`UPDATE objects SET type = 'url'
		WHERE type = 'text' AND pipeline LIKE 'url.%'`)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 0 {
		t.Errorf("replay changed %d rows, want 0", n)
	}
}
