package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_TextContentDefault runs the cross-driver TextContent
// defaulting contract against a fresh SQLite database.
func TestConformance_TextContentDefault(t *testing.T) {
	storagetest.TextContentDefaultConformance(t, newTestDriver(t))
}
