//go:build integration

package postgres_test

import (
	"context"
	"testing"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
)

// TestPostgres_SearchSchema_FTSColumns pins the FTS half of the search
// schema: projected_fts_body, the STORED generated tsvector column over it,
// and the GIN index. Generated-column maintenance replaces SQLite's manual
// delete+reinsert and cannot drift from the row.
func TestPostgres_SearchSchema_FTSColumns(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	db := drv.DB()

	if !pgColumnExists(t, db, "objects", "projected_fts_body") {
		t.Fatal("objects.projected_fts_body missing")
	}

	// fts must be a STORED generated column of type tsvector.
	var attgenerated, typname string
	err := db.QueryRow(`
		SELECT a.attgenerated, t.typname
		  FROM pg_attribute a
		  JOIN pg_type t ON t.oid = a.atttypid
		 WHERE a.attrelid = 'objects'::regclass
		   AND a.attname = 'fts' AND NOT a.attisdropped`).Scan(&attgenerated, &typname)
	if err != nil {
		t.Fatalf("objects.fts column: %v", err)
	}
	if typname != "tsvector" {
		t.Errorf("objects.fts type: got %q want tsvector", typname)
	}
	if attgenerated != "s" {
		t.Errorf("objects.fts must be a STORED generated column, attgenerated=%q", attgenerated)
	}

	// GIN index over fts.
	var ginCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM pg_indexes
		 WHERE tablename = 'objects' AND indexdef ILIKE '%USING gin%(fts)%'`).Scan(&ginCount)
	if err != nil {
		t.Fatalf("count gin indexes: %v", err)
	}
	if ginCount == 0 {
		t.Error("no GIN index over objects.fts")
	}

	// The generated column must track the row with no manual maintenance.
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO objects (id, type, projected_fts_body, created_at, updated_at)
		VALUES ('schema-fts-1', 'note', 'postgres parity landed cleanly', NOW(), NOW())`); err != nil {
		t.Fatalf("insert row with fts body: %v", err)
	}
	var hit bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM objects
			 WHERE id = 'schema-fts-1'
			   AND fts @@ websearch_to_tsquery('simple', 'parity')
		)`).Scan(&hit); err != nil {
		t.Fatalf("query generated tsvector: %v", err)
	}
	if !hit {
		t.Error("generated fts column did not index projected_fts_body")
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE objects SET projected_fts_body = 'entirely different words' WHERE id = 'schema-fts-1'`); err != nil {
		t.Fatalf("update fts body: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM objects
			 WHERE id = 'schema-fts-1'
			   AND fts @@ websearch_to_tsquery('simple', 'parity')
		)`).Scan(&hit); err != nil {
		t.Fatalf("re-query generated tsvector: %v", err)
	}
	if hit {
		t.Error("generated fts column did not follow the row update")
	}
}

// TestPostgres_SearchSchema_HonestVectorColumns pins the vector half:
// embeddings.vector is a real pgvector column (the dead BYTEA is gone), the
// phantom index signature stamped for the nonexistent BYTEA index is
// removed, and the ANN index on objects.embedding is HNSW cosine.
func TestPostgres_SearchSchema_HonestVectorColumns(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	db := drv.DB()

	var udt string
	err := db.QueryRow(`
		SELECT udt_name FROM information_schema.columns
		 WHERE table_name = 'embeddings' AND column_name = 'vector'`).Scan(&udt)
	if err != nil {
		t.Fatalf("embeddings.vector column: %v", err)
	}
	if udt != "vector" {
		t.Errorf("embeddings.vector type: got %q want vector (pgvector)", udt)
	}

	// No signature row may exist for an index that does not exist.
	var phantom int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM index_signatures
		 WHERE signature_id LIKE 'embeddings_%'`).Scan(&phantom); err != nil {
		t.Fatalf("count phantom signatures: %v", err)
	}
	if phantom != 0 {
		t.Errorf("write-only signature rows stamped for nonexistent embeddings index: %d", phantom)
	}

	// ANN index: HNSW cosine on objects.embedding; the old ivfflat is gone.
	var hnsw, ivfflat int
	if err := db.QueryRow(`
		SELECT
			COUNT(*) FILTER (WHERE indexdef ILIKE '%USING hnsw%embedding%cosine%'),
			COUNT(*) FILTER (WHERE indexdef ILIKE '%USING ivfflat%')
		FROM pg_indexes WHERE tablename = 'objects'`).Scan(&hnsw, &ivfflat); err != nil {
		t.Fatalf("inspect ANN indexes: %v", err)
	}
	if hnsw == 0 {
		t.Error("no HNSW cosine index on objects.embedding")
	}
	if ivfflat != 0 {
		t.Error("stale ivfflat index still present on objects")
	}
}

// TestPostgres_SetVectorDimension mirrors the SQLite driver contract: the
// dimension configured before Init is applied to the schema at migration
// time (dynamic DDL), and has no effect afterwards.
func TestPostgres_SetVectorDimension(t *testing.T) {
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn)
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })

	drv.SetVectorDimension(8)
	if err := drv.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate with dimension 8: %v", err)
	}

	var colType string
	err = drv.DB().QueryRow(`
		SELECT format_type(a.atttypid, a.atttypmod)
		  FROM pg_attribute a
		 WHERE a.attrelid = 'objects'::regclass
		   AND a.attname = 'embedding' AND NOT a.attisdropped`).Scan(&colType)
	if err != nil {
		t.Fatalf("read embedding column type: %v", err)
	}
	if colType != "vector(8)" {
		t.Errorf("embedding column type: got %q want vector(8)", colType)
	}

	// A vector of the configured dimension must be storable.
	if _, err := drv.DB().Exec(`
		INSERT INTO objects (id, type, embedding, created_at, updated_at)
		VALUES ('dim-8-1', 'note', '[1,2,3,4,5,6,7,8]', NOW(), NOW())`); err != nil {
		t.Errorf("store 8-dim vector: %v", err)
	}
}
