package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func seed059(t *testing.T, d *Driver, id, typ, pipeline, source string) {
	t.Helper()
	if _, err := d.db.Exec(`INSERT INTO objects (id, type, pipeline, source, created_at, updated_at)
		VALUES (?, ?, ?, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, id, typ, pipeline, source); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func type059(t *testing.T, d *Driver, id string) string {
	t.Helper()
	var typ string
	if err := d.db.QueryRow(`SELECT type FROM objects WHERE id = ?`, id).Scan(&typ); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return typ
}

// TestMigration059_URLObjectType retypes fetched-page rows stored as
// text and leaves every other row alone.
func TestMigration059_URLObjectType(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v58.db"), 58)
	t.Cleanup(func() { d.Close(ctx) })

	seed059(t, d, "page", "text", "url.generic@v1", "https://example.com/a")
	seed059(t, d, "page-http", "text", "url.article@v0", "HTTP://example.com/b")
	seed059(t, d, "repo", "repo", "url.repo@v1", "https://github.com/o/r")
	seed059(t, d, "repo-as-text", "text", "url.repo@v1", "https://github.com/o/r2")
	seed059(t, d, "doc", "document", "url.generic@v1", "https://example.com/x.pdf")
	seed059(t, d, "note", "text", "text.short@v1", "https://example.com/c")
	seed059(t, d, "pasted", "text", "url.generic@v1", "cli")
	seed059(t, d, "nopipe", "text", "", "https://example.com/d")

	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	want := map[string]string{
		"page": "url", "page-http": "url",
		"repo": "repo", "repo-as-text": "url", "doc": "document",
		"note": "text", "pasted": "url", "nopipe": "text",
	}
	for id, typ := range want {
		if got := type059(t, d, id); got != typ {
			t.Errorf("%s: type = %q, want %q", id, got, typ)
		}
	}

	// Replay: nothing left to match.
	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("replay: %v", err)
	}
	for id, typ := range want {
		if got := type059(t, d, id); got != typ {
			t.Errorf("after replay %s: type = %q, want %q", id, got, typ)
		}
	}

	// Replay the SQL itself (migrateThrough skips applied versions): no
	// row matches any more.
	res, err := d.db.Exec(migration059)
	if err != nil {
		t.Fatalf("exec 058 again: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 0 {
		t.Errorf("second run of 058 changed %d rows, want 0", n)
	}
}
