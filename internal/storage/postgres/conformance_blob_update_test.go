//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_BlobUpdate runs the cross-driver update blob contract
// against a fresh Postgres database.
func TestConformance_BlobUpdate(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.BlobUpdateConformance(t, drv)
}
