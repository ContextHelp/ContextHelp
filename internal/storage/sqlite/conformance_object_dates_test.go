package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ObjectDateFilter runs the cross-driver created_at
// bound contract against a fresh SQLite database.
func TestConformance_ObjectDateFilter(t *testing.T) {
	storagetest.ObjectDateFilterConformance(t, newTestDriver(t))
}
