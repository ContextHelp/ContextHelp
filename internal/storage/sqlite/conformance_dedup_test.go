package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ProfileDedup runs the per-profile content-hash dedup
// contract against a fresh SQLite database.
func TestConformance_ProfileDedup(t *testing.T) {
	storagetest.ProfileDedupConformance(t, newTestDriver(t))
}
