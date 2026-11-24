//go:build !unix

package pipeline

import (
	"io/fs"
	"testing"
)

// Windows reports every directory as 0777 or 0555; the private check must
// accept both.
func TestPrivateToUserWithoutUnixModes(t *testing.T) {
	for _, mode := range []fs.FileMode{fs.ModeDir | 0o777, fs.ModeDir | 0o555} {
		if !privateToUser(fakeInfo{mode}) || !ownedByCurrentUser(fakeInfo{mode}) {
			t.Errorf("mode %o refused", mode)
		}
	}
}
