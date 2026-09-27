package service_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/service/servicetest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Every text search mode scopes to the requested profile on SQLite. The
// Postgres driver runs the same scenario under the integration tag.
func TestSearchModes_ScopeToProfile_SQLite(t *testing.T) {
	servicetest.RunHybridProfileScope(t, storageutil.NewTestDriver(t))
}
