package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

// testBlobStore returns a local blob store rooted in a temp directory.
func testBlobStore(t testing.TB) storage.BlobStore {
	t.Helper()
	bs, err := local.New(t.TempDir())
	if err != nil {
		t.Fatalf("new test blob store: %v", err)
	}
	return bs
}
