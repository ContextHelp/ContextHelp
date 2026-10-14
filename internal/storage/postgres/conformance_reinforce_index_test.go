//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ReinforceReindex runs the cross-driver Reinforce
// re-index contract against a fresh Postgres database.
func TestConformance_ReinforceReindex(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.ReinforceReindexConformance(t, drv)
}
