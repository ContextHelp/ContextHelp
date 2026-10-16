package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_Entitlements runs the cross-driver entitlement store
// contract against a fresh SQLite database.
func TestConformance_Entitlements(t *testing.T) {
	storagetest.EntitlementConformance(t, newTestDriver(t))
}
