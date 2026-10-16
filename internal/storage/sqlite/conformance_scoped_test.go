package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ScopedDriver runs the viewer-scoped driver contract
// against a fresh SQLite database.
func TestConformance_ScopedDriver(t *testing.T) {
	storagetest.ScopedDriverConformance(t, newTestDriver(t))
}
