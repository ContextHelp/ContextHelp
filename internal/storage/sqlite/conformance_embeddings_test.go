package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_EmbeddingStore runs the cross-driver EmbeddingStore
// contract against a fresh SQLite database.
func TestConformance_EmbeddingStore(t *testing.T) {
	storagetest.EmbeddingStoreConformance(t, newTestDriver(t))
}
