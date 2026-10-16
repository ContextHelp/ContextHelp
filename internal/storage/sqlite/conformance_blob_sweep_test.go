package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_BlobSweep runs the cross-driver orphan-blob sweep
// contract against a fresh SQLite database.
func TestConformance_BlobSweep(t *testing.T) {
	storagetest.BlobSweepConformance(t, newTestDriver(t))
}
