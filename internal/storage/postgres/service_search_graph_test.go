//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/service/servicetest"
)

// TestPostgres_SearchGraph_GateAndProfile runs the search graph pipeline
// dpkms serves (service.Find with Trace, then searchgraph.Build over the driver)
// against a fresh database: profile-scoped objects, a hidden entity gone
// from every part of the document.
func TestPostgres_SearchGraph_GateAndProfile(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	servicetest.RunSearchGraphGate(t, drv)
}
