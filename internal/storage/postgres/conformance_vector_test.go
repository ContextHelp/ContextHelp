//go:build integration

package postgres_test

import (
	"context"
	"testing"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestPostgres_Conformance_VectorRankFixture runs the cross-driver golden
// rank fixture against a fresh database whose embedding column is created
// at the fixture dimension, exercising the pgvector cosine path end to end.
func TestPostgres_Conformance_VectorRankFixture(t *testing.T) {
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn)
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	drv.SetVectorDimension(storagetest.VectorRankDimension)
	if err := drv.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	storagetest.RunVectorRankFixture(t, drv)
}

// TestPostgres_Conformance_Search runs the full cross-driver search
// conformance suite with the Postgres capabilities flipped on: schema,
// methods, sanitizer seam, and signatures have all landed, so FTS and
// Vectors assert for real instead of rendering as skips. Dimension
// enforcement is strict — the typmod'd pgvector column refuses mismatched
// writes.
func TestPostgres_Conformance_Search(t *testing.T) {
	drv := freshVectorDriver(t, storagetest.VectorRankDimension)
	storagetest.RunSearchConformance(t, drv, storagetest.SearchCapabilities{
		FTS:                    true,
		Vectors:                true,
		StrictDimensionOnWrite: true,
	})
}
