//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_TagFilter runs the cross-driver multi-tag selection
// contract against a fresh Postgres database.
func TestConformance_TagFilter(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.TagFilterConformance(t, drv)
}
