package representation_test

import (
	"go/build"
	"strings"
	"testing"
)

// representation sits below the blob store: depending on storage/blob would
// pull its cloud and config dependencies into the projection and every
// storage backend. It reads the artifact keys from storage.
func TestRepresentationDoesNotImportBlob(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range pkg.Imports {
		if strings.HasSuffix(imp, "/storage/blob") || strings.Contains(imp, "/storage/blob/") {
			t.Errorf("representation imports %s", imp)
		}
	}
}
