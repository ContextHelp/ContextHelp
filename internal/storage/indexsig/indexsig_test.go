package indexsig

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

// TestHashFTSInputs_ByteStableAcrossPackageMove pins the SQLite FTS hash
// formula to the exact value the pre-package sqlite driver computed. Existing
// installations carry stored hashes; a formula drift here would flag every
// healthy index as mismatched on upgrade and trigger spurious rebuilds.
// Golden value captured from the original sqlite implementation.
func TestHashFTSInputs_ByteStableAcrossPackageMove(t *testing.T) {
	const golden = "abd3a3ef51eb220ec218ff07b660907e6345e6c0fbdf1e405e38d4013a9a1299"
	got := HashFTSInputs("fts5-default", "v1", "CREATE VIRTUAL TABLE objects_fts USING fts5(x)")
	if got != golden {
		t.Fatalf("SQLite FTS hash formula drifted:\n  got  %s\n  want %s", got, golden)
	}
}

func TestHashFTSInputs_ChangesWhenInputsChange(t *testing.T) {
	base := HashFTSInputs("fts5-default", "v1", "ddl")
	if HashFTSInputs("icu", "v1", "ddl") == base {
		t.Error("tokenizer change must change hash")
	}
	if HashFTSInputs("fts5-default", "v2", "ddl") == base {
		t.Error("projection change must change hash")
	}
	if HashFTSInputs("fts5-default", "v1", "other") == base {
		t.Error("ddl change must change hash")
	}
}

// TestComputeEmbedding_ExtendedInputs pins the extended embedding signature:
// the index description (method, ops class, build params) participates in the
// hash, so a rebuilt index with different tuning no longer verifies against
// the old stamp — and the summary names every input for operators.
func TestComputeEmbedding_ExtendedInputs(t *testing.T) {
	base, summary := ComputeEmbedding("m1", "openai", 1536, "hnsw", "vector_cosine_ops", "m='16', ef_construction='64'")
	for _, part := range []string{
		"model_id=m1", "provider=openai", "dimension=1536",
		"method=hnsw", "ops=vector_cosine_ops", "params=m='16', ef_construction='64'",
	} {
		if !strings.Contains(summary, part) {
			t.Errorf("summary missing %q: %s", part, summary)
		}
	}

	if h, _ := ComputeEmbedding("m1", "openai", 1536, "ivfflat", "vector_cosine_ops", "m='16', ef_construction='64'"); h == base {
		t.Error("method change must change hash")
	}
	if h, _ := ComputeEmbedding("m1", "openai", 1536, "hnsw", "vector_l2_ops", "m='16', ef_construction='64'"); h == base {
		t.Error("ops class change must change hash")
	}
	if h, _ := ComputeEmbedding("m1", "openai", 1536, "hnsw", "vector_cosine_ops", ""); h == base {
		t.Error("build params change must change hash")
	}
	if h, _ := ComputeEmbedding("m1", "openai", 768, "hnsw", "vector_cosine_ops", "m='16', ef_construction='64'"); h == base {
		t.Error("dimension change must change hash")
	}
}

func TestComputeEmbedding_Deterministic(t *testing.T) {
	h1, s1 := ComputeEmbedding("m", "p", 4, "hnsw", "vector_cosine_ops", "")
	h2, s2 := ComputeEmbedding("m", "p", 4, "hnsw", "vector_cosine_ops", "")
	if h1 != h2 || s1 != s2 {
		t.Errorf("not deterministic: %s/%s vs %s/%s", h1, s1, h2, s2)
	}
}

func TestEmbeddingSignatureID(t *testing.T) {
	if got := EmbeddingSignatureID("m1"); got != "embeddings_m1" {
		t.Errorf("EmbeddingSignatureID: got %q", got)
	}
}

