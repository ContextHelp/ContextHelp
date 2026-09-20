package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrateFromScratch(t *testing.T) {
	dir := t.TempDir()
	d, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer d.Close(context.Background())

	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Verify all tables exist.
	tables := []string{"objects", "entities", "edges", "jobs", "feeds", "feed_items", "batches", "schema_version", "federation_watermarks"}
	for _, table := range tables {
		var name string
		err := d.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	d, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer d.Close(context.Background())

	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestSchemaVersion(t *testing.T) {
	d := newTestDriver(t)

	var version int
	err := d.db.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version)
	if err != nil {
		t.Fatalf("query version: %v", err)
	}
	want := migrations[len(migrations)-1].Version
	if version != want {
		t.Errorf("version: got %d, want %d", version, want)
	}
}

func TestFTSTableExists(t *testing.T) {
	d := newTestDriver(t)

	var name string
	err := d.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='objects_fts'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("objects_fts not found: %v", err)
	}
}
