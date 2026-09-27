//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/service/servicetest"
)

// TestPostgres_SearchModes_ScopeToProfile runs the service-level profile
// scoping scenario (hybrid, semantic and FTS through the service) against a
// fresh database.
func TestPostgres_SearchModes_ScopeToProfile(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	servicetest.RunHybridProfileScope(t, drv)
}
