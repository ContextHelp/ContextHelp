//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_Outbox runs the cross-driver event outbox contract
// against a fresh Postgres database.
func TestConformance_Outbox(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.OutboxConformance(t, drv)
}
