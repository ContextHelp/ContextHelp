//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ScopedDriver runs the viewer-scoped driver contract
// against a fresh Postgres database.
func TestConformance_ScopedDriver(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.ScopedDriverConformance(t, drv)
}
