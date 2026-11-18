//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/service/servicetest"
)

// TestPostgres_ProfileFilterAndFindLens runs the service-level profile
// scenario (storage profile filter on hybrid, semantic and FTS; Find
// treating its profile as a lens) against a fresh database.
func TestPostgres_ProfileFilterAndFindLens(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	servicetest.RunProfileFilterAndFindLens(t, drv)
}
