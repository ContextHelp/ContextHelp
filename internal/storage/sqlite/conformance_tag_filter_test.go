package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_TagFilter runs the cross-driver multi-tag selection
// contract against a fresh SQLite database.
func TestConformance_TagFilter(t *testing.T) {
	storagetest.TagFilterConformance(t, newTestDriver(t))
}