// TestSQLiteVectorIndexFor describes a named per-model vec0 table: absent
// is the zero description, present carries method, metric, and the DDL as
// build params.
func TestSQLiteVectorIndexFor(t *testing.T) {
	sqlite_vec.Auto()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()

	desc, err := SQLiteVectorIndexFor(ctx, db, "vec_emb_x")
	if err != nil || desc.Exists() {
		t.Fatalf("absent table: %+v, %v", desc, err)
	}

	const cosine = "CREATE VIRTUAL TABLE vec_emb_x USING vec0(embedding float[4] distance_metric=cosine)"
	if _, err := db.Exec(cosine); err != nil {
		t.Fatal(err)
	}
	desc, err = SQLiteVectorIndexFor(ctx, db, "vec_emb_x")
	if err != nil {
		t.Fatal(err)
	}
	want := VectorIndexDescription{Method: "vec0", OpsClass: "cosine", BuildParams: cosine}
	if desc != want || !desc.Exists() {
		t.Errorf("cosine table: got %+v want %+v", desc, want)
	}

	if _, err := db.Exec("CREATE VIRTUAL TABLE vec_emb_y USING vec0(embedding float[4])"); err != nil {
		t.Fatal(err)
	}
	if desc, _ := SQLiteVectorIndexFor(ctx, db, "vec_emb_y"); desc.OpsClass != "l2" {
		t.Errorf("default-metric table: got %+v want ops l2", desc)
	}
}

// TestLoadUpsert_InTransaction pins that signature reads and writes join a
// caller's transaction: a rolled-back stamp never lands.
func TestLoadUpsert_InTransaction(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	if _, err := db.Exec(`CREATE TABLE index_signatures (
		signature_id TEXT PRIMARY KEY, signature_hash TEXT NOT NULL,
		computed_at TEXT NOT NULL, inputs_summary TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := Upsert(ctx, tx, DialectSQLite, "embeddings_m", "h", "s"); err != nil {
		t.Fatal(err)
	}
	if row, err := Load(ctx, tx, DialectSQLite, "embeddings_m"); err != nil || row == nil || row.SignatureHash != "h" {
		t.Fatalf("load inside tx: %+v, %v", row, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if row, err := Load(ctx, db, DialectSQLite, "embeddings_m"); err != nil || row != nil {
		t.Errorf("rolled-back stamp visible: %+v, %v", row, err)
	}
}

// TestVerifyFTS_PreDedupeStampMismatches pins the projection bump that
// dropped repeated segments from the FTS body: an index stamped by the
// previous projection ("v1") must report a mismatch on the next open, and
// verify re-stamps it so the open after that matches.
func TestVerifyFTS_PreDedupeStampMismatches(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	// A plain table stands in for the fts5 virtual table: verify reads only
	// its DDL from sqlite_master.
	const ddl = `CREATE TABLE objects_fts (id, projected_fts_body)`
	for _, stmt := range []string{ddl, `CREATE TABLE index_signatures (
		signature_id TEXT PRIMARY KEY, signature_hash TEXT NOT NULL,
		computed_at TEXT NOT NULL, inputs_summary TEXT NOT NULL DEFAULT '')`} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	oldHash := HashFTSInputs(SQLiteFTSTokenizer, "v1", ddl)
	if err := Upsert(ctx, db, DialectSQLite, FTSSignatureID, oldHash, "projection=v1"); err != nil {
		t.Fatal(err)
	}

	res, err := VerifyFTS(ctx, db, DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if res.Match || res.FirstBoot {
		t.Fatalf("v1 stamp: Match=%v FirstBoot=%v, want a mismatch (projection version not bumped?)", res.Match, res.FirstBoot)
	}
	if res.OldHash != oldHash {
		t.Errorf("OldHash = %s, want the v1 stamp %s", res.OldHash, oldHash)
	}
	if !strings.Contains(res.InputsSummary, "projection="+ProjectionVersion) {
		t.Errorf("summary %q does not name projection %s", res.InputsSummary, ProjectionVersion)
	}

	res, err = VerifyFTS(ctx, db, DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Match {
		t.Errorf("second open after re-stamp: Match=false")
	}
}
