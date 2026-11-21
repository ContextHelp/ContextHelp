package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_MatchBySQL runs the per-row predicate match contract
// against a fresh SQLite database.
func TestConformance_MatchBySQL(t *testing.T) {
	storagetest.MatchBySQLConformance(t, newTestDriver(t))
}
