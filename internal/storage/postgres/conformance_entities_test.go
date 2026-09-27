//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ThinEntity runs the cross-driver thin-entity round trip
// against a fresh Postgres database.
func TestConformance_ThinEntity(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.ThinEntityConformance(t, drv)
}
