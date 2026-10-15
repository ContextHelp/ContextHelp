package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_Metering runs the cross-driver metering store contract
// against a fresh SQLite database.
func TestConformance_Metering(t *testing.T) {
	storagetest.MeteringConformance(t, newTestDriver(t))
}
