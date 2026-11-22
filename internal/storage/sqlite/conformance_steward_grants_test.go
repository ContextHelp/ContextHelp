package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_StewardGrants runs the cross-driver steward grant
// store contract against a fresh SQLite database.
func TestConformance_StewardGrants(t *testing.T) {
	storagetest.StewardGrantsConformance(t, newTestDriver(t))
}
