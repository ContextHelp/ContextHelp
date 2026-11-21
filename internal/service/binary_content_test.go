package service_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/service/servicetest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Binary content (NUL, invalid UTF-8) through every write path keeps the
// row text-only and the blob exact, on SQLite. The Postgres driver runs
// the same scenario under the integration tag.
func TestBinaryContent_SQLite(t *testing.T) {
	servicetest.RunBinaryContent(t, storageutil.NewTestDriver(t))
}
