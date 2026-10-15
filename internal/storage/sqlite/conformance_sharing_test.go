package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_Sharing runs the cross-driver sharing registry
// contract against a fresh SQLite database.
func TestConformance_Sharing(t *testing.T) {
	storagetest.SharingConformance(t, newTestDriver(t))
}
