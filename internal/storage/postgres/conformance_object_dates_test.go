//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ObjectDateFilter runs the cross-driver created_at
// bound contract against a fresh Postgres database.
func TestConformance_ObjectDateFilter(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.ObjectDateFilterConformance(t, drv)
}
