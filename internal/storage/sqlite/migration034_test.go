package sqlite

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// seedLegacyPlaceholder re-creates the pre-037 state migrations 033-035
// operate on: the legacy-blob placeholder model and its stamp. Migration
// 037 deletes both on every database, so these historic migrations are
// exercised by calling their fns directly against the re-seeded rows.
func seedLegacyPlaceholder(t *testing.T, d *Driver) {
	t.Helper()
	if err := migrate033EmbeddingsBackfill(context.Background(), d); err != nil {
		t.Fatalf("seed legacy placeholder: %v", err)
	}
}

// expectedEmbeddingStamp computes the signature the stored row must carry:
// the legacy model identity hashed together with the LIVE vec_objects index
// description (post-034 that is vec0 with distance_metric=cosine).
func expectedEmbeddingStamp(t *testing.T, d *Driver, dim int) (modelID, hash, summary string) {
	t.Helper()
	ctx := context.Background()
	idx, err := indexsig.SQLiteVectorIndex(ctx, d.db)
	if err != nil {
		t.Fatalf("describe live vector index: %v", err)
	}
	modelID = LegacyEmbeddingModelID(dim)
	hash, summary = indexsig.ComputeEmbedding(
		modelID, "legacy-blob", dim, idx.Method, idx.OpsClass, idx.BuildParams)
	return modelID, hash, summary
}

// TestMigration034_RestampsAfterDDLSwap pins the re-stamp inside 034
// itself, isolated from 035's converging sweep: after the fn swaps the DDL
// it must rewrite the stamp, so a DB sitting at exactly version 34 already
// describes the cosine index it just built.
func TestMigration034_RestampsAfterDDLSwap(t *testing.T) {
	const dim = 4
	d := newTestDriverDim(t, dim)
	ctx := context.Background()
	seedLegacyPlaceholder(t, d)

	modelID, wantHash, _ := expectedEmbeddingStamp(t, d, dim)
	sigID := EmbeddingSignatureID(modelID)

	// Regress the stamp to the pre-034 (L2) provenance — the state a
	// ledger walking 033 → 034 sees when 034's fn starts.
	staleHash, staleSummary := indexsig.ComputeEmbedding(
		modelID, "legacy-blob", dim, "vec0", "l2",
		"CREATE VIRTUAL TABLE vec_objects USING vec0(id TEXT PRIMARY KEY, embedding float[4])")
	if err := UpsertIndexSignature(ctx, d.db, sigID, staleHash, staleSummary); err != nil {
		t.Fatalf("regress stamp: %v", err)
	}

	// Run 034's fn alone — no 035 sweep to paper over a missing re-stamp.
	if err := migrate034VecObjectsCosine(ctx, d); err != nil {
		t.Fatalf("re-run 034: %v", err)
	}

	row, err := LoadIndexSignature(ctx, d.db, sigID)
	if err != nil || row == nil {
		t.Fatalf("load after 034: row=%v err=%v", row, err)
	}
	if row.SignatureHash != wantHash {
		t.Errorf("034 left a stale stamp: hash = %q, want %q (summary %q)",
			row.SignatureHash, wantHash, row.InputsSummary)
	}
}

// TestMigration035_RestampsStaleSignatureOnUpgradedDB covers installs that
// applied 034 before it re-stamped: their stored signature still describes
// the dropped L2 table. Migration 035 recomputes the stamp from the live
// index so those DBs converge with fresh installs.
func TestMigration035_RestampsStaleSignatureOnUpgradedDB(t *testing.T) {
	const dim = 4
	d := newTestDriverDim(t, dim)
	ctx := context.Background()
	seedLegacyPlaceholder(t, d)

	modelID, wantHash, _ := expectedEmbeddingStamp(t, d, dim)
	sigID := EmbeddingSignatureID(modelID)

	// Regress the row to the stamp a pre-fix 034 left behind: the legacy
	// model identity hashed with the old L2 vec0 description.
	staleHash, staleSummary := indexsig.ComputeEmbedding(
		modelID, "legacy-blob", dim, "vec0", "l2",
		"CREATE VIRTUAL TABLE vec_objects USING vec0(id TEXT PRIMARY KEY, embedding float[4])")
	if err := UpsertIndexSignature(ctx, d.db, sigID, staleHash, staleSummary); err != nil {
		t.Fatalf("regress stamp: %v", err)
	}
	// Apply 035 as a DB that upgraded before 035 existed would.
	if err := migrate035RestampEmbeddingSignatures(ctx, d); err != nil {
		t.Fatalf("apply 035: %v", err)
	}

	row, err := LoadIndexSignature(ctx, d.db, sigID)
	if err != nil {
		t.Fatalf("load after 035: %v", err)
	}
	if row == nil {
		t.Fatalf("signature row vanished")
	}
	if row.SignatureHash != wantHash {
		t.Errorf("035 did not re-stamp: hash = %q, want %q (summary %q)",
			row.SignatureHash, wantHash, row.InputsSummary)
	}
}

// TestMigration035_Idempotent pins that re-running the re-stamp on an
// already-correct DB rewrites the same values — no error, no drift.
func TestMigration035_Idempotent(t *testing.T) {
	const dim = 4
	d := newTestDriverDim(t, dim)
	ctx := context.Background()
	seedLegacyPlaceholder(t, d)

	modelID, wantHash, _ := expectedEmbeddingStamp(t, d, dim)

	if err := migrate035RestampEmbeddingSignatures(ctx, d); err != nil {
		t.Fatalf("first re-run: %v", err)
	}
	if err := migrate035RestampEmbeddingSignatures(ctx, d); err != nil {
		t.Fatalf("second re-run: %v", err)
	}

	row, err := LoadIndexSignature(ctx, d.db, EmbeddingSignatureID(modelID))
	if err != nil || row == nil {
		t.Fatalf("load after re-runs: row=%v err=%v", row, err)
	}
	if row.SignatureHash != wantHash {
		t.Errorf("idempotent re-run drifted hash: %q, want %q", row.SignatureHash, wantHash)
	}
}
