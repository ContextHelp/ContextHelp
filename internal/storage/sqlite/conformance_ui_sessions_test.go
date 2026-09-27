package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_UISessions runs the cross-driver web UI session store
// contract against a fresh SQLite database.
func TestConformance_UISessions(t *testing.T) {
	storagetest.UISessionConformance(t, newTestDriver(t))
}
