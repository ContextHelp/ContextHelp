package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// sharingTables are the tables migration 051 creates.
var sharingTables = []string{"audiences", "audience_members", "share_policies", "share_grants", "access_log", "access_log_objects", "objects_fts_masked"}

// sharingColumnsWanted are the columns migration 051 adds.
var sharingColumnsWanted = []struct{ table, column string }{
	{"principals", "deny_classes"},
	{"principal_identities", "share_policy_id"},
	{"objects", "classes"},
	{"objects", "projected_fts_body_masked"},
	{"embeddings", "variant"},
}

// assertSharingSchema checks that every registry table and column
// exists, that the embeddings key carries variant and that every
// embeddings row is raw.
func assertSharingSchema(t *testing.T, d *Driver) {
	t.Helper()
	for _, table := range sharingTables {
		if !hasTable(t, d, table) {
			t.Errorf("table %s missing", table)
		}
	}
	for _, c := range sharingColumnsWanted {
		if n := scalarInt(t, d, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, c.table, c.column); n != 1 {
			t.Errorf("column %s.%s: %d, want 1", c.table, c.column, n)
		}
	}
	// The unique key is (object_id, model_id, chunk_idx, variant): the
	// index that SQLite builds for it lists four columns.
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM pragma_index_list('embeddings') il
		JOIN pragma_index_info(il.name) ii WHERE il."unique" = 1`); n != 4 {
		t.Errorf("embeddings unique key spans %d columns, want 4 (variant added)", n)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM embeddings WHERE variant <> 'raw'`); n != 0 {
		t.Errorf("%d embeddings rows are not raw after the migration", n)
	}
}

// seedPre051 writes an indexed object, a principal and an identity into
// a schema-50 database by hand (the stores write the columns 051 adds)
// and returns the identity.
func seedPre051(t *testing.T, d *Driver) *storage.Identity {
	t.Helper()
	ctx := context.Background()
	for _, table := range sharingTables {
		if hasTable(t, d, table) {
			t.Fatalf("schema 50 already has %s: nothing for 051 to create", table)
		}
	}
	if _, err := d.db.ExecContext(ctx, `INSERT INTO objects (id, type, raw_content, text_content, status, created_at, updated_at, projected_fts_body, fts_indexed)
		VALUES ('m051', 'note', 'before the registry', 'before the registry', 'active', '2026-10-15T00:00:00Z', '2026-10-15T00:00:00Z', 'before the registry', 1)`); err != nil {
		t.Fatalf("insert object at 50: %v", err)
	}
	if _, err := d.db.ExecContext(ctx, `INSERT INTO objects_fts(rowid, id, projected_fts_body)
		VALUES ((SELECT rowid FROM objects WHERE id = 'm051'), 'm051', 'before the registry')`); err != nil {
		t.Fatalf("index object at 50: %v", err)
	}
	person := &storage.Principal{ID: "prn_m051", Kind: storage.PrincipalKindPerson, DisplayName: "Before"}
	if err := d.Auth().Principals().Create(ctx, person); err != nil {
		t.Fatalf("create principal at 50: %v", err)
	}
	identity := &storage.Identity{PrincipalID: person.ID, Method: storage.IdentityMethodAPIKey, Subject: "key-m051"}
	if err := d.Auth().Identities().Create(ctx, identity); err != nil {
		t.Fatalf("create identity at 50: %v", err)
	}
	return identity
}

