//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_AmbientSession runs the ambient-capture storage
// contract (session id, job ambient context) against a fresh Postgres
// database.
func TestConformance_AmbientSession(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.AmbientSessionConformance(t, drv)
}
