//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_MatchBySQL runs the per-row predicate match contract
// against a fresh Postgres database.
func TestConformance_MatchBySQL(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.MatchBySQLConformance(t, drv)
}
