package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_OwnerPrincipal runs the cross-driver per-principal
// contract of the search history, saved search and resurfacing queue
// stores against a fresh SQLite database.
func TestConformance_OwnerPrincipal(t *testing.T) {
	storagetest.OwnerPrincipalConformance(t, newTestDriver(t))
}
