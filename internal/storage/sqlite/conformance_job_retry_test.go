package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_JobRetry runs the cross-driver job retry/requeue
// contract against a fresh SQLite database.
func TestConformance_JobRetry(t *testing.T) {
	storagetest.JobRetryConformance(t, newTestDriver(t))
}
