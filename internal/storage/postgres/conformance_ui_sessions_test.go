//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_UISessions runs the cross-driver web UI session store
// contract against a fresh Postgres database.
func TestConformance_UISessions(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.UISessionConformance(t, drv)
}
