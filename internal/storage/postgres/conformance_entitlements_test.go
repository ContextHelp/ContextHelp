//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_Entitlements runs the cross-driver entitlement store
// contract against a fresh Postgres database.
func TestConformance_Entitlements(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.EntitlementConformance(t, drv)
}
