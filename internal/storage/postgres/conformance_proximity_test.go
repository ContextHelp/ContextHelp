//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ProximityDelete runs the cross-driver object-delete
// proximity contract against a fresh Postgres database.
func TestConformance_ProximityDelete(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.ProximityDeleteConformance(t, drv)
}
