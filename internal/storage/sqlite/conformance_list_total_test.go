package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ListTotal runs the cross-driver list total and page
// contract against a fresh SQLite database.
func TestConformance_ListTotal(t *testing.T) {
	storagetest.ListTotalConformance(t, newTestDriver(t))
}
