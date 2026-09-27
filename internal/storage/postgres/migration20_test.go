//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
)

// TestPgMigration20_RepairsThinEntityMetadata migrates a schema-19 database
// holding a thin entity stored the way UpsertThin used to store it
// (metadata '[]'): the row reads back through Get and List with empty
// metadata, a full entity's metadata is untouched, and a non-empty array —
// which no writer produces — is left as is.
func TestPgMigration20_RepairsThinEntityMetadata(t *testing.T) {
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn)
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	ctx := context.Background()
	if err := drv.MigrateThroughForTest(ctx, 19); err != nil {
		t.Fatalf("migrate through 19: %v", err)
	}
	db := drv.DB()
	seeds := []struct{ slug, status, metadata string }{
		{"thin.legacy", "thin", `[]`},
		{"full.kept", "full", `{"source":"pull"}`},
		{"odd.array", "full", `["x"]`},
	}
	for _, s := range seeds {
		if _, err := db.Exec(`
			INSERT INTO entities (slug, title, metadata, content_status, created_at, updated_at)
			VALUES ($1, $1, $2::jsonb, $3, NOW(), NOW())`, s.slug, s.metadata, s.status); err != nil {
			t.Fatalf("seed %s: %v", s.slug, err)
		}
	}
	if _, err := drv.Entities().Get(ctx, "thin.legacy"); err == nil {
		t.Fatal("schema 19 read the '[]' row without error; the seed no longer reproduces the defect")
	}

	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	thin, err := drv.Entities().Get(ctx, "thin.legacy")
	if err != nil {
		t.Fatalf("Get(thin.legacy): %v", err)
	}
	if len(thin.Metadata) != 0 || thin.ContentStatus != storage.ContentStatusThin {
		t.Errorf("thin.legacy: metadata=%v status=%q, want empty/thin", thin.Metadata, thin.ContentStatus)
	}
	listed, err := drv.Entities().List(ctx, storage.EntityFilter{ContentStatus: storage.ContentStatusThin})
	if err != nil || len(listed) != 1 {
		t.Fatalf("List(thin): %d entities, %v", len(listed), err)
	}
	full, err := drv.Entities().Get(ctx, "full.kept")
	if err != nil {
		t.Fatalf("Get(full.kept): %v", err)
	}
	if full.Metadata["source"] != "pull" || len(full.Metadata) != 1 {
		t.Errorf("full.kept metadata = %v, want map[source:pull]", full.Metadata)
	}
	var odd string
	if err := db.QueryRow(`SELECT metadata::text FROM entities WHERE slug = 'odd.array'`).Scan(&odd); err != nil {
		t.Fatalf("read odd.array: %v", err)
	}
	if odd != `["x"]` {
		t.Errorf("odd.array metadata = %s, want it left as [\"x\"]", odd)
	}
	if n := pgScalar(t, drv, `SELECT MAX(version) FROM schema_version`); n != pgdrv.LatestSchemaVersionForTest() {
		t.Errorf("ledger at %d, want %d", n, pgdrv.LatestSchemaVersionForTest())
	}
}
