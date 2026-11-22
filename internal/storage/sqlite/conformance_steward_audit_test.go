package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_StewardAudit runs the cross-driver steward audit
// contract, each case against a fresh SQLite database.
func TestConformance_StewardAudit(t *testing.T) {
	storagetest.StewardAuditConformance(t, func(t *testing.T) storage.StorageDriver { return newTestDriver(t) })
}
