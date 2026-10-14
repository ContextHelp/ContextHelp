package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ChangeOrder runs the cross-driver change-order paging
// contract against a fresh SQLite database.
func TestConformance_ChangeOrder(t *testing.T) {
	storagetest.ChangeOrderConformance(t, newTestDriver(t))
}
