package service_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// A blob entry whose name has no separator can still escape the store
// through its shard directory: "..xx" is stored at <root>/../xx/..xx.
// Restore refuses such an entry before anything is written.
func TestRestoreRefusesBlobKeyOutsideStore(t *testing.T) {
	for _, key := range []string{"..xx", "....", "ab.meta.json", "nul", strings.Repeat("AB", 32)} {
		t.Run(key, func(t *testing.T) {
			l := newRestoreLayout(t)
			archive := filepath.Join(l.base, "evil.tar.gz")
			writeTarGz(t, archive, []tarEntry{
				{name: "ctxt-backup/ctxt.db", body: "db bytes"},
				{name: "ctxt-backup/blobs/" + key, body: "x"},
			})
			if _, err := service.Restore(ownerCtx(), l.opts(t, archive)); err == nil {
				t.Errorf("restore of blob entry %q: want error", key)
			}
			// The store is <base>/blobstore: "..xx" would land in <base>/xx.
			if _, err := os.Stat(filepath.Join(l.base, "xx")); !os.IsNotExist(err) {
				t.Errorf("restore wrote a blob outside the store (stat err %v)", err)
			}
			l.assertNothingWritten(t)
		})
	}
}