// assertPre051Defaults checks the rows seedPre051 wrote took the
// defaults: no ceiling, no key policy, no classes, an empty masked body
// that the re-projection fills.
func assertPre051Defaults(t *testing.T, d *Driver, identity *storage.Identity) *storage.KnowledgeObject {
	t.Helper()
	ctx := context.Background()
	if got, err := d.Sharing().DenyClasses(ctx, identity.PrincipalID); err != nil || got != nil {
		t.Errorf("DenyClasses(existing) = %v, %v; want none", got, err)
	}
	if got, err := d.Sharing().IdentityPolicy(ctx, identity.ID); err != nil || got != "" {
		t.Errorf("IdentityPolicy(existing) = %q, %v; want empty", got, err)
	}
	obj, err := d.Objects().Get(ctx, "m051")
	if err != nil {
		t.Fatalf("Get after 051: %v", err)
	}
	if obj.Classes != nil {
		t.Errorf("classes of a pre-051 row = %v, want none", obj.Classes)
	}
	var masked string
	if err := d.db.QueryRowContext(ctx, `SELECT projected_fts_body_masked FROM objects WHERE id = 'm051'`).Scan(&masked); err != nil || masked != "" {
		t.Errorf("masked body of a pre-051 row = %q, %v; want empty until re-projection", masked, err)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM objects WHERE projection_version != ?`, indexsig.ProjectionVersion); n != 1 {
		t.Errorf("stale projections after 051 = %d, want the pre-051 row", n)
	}
	return obj
}

// assertPost051Write checks a write after the migration round-trips
// classes and fills the masked body, which the masked FTS table answers
// for.
func assertPost051Write(t *testing.T, d *Driver, obj *storage.KnowledgeObject) {
	t.Helper()
	ctx := context.Background()
	obj.Classes = []string{"personal", "financial"}
	if err := d.Objects().Update(ctx, obj); err != nil {
		t.Fatalf("Update after 051: %v", err)
	}
	got, err := d.Objects().Get(ctx, "m051")
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if len(got.Classes) != 2 || got.Classes[0] != "financial" || got.Classes[1] != "personal" {
		t.Errorf("classes after Update = %v, want financial, personal", got.Classes)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM objects_fts_masked WHERE objects_fts_masked MATCH 'registry'`); n != 1 {
		t.Errorf("masked FTS hits after Update = %d, want 1", n)
	}
}

