//go:build integration

package postgres_test

import (
	"context"
	"testing"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestPostgres_Conformance_VectorRankFixture runs the cross-driver golden
// rank fixture against a fresh database through the per-model index. It
// skips until the driver's EmbeddingStore is implemented.
func TestPostgres_Conformance_VectorRankFixture(t *testing.T) {
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn)
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	if err := drv.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	storagetest.RunVectorRankFixture(t, drv)
}

// TestPostgres_Conformance_Search runs the full cross-driver search
// conformance suite with both legs claimed; the vector subtests skip until
// the driver's EmbeddingStore is implemented.
func TestPostgres_Conformance_Search(t *testing.T) {
	drv := freshVectorDriver(t, storagetest.VectorRankDimension)
	storagetest.RunSearchConformance(t, drv, storagetest.SearchCapabilities{
		FTS:     true,
		Vectors: true,
	})
}
