package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_BinaryContent runs the cross-driver binary content
// contract against a fresh SQLite database.
func TestConformance_BinaryContent(t *testing.T) {
	storagetest.BinaryContentConformance(t, newTestDriver(t))
}
