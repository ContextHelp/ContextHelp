//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_Metering runs the cross-driver metering store contract
// against a fresh Postgres database.
func TestConformance_Metering(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.MeteringConformance(t, drv)
}
