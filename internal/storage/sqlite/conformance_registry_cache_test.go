package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_RegistryCache runs the cross-driver registry cache
// contract against a fresh SQLite database.
func TestConformance_RegistryCache(t *testing.T) {
	storagetest.RegistryCacheConformance(t, newTestDriver(t))
}
