//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// TestPgMigration23_AliasesObjectFK migrates a schema-22 database (no
// foreign key from aliases to objects) holding an alias whose object is
// gone and one whose object exists: the orphan is deleted, the other
// kept, and deleting an object now deletes its aliases. Replaying the
// migration is a no-op.
func TestPgMigration23_AliasesObjectFK(t *testing.T) {
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	ctx := context.Background()
	if err := drv.MigrateThroughForTest(ctx, 22); err != nil {
		t.Fatalf("migrate through 22: %v", err)
	}
	db := drv.DB()
	if _, err := db.Exec(`
		INSERT INTO objects (id, type, raw_content, created_at, updated_at)
		VALUES ('m23-live', 'note', 'body', NOW(), NOW())`); err != nil {
		t.Fatalf("seed object: %v", err)
	}
	for alias, objectID := range map[string]string{"m23-orphan": "m23-deleted", "m23-kept": "m23-live"} {
		if _, err := db.Exec(`
			INSERT INTO aliases (alias, object_id, scope, profile, created_at, updated_at)
			VALUES ($1, $2, 'global', '', NOW(), NOW())`, alias, objectID); err != nil {
			t.Fatalf("seed alias %s: %v (schema 22 already enforces a foreign key?)", alias, err)
		}
	}

	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if id, err := drv.Aliases().Resolve(ctx, "m23-orphan", ""); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("orphan alias after migrate: Resolve = %q, %v; want ErrNotFound", id, err)
	}
	if id, err := drv.Aliases().Resolve(ctx, "m23-kept", ""); err != nil || id != "m23-live" {
		t.Errorf("kept alias after migrate: Resolve = %q, %v; want m23-live", id, err)
	}
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM pg_constraint WHERE conname = 'aliases_object_fk'`); n != 1 {
		t.Errorf("aliases_object_fk constraints = %d, want 1", n)
	}
	if err := drv.Aliases().Create(ctx, &storage.Alias{
		Alias: "m23-dangling", ObjectID: "m23-nowhere", Scope: "global",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err == nil {
		t.Error("alias to a missing object was stored; want the foreign key to refuse it")
	}

	if err := drv.Objects().Delete(ctx, "m23-live"); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	if id, err := drv.Aliases().Resolve(ctx, "m23-kept", ""); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("alias after its object's delete: Resolve = %q, %v; want ErrNotFound", id, err)
	}

	if _, err := db.Exec(`DELETE FROM schema_version WHERE version = 23`); err != nil {
		t.Fatalf("unrecord 23: %v", err)
	}
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("replay migrate: %v", err)
	}
	if n := pgScalar(t, drv, `SELECT MAX(version) FROM schema_version`); n != pgdrv.LatestSchemaVersionForTest() {
		t.Errorf("ledger at %d, want %d", n, pgdrv.LatestSchemaVersionForTest())
	}
}
