package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_EdgeBatch runs the cross-driver edge batch contract
// against a fresh SQLite database.
func TestConformance_EdgeBatch(t *testing.T) {
	storagetest.EdgeBatchConformance(t, newTestDriver(t))
}
