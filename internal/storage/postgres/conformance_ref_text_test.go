//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_RefText runs the cross-driver ref-text repair contract
// against a fresh Postgres database.
func TestConformance_RefText(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.RefTextConformance(t, drv, storagetest.ProjectionDB{DB: drv.DB(), Dialect: indexsig.DialectPostgres})
}
