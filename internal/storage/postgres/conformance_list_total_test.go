//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ListTotal runs the cross-driver list total and page
// contract against a fresh Postgres database.
func TestConformance_ListTotal(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.ListTotalConformance(t, drv)
}
