//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// sharingTables are the tables migration 31 creates.
var sharingTables = []string{"audiences", "audience_members", "share_policies", "share_grants", "access_log", "access_log_objects"}

// sharingColumns are the columns migration 31 adds.
var sharingColumns = []struct{ table, column string }{
	{"principals", "deny_classes"},
	{"principal_identities", "share_policy_id"},
	{"objects", "classes"},
	{"objects", "projected_fts_body_masked"},
	{"objects", "fts_masked"},
	{"embeddings", "variant"},
}

// pre31Spec is the model the upgrade case registers at schema 30.
var pre31Spec = storage.EmbeddingModelSpec{ModelID: "fixture-m031", Provider: "fixture", Dimension: 3}

// assertSharingSchema checks that every registry table, column and
// index exists and that the embeddings primary key carries variant.
func assertSharingSchema(t *testing.T, drv *pgdrv.Driver) {
	t.Helper()
	db := drv.DB()
	for _, table := range sharingTables {
		if !pgTableExists(t, db, table) {
			t.Errorf("table %s missing", table)
		}
	}
	for _, c := range sharingColumns {
		if !pgColumnExists(t, db, c.table, c.column) {
			t.Errorf("column %s.%s missing", c.table, c.column)
		}
	}
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'objects' AND indexname = 'idx_objects_fts_masked'`); n != 1 {
		t.Errorf("idx_objects_fts_masked: %d rows in pg_indexes, want 1", n)
	}
	if n := pgScalar(t, drv, `SELECT array_length(conkey, 1) FROM pg_constraint WHERE conname = 'embeddings_pkey' AND conrelid = 'embeddings'::regclass`); n != 4 {
		t.Errorf("embeddings_pkey spans %d columns, want 4 (variant added)", n)
	}
}

// seedPre31 builds a schema-30 database at dsn holding an object, a
// principal, an identity, a registered model and an embedding, written
// by hand where the stores write the columns 31 adds, and returns the
// identity.
func seedPre31(t *testing.T, dsn string) *storage.Identity {
	t.Helper()
	ctx := context.Background()
	old, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	if err := old.MigrateThroughForTest(ctx, 30); err != nil {
		t.Fatalf("migrate through 30: %v", err)
	}
	for _, table := range sharingTables {
		if pgTableExists(t, old.DB(), table) {
			t.Fatalf("schema 30 already has %s: nothing for 31 to create", table)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := old.DB().ExecContext(ctx, `INSERT INTO objects (id, type, raw_content, text_content, projected_fts_body, status, created_at, updated_at, fts_indexed)
		VALUES ('m031', 'note', 'before the registry', 'before the registry', 'before the registry', 'active', $1, $1, TRUE)`, now); err != nil {
		t.Fatalf("insert object at 30: %v", err)
	}
	person := &storage.Principal{ID: "prn_m031", Kind: storage.PrincipalKindPerson, DisplayName: "Before"}
	if err := old.Auth().Principals().Create(ctx, person); err != nil {
		t.Fatalf("create principal at 30: %v", err)
	}
	identity := &storage.Identity{PrincipalID: person.ID, Method: storage.IdentityMethodAPIKey, Subject: "key-m031"}
	if err := old.Auth().Identities().Create(ctx, identity); err != nil {
		t.Fatalf("create identity at 30: %v", err)
	}
	if _, err := old.DB().ExecContext(ctx, `INSERT INTO embedding_models (model_id, provider, dimension, is_default, registered_at, config_json)
		VALUES ($1, $2, $3, 0, NOW(), '{}')`, pre31Spec.ModelID, pre31Spec.Provider, pre31Spec.Dimension); err != nil {
		t.Fatalf("register model at 30: %v", err)
	}
	if err := old.Embeddings().EnsureIndex(ctx, pre31Spec); err != nil {
		t.Fatalf("EnsureIndex at 30: %v", err)
	}
	if _, err := old.DB().ExecContext(ctx, `INSERT INTO embeddings (object_id, model_id, chunk_idx, vector, text, created_at)
		VALUES ('m031', $1, 0, '[1,0,0]'::vector, 'embedded', NOW())`, pre31Spec.ModelID); err != nil {
		t.Fatalf("insert embedding at 30: %v", err)
	}
	if err := old.Close(ctx); err != nil {
		t.Fatal(err)
	}
	return identity
}

// assertPre31Defaults checks the rows seedPre31 wrote took the defaults
// and read back through the migrated driver.
func assertPre31Defaults(t *testing.T, drv *pgdrv.Driver, identity *storage.Identity) *storage.KnowledgeObject {
	t.Helper()
	ctx := context.Background()
	if got, err := drv.Sharing().DenyClasses(ctx, identity.PrincipalID); err != nil || got != nil {
		t.Errorf("DenyClasses(existing) = %v, %v; want none", got, err)
	}
	if got, err := drv.Sharing().IdentityPolicy(ctx, identity.ID); err != nil || got != "" {
		t.Errorf("IdentityPolicy(existing) = %q, %v; want empty", got, err)
	}
	obj, err := drv.Objects().Get(ctx, "m031")
	if err != nil {
		t.Fatalf("Get after 31: %v", err)
	}
	if obj.Classes != nil {
		t.Errorf("classes of a pre-31 row = %v, want none", obj.Classes)
	}
	rows, err := drv.Embeddings().Get(ctx, "m031", pre31Spec.ModelID)
	if err != nil || len(rows) != 1 || rows[0].Variant != storage.EmbeddingVariantRaw || rows[0].Text != "embedded" {
		t.Fatalf("Get embeddings after 31 = %+v, %v; want the raw row", rows, err)
	}
	return obj
}

// assertPost31Write checks a write after the migration round-trips
// classes, fills the masked tsvector and stores both variants.
func assertPost31Write(t *testing.T, drv *pgdrv.Driver, obj *storage.KnowledgeObject) {
	t.Helper()
	ctx := context.Background()
	obj.Classes = []string{"personal"}
	if err := drv.Objects().Update(ctx, obj); err != nil {
		t.Fatalf("Update after 31: %v", err)
	}
	got, err := drv.Objects().Get(ctx, "m031")
	if err != nil || len(got.Classes) != 1 || got.Classes[0] != "personal" {
		t.Fatalf("classes after Update = %v, %v; want personal", got.Classes, err)
	}
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM objects WHERE fts_masked @@ websearch_to_tsquery('simple', 'registry')`); n != 1 {
		t.Errorf("masked tsvector hits after Update = %d, want 1", n)
	}
	if err := drv.Embeddings().Put(ctx, "m031", []storage.ObjectVector{
		{ModelID: pre31Spec.ModelID, Vector: []float32{1, 0, 0}},
		{ModelID: pre31Spec.ModelID, Vector: []float32{0, 1, 0}, Variant: storage.EmbeddingVariantMasked},
	}); err != nil {
		t.Fatalf("Put both variants after 31: %v", err)
	}
	rows, err := drv.Embeddings().Get(ctx, "m031", pre31Spec.ModelID)
	if err != nil || len(rows) != 2 || rows[0].Variant != storage.EmbeddingVariantRaw || rows[1].Variant != storage.EmbeddingVariantMasked {
		t.Fatalf("Get both variants = %+v, %v; want raw then masked", rows, err)
	}
}

