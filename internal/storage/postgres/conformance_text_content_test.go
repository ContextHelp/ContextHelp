//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_TextContentDefault runs the cross-driver TextContent
// defaulting contract against a fresh Postgres database.
func TestConformance_TextContentDefault(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.TextContentDefaultConformance(t, drv, false)
}
