//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_Approvals runs the cross-driver approval store
// contract against a fresh Postgres database.
func TestConformance_Approvals(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.ApprovalsConformance(t, drv)
}
