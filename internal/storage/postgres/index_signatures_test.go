//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// TestPostgresFTSSignature_VerifyLifecycle pins ADR-070 detection on the
// Postgres backend: first boot stamps, second boot matches, and the hash
// inputs are the catalog-derived facts (regconfig, projection version,
// generated-column expression, index DDL).
func TestPostgresFTSSignature_VerifyLifecycle(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()

	res, err := indexsig.VerifyFTS(ctx, drv.DB(), indexsig.DialectPostgres)
	if err != nil {
		t.Fatalf("first verify: %v", err)
	}
	if !res.FirstBoot || res.Match || res.OldHash != "" || res.NewHash == "" {
		t.Fatalf("first boot result off: %+v", res)
	}
	for _, part := range []string{"regconfig=simple", "projection=", "genexpr-len=", "indexddl-len="} {
		if !strings.Contains(res.InputsSummary, part) {
			t.Errorf("inputs summary missing %q: %s", part, res.InputsSummary)
		}
	}

	res2, err := indexsig.VerifyFTS(ctx, drv.DB(), indexsig.DialectPostgres)
	if err != nil {
		t.Fatalf("second verify: %v", err)
	}
	if !res2.Match || res2.FirstBoot {
		t.Fatalf("second boot should match: %+v", res2)
	}
	if res2.OldHash != res.NewHash {
		t.Errorf("stored hash not persisted: old=%s new=%s", res2.OldHash, res.NewHash)
	}

	stored, err := indexsig.Load(ctx, drv.DB(), indexsig.DialectPostgres, indexsig.FTSSignatureID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if stored == nil || stored.SignatureHash != res.NewHash {
		t.Fatalf("stored row off: %+v", stored)
	}
	if stored.ComputedAt.IsZero() {
		t.Error("computed_at not round-tripped from TIMESTAMPTZ")
	}
}

// TestPostgresVectorIndexDescription pins the catalog reader feeding the
// embedding signature: an indexable dimension reports the HNSW cosine index;
// a driver migrated above the HNSW ceiling reports the seq-scan fallback.
func TestPostgresVectorIndexDescription(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	desc, err := indexsig.PostgresVectorIndex(context.Background(), drv.DB())
	if err != nil {
		t.Fatalf("describe (indexed): %v", err)
	}
	if desc.Method != "hnsw" || desc.OpsClass != "vector_cosine_ops" {
		t.Errorf("indexed description: got %+v want hnsw/vector_cosine_ops", desc)
	}

	big := freshVectorDriver(t, 3000) // above HNSW ceiling: no index built
	desc, err = indexsig.PostgresVectorIndex(context.Background(), big.DB())
	if err != nil {
		t.Fatalf("describe (unindexed): %v", err)
	}
	if desc.Method != "seqscan" {
		t.Errorf("unindexed description: got %+v want seqscan fallback", desc)
	}
}

// TestPostgresMigrate_StampsVectorSignature pins that migration stamps the
// embeddings_<default-model> signature row from the live index description —
// the Postgres index_signatures table stops being write-only.
func TestPostgresMigrate_StampsVectorSignature(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	ctx := context.Background()

	var modelID string
	if err := drv.DB().QueryRowContext(ctx,
		`SELECT model_id FROM embedding_models WHERE is_default = 1`).Scan(&modelID); err != nil {
		t.Fatalf("default model: %v", err)
	}

	row, err := indexsig.Load(ctx, drv.DB(), indexsig.DialectPostgres, indexsig.EmbeddingSignatureID(modelID))
	if err != nil {
		t.Fatalf("load embedding signature: %v", err)
	}
	if row == nil {
		t.Fatal("no embedding signature stamped at migration")
	}
	for _, part := range []string{"model_id=" + modelID, "method=hnsw", "ops=vector_cosine_ops"} {
		if !strings.Contains(row.InputsSummary, part) {
			t.Errorf("inputs summary missing %q: %s", part, row.InputsSummary)
		}
	}

	// Re-migrating must be idempotent, not duplicate or error.
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
}
