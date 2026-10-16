//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_BlobSweep runs the cross-driver orphan-blob sweep
// contract against a fresh Postgres database.
func TestConformance_BlobSweep(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.BlobSweepConformance(t, drv)
}
