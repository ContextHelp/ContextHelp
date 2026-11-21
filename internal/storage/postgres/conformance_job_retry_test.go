//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_JobRetry runs the cross-driver job retry/requeue
// contract against a fresh Postgres database.
func TestConformance_JobRetry(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.JobRetryConformance(t, drv)
}
