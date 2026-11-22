//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_StewardAudit runs the cross-driver steward audit
// contract, each case against a fresh Postgres database: concurrent
// appends there rely on the chain head's row lock.
func TestConformance_StewardAudit(t *testing.T) {
	storagetest.StewardAuditConformance(t, func(t *testing.T) storage.StorageDriver {
		drv, _ := freshIntegrationDriver(t)
		return drv
	})
}
