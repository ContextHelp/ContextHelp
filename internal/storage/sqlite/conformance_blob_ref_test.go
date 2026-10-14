package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_BlobRefNotIndexed runs the cross-driver blob-ref
// indexing contract against a fresh SQLite database.
func TestConformance_BlobRefNotIndexed(t *testing.T) {
	storagetest.BlobRefNotIndexedConformance(t, newTestDriver(t))
}
