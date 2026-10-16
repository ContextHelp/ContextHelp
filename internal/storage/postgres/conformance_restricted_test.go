//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_RestrictedViewer runs the restricted viewer's scoped
// read contract against a fresh Postgres database.
func TestConformance_RestrictedViewer(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.RestrictedViewerConformance(t, drv)
}
