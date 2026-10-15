package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_Auth runs the cross-driver principal registry
// contract against a fresh SQLite database.
func TestConformance_Auth(t *testing.T) {
	storagetest.AuthConformance(t, newTestDriver(t))
}
