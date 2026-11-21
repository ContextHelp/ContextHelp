//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_BinaryContent runs the cross-driver binary content
// contract against a fresh Postgres database.
func TestConformance_BinaryContent(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.BinaryContentConformance(t, drv)
}
