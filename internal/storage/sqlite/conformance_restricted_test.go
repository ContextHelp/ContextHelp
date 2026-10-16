package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_RestrictedViewer runs the restricted viewer's scoped
// read contract against a fresh SQLite database.
func TestConformance_RestrictedViewer(t *testing.T) {
	storagetest.RestrictedViewerConformance(t, newTestDriver(t))
}
