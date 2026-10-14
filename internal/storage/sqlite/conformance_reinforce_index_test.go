package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ReinforceReindex runs the cross-driver Reinforce
// re-index contract against a fresh SQLite database.
func TestConformance_ReinforceReindex(t *testing.T) {
	storagetest.ReinforceReindexConformance(t, newTestDriver(t))
}
