//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_BlobDelete runs the cross-driver hard-delete blob
// contract against a fresh Postgres database.
func TestConformance_BlobDelete(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.BlobDeleteConformance(t, drv)
}
