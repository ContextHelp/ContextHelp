package service_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/service/servicetest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// The storage profile filter scopes every text search mode and Find treats
// its profile as a lens, on SQLite. The Postgres driver runs the same
// scenario under the integration tag.
func TestProfileFilterAndFindLens_SQLite(t *testing.T) {
	servicetest.RunProfileFilterAndFindLens(t, storageutil.NewTestDriver(t))
}
