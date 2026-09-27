package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ThinEntity runs the cross-driver thin-entity round trip
// against a fresh SQLite database.
func TestConformance_ThinEntity(t *testing.T) {
	storagetest.ThinEntityConformance(t, newTestDriver(t))
}
