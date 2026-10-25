package storageutil

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

func testBlobCfg(t *testing.T) config.BlobConfig {
	return config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{Path: filepath.Join(t.TempDir(), "blobs")}}
}

// migrateTestDB creates a migrated SQLite database and returns its path.
func migrateTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	d, err := Open("sqlite", path, testBlobCfg(t), WithMigrateSnapshot(false))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := d.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(ctx); err != nil {
		t.Fatal(err)
	}
	return path
}

// A read open of a current database reads, and never migrates.
func TestOpenReadOnly_CurrentSchema(t *testing.T) {
	ctx := context.Background()
	d, err := OpenReadOnly(ctx, "sqlite", migrateTestDB(t), testBlobCfg(t))
	if err != nil {
		t.Fatalf("read-only open of a current database: %v", err)
	}
	t.Cleanup(func() { _ = d.Close(ctx) })
	if _, _, err := d.Jobs().List(ctx, storage.JobFilter{}); err != nil {
		t.Errorf("list jobs read-only: %v", err)
	}
	if err := d.Init(ctx); !errors.Is(err, sqlite.ErrReadOnly) {
		t.Errorf("Init on the read-only driver = %v; want ErrReadOnly", err)
	}
}

// A read against a missing or never-migrated database fails clearly and
// creates nothing.
func TestOpenReadOnly_NoSchema(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.sqlite")
	empty := filepath.Join(dir, "empty.sqlite")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"missing": missing, "empty": empty} {
		t.Run(name, func(t *testing.T) {
			if _, err := OpenReadOnly(ctx, "sqlite", path, testBlobCfg(t)); !errors.Is(err, ErrNoSchema) {
				t.Fatalf("read-only open = %v; want ErrNoSchema", err)
			}
		})
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("read-only open created the database: %v", err)
	}
}

// A database a version behind is not read: the read handle cannot
// migrate it.
func TestOpenReadOnly_SchemaBehind(t *testing.T) {
	ctx := context.Background()
	path := migrateTestDB(t)
	w, err := Open("sqlite", path, testBlobCfg(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.(*sqlite.Driver).DB().ExecContext(ctx,
		`DELETE FROM schema_version WHERE version = (SELECT MAX(version) FROM schema_version)`); err != nil {
		t.Fatal(err)
	}
	_ = w.Close(ctx)

	if _, err := OpenReadOnly(ctx, "sqlite", path, testBlobCfg(t)); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("read-only open of a database behind = %v; want ErrSchemaMismatch", err)
	}
}