// TestMigration051_Sharing migrates a schema-50 database holding an
// object, a principal and an identity: the registry appears, the
// existing rows keep their defaults, the object's classes round-trip,
// re-running 051 is a no-op and the ledger ends at the latest version.
func TestMigration051_Sharing(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v50.db"), 50)
	t.Cleanup(func() { d.Close(ctx) })
	identity := seedPre051(t, d)

	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertSharingSchema(t, d)
	obj := assertPre051Defaults(t, d, identity)
	assertPost051Write(t, d, obj)

	if err := migrate051Sharing(ctx, d); err != nil {
		t.Fatalf("re-run 051: %v", err)
	}
	assertSharingSchema(t, d)
	if got, _ := d.Objects().Get(ctx, "m051"); len(got.Classes) != 2 {
		t.Errorf("classes after re-run = %v, want kept", got.Classes)
	}
	if n := scalarInt(t, d, `SELECT MAX(version) FROM schema_version`); n != latestMigrationVersion() {
		t.Errorf("schema_version after migrate = %d, want %d", n, latestMigrationVersion())
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM schema_version WHERE version = 51`); n != 1 {
		t.Errorf("ledger rows for 051: %d, want 1", n)
	}
}

// seedPre051Embedding registers a model, builds its index and writes one
// embedding by hand into a schema-50 database (the store writes the
// variant column 051 adds). It returns the spec and the row's id.
func seedPre051Embedding(t *testing.T, d *Driver) (storage.EmbeddingModelSpec, int) {
	t.Helper()
	ctx := context.Background()
	if _, err := d.db.ExecContext(ctx, `INSERT INTO objects (id, type, raw_content, status, created_at, updated_at)
		VALUES ('e051', 'note', 'embedded', 'active', '2026-10-15T00:00:00Z', '2026-10-15T00:00:00Z')`); err != nil {
		t.Fatalf("insert object at 50: %v", err)
	}
	spec := storage.EmbeddingModelSpec{ModelID: "fixture-m051", Provider: "fixture", Dimension: 3}
	if _, err := d.db.ExecContext(ctx, `INSERT INTO embedding_models (model_id, provider, dimension, is_default, registered_at, config_json)
		VALUES (?, ?, ?, 0, '2026-10-15T00:00:00Z', '{}')`, spec.ModelID, spec.Provider, spec.Dimension); err != nil {
		t.Fatalf("register model at 50: %v", err)
	}
	if err := d.Embeddings().EnsureIndex(ctx, spec); err != nil {
		t.Fatalf("EnsureIndex at 50: %v", err)
	}
	blob, err := sqlite_vec.SerializeFloat32([]float32{1, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.ExecContext(ctx, `INSERT INTO embeddings (object_id, model_id, chunk_idx, vector, text, created_at)
		VALUES ('e051', ?, 0, ?, 'embedded', '2026-10-15T00:00:00Z')`, spec.ModelID, blob); err != nil {
		t.Fatalf("insert embedding at 50: %v", err)
	}
	return spec, scalarInt(t, d, `SELECT id FROM embeddings WHERE object_id = 'e051'`)
}

// assertEmbeddingSurvived051 checks the seeded row kept its id and
// reads back raw, and that the vec0 index and its triggers were rebuilt.
func assertEmbeddingSurvived051(t *testing.T, d *Driver, spec storage.EmbeddingModelSpec, idBefore int) {
	t.Helper()
	ctx := context.Background()
	if id := scalarInt(t, d, `SELECT id FROM embeddings WHERE object_id = 'e051'`); id != idBefore {
		t.Errorf("embeddings.id changed across the rebuild: %d -> %d", idBefore, id)
	}
	rows, err := d.Embeddings().Get(ctx, "e051", spec.ModelID)
	if err != nil || len(rows) != 1 || rows[0].Variant != storage.EmbeddingVariantRaw || rows[0].Text != "embedded" {
		t.Fatalf("Get after 051 = %+v, %v; want the raw row", rows, err)
	}
	ix := vecIndexFor(spec.ModelID)
	for _, trig := range []string{ix.insTrig, ix.updTrig, ix.delTrig} {
		if n := scalarInt(t, d, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = ?`, trig); n != 1 {
			t.Errorf("trigger %s after 051: %d, want rebuilt", trig, n)
		}
	}
	hits, err := d.Embeddings().Search(ctx, storage.VectorQuery{ModelID: spec.ModelID, Vector: []float32{1, 0, 0}, TopK: 1})
	if err != nil || len(hits) != 1 || hits[0].ObjectID != "e051" {
		t.Fatalf("Search after 051 = %+v, %v; want the row through the rebuilt index", hits, err)
	}
}

// TestMigration051_EmbeddingsKeepIDsAndIndexes migrates a schema-50
// database holding a registered model and an embedding: the row keeps
// its id and vector, reads back raw, the vec0 index and its triggers are
// rebuilt, and a masked row for the same chunk can now be stored beside
// it.
func TestMigration051_EmbeddingsKeepIDsAndIndexes(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v50.db"), 50)
	t.Cleanup(func() { d.Close(ctx) })
	spec, idBefore := seedPre051Embedding(t, d)

	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertSharingSchema(t, d)
	assertEmbeddingSurvived051(t, d, spec, idBefore)

	if err := d.Embeddings().Put(ctx, "e051", []storage.ObjectVector{
		{ModelID: spec.ModelID, Vector: []float32{1, 0, 0}},
		{ModelID: spec.ModelID, Vector: []float32{0, 1, 0}, Variant: storage.EmbeddingVariantMasked},
	}); err != nil {
		t.Fatalf("Put both variants after 051: %v", err)
	}
	rows, err := d.Embeddings().Get(ctx, "e051", spec.ModelID)
	if err != nil || len(rows) != 2 || rows[0].Variant != storage.EmbeddingVariantRaw || rows[1].Variant != storage.EmbeddingVariantMasked {
		t.Fatalf("Get both variants = %+v, %v; want raw then masked", rows, err)
	}
}

// TestMigration051_FreshInstall pins that a fresh database carries the
// sharing registry and the variant key.
func TestMigration051_FreshInstall(t *testing.T) {
	assertSharingSchema(t, newTestDriver(t))
}
