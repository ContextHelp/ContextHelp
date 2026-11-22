//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_StewardGrants runs the cross-driver steward grant
// store contract against a fresh Postgres database.
func TestConformance_StewardGrants(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.StewardGrantsConformance(t, drv)
}
