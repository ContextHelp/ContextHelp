//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_OwnerPrincipal runs the cross-driver per-principal
// contract of the search history, saved search and resurfacing queue
// stores against a fresh Postgres database. The Postgres stores are
// placeholders, so each suite skips as a parity gap until they land.
func TestConformance_OwnerPrincipal(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.OwnerPrincipalConformance(t, drv)
}
