package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_Outbox runs the cross-driver event outbox contract
// against a fresh SQLite database.
func TestConformance_Outbox(t *testing.T) {
	storagetest.OutboxConformance(t, newTestDriver(t))
}
