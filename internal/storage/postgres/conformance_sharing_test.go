//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_Sharing runs the cross-driver sharing registry
// contract against a fresh Postgres database.
func TestConformance_Sharing(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.SharingConformance(t, drv)
}
