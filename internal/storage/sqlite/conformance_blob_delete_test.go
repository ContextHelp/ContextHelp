package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_BlobDelete runs the cross-driver hard-delete blob
// contract against a fresh SQLite database.
func TestConformance_BlobDelete(t *testing.T) {
	storagetest.BlobDeleteConformance(t, newTestDriver(t))
}
