//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_BlobRefNotIndexed runs the cross-driver blob-ref
// indexing contract against a fresh Postgres database.
func TestConformance_BlobRefNotIndexed(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.BlobRefNotIndexedConformance(t, drv)
}
