package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_StatusTransition runs the cross-driver conditional
// status move contract against a fresh SQLite database.
func TestConformance_StatusTransition(t *testing.T) {
	storagetest.StatusTransitionConformance(t, newTestDriver(t))
}
