//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_StatusTransition runs the cross-driver conditional
// status move contract against a fresh Postgres database.
func TestConformance_StatusTransition(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.StatusTransitionConformance(t, drv)
}
