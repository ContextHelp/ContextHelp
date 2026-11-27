package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_GraphProvenance runs the cross-driver graph stamp
// contract against a fresh SQLite database.
func TestConformance_GraphProvenance(t *testing.T) {
	storagetest.GraphProvenanceConformance(t, newTestDriver(t))
}

// TestConformance_GraphMarker runs the cross-driver graph mark contract.
func TestConformance_GraphMarker(t *testing.T) {
	storagetest.GraphMarkerConformance(t, newTestDriver(t))
}

// TestConformance_GraphMarkOwnership runs the cross-driver mark ownership contract.
func TestConformance_GraphMarkOwnership(t *testing.T) {
	storagetest.GraphMarkOwnershipConformance(t, newTestDriver(t))
}