// TestPgMigration031_Sharing pins that a fresh database and one migrated
// from schema 30 both carry the sharing registry, that rows written
// before the migration keep their defaults and read back, that classes
// and both variants round-trip afterwards, and that replaying the chain
// is a no-op.
func TestPgMigration031_Sharing(t *testing.T) {
	ctx := context.Background()

	t.Run("fresh install", func(t *testing.T) {
		drv, _ := freshIntegrationDriver(t)
		assertSharingSchema(t, drv)
	})

	t.Run("upgrade from 30", func(t *testing.T) {
		dsn, _ := freshDatabaseDSN(t)
		identity := seedPre31(t, dsn)

		drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
		if err != nil {
			t.Fatalf("postgres.New: %v", err)
		}
		t.Cleanup(func() { drv.Close(ctx) })
		if err := drv.Migrate(ctx); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		assertSharingSchema(t, drv)
		obj := assertPre31Defaults(t, drv, identity)
		assertPost31Write(t, drv, obj)

		if err := drv.Migrate(ctx); err != nil {
			t.Fatalf("replay Migrate: %v", err)
		}
		assertSharingSchema(t, drv)
		var version sql.NullInt64
		if err := drv.DB().QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
			t.Fatalf("read ledger: %v", err)
		}
		if int(version.Int64) != pgdrv.LatestSchemaVersionForTest() {
			t.Errorf("schema_version after migrate = %d, want %d", version.Int64, pgdrv.LatestSchemaVersionForTest())
		}
	})
}
