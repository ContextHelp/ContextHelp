package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_TagCounts runs the cross-driver tag count contract
// against a fresh SQLite database.
func TestConformance_TagCounts(t *testing.T) {
	storagetest.TagCountsConformance(t, newTestDriver(t))
}
