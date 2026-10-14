//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ChangeOrder runs the cross-driver change-order paging
// contract against a fresh Postgres database.
func TestConformance_ChangeOrder(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.ChangeOrderConformance(t, drv)
}

// TestConformance_Watermark runs the cross-driver watermark contract
// against a fresh Postgres database.
func TestConformance_Watermark(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.WatermarkConformance(t, drv)
}
