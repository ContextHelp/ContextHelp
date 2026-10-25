//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_RegistryCache runs the cross-driver registry cache
// contract against a fresh Postgres database.
func TestConformance_RegistryCache(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.RegistryCacheConformance(t, drv)
}
