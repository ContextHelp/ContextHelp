package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_AmbientSession runs the ambient-capture storage
// contract (session id, job ambient context) against a fresh SQLite
// database.
func TestConformance_AmbientSession(t *testing.T) {
	storagetest.AmbientSessionConformance(t, newTestDriver(t))
}
