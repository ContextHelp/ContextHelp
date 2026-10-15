//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ProfileDedup runs the per-profile content-hash dedup
// contract against a fresh Postgres database.
func TestConformance_ProfileDedup(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.ProfileDedupConformance(t, drv)
}
