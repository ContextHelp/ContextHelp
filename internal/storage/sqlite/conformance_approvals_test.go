package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_Approvals runs the cross-driver approval store
// contract against a fresh SQLite database.
func TestConformance_Approvals(t *testing.T) {
	storagetest.ApprovalsConformance(t, newTestDriver(t))
}
