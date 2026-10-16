package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_BlobUpdate runs the cross-driver update blob contract
// against a fresh SQLite database.
func TestConformance_BlobUpdate(t *testing.T) {
	storagetest.BlobUpdateConformance(t, newTestDriver(t))
}
