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

// TestConformance_EdgeMetadata runs the cross-driver edge metadata
// round-trip contract against a fresh SQLite database.
func TestConformance_EdgeMetadata(t *testing.T) {
	storagetest.EdgeMetadataConformance(t, newTestDriver(t))
}

// TestConformance_EdgeCreateMissing runs the cross-driver CreateMissing
// contract against a fresh SQLite database.
func TestConformance_EdgeCreateMissing(t *testing.T) {
	storagetest.EdgeCreateMissingConformance(t, newTestDriver(t))
}
