//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_Auth runs the cross-driver principal registry
// contract, its concurrent ticket redemption included, against a fresh
// Postgres database.
func TestConformance_Auth(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.AuthConformance(t, drv)
}
