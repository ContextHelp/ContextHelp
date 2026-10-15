package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ProximityDelete runs the cross-driver object-delete
// proximity contract against a fresh SQLite database.
func TestConformance_ProximityDelete(t *testing.T) {
	storagetest.ProximityDeleteConformance(t, newTestDriver(t))
}
