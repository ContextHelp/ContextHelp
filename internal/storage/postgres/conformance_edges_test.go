//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_EdgeBatch runs the cross-driver edge batch contract
// against a fresh Postgres database.
func TestConformance_EdgeBatch(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.EdgeBatchConformance(t, drv)
}

// TestConformance_EdgeMetadata runs the cross-driver edge metadata
// round-trip contract against a fresh Postgres database.
func TestConformance_EdgeMetadata(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.EdgeMetadataConformance(t, drv)
}

// TestConformance_EdgeCreateMissing runs the cross-driver CreateMissing
// contract against a fresh Postgres database.
func TestConformance_EdgeCreateMissing(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.EdgeCreateMissingConformance(t, drv)
}
