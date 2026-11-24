//go:build unix

package pipeline

import (
	"os"
	"testing"
)

func TestPrivateToUserUnix(t *testing.T) {
	for mode, want := range map[os.FileMode]bool{0o700: true, 0o500: false, 0o300: false, 0o600: false, 0o000: false, 0o750: false, 0o705: false, 0o755: false, 0o777: false} {
		dir := t.TempDir()
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := privateToUser(info); got != want {
			t.Errorf("mode %o: privateToUser = %v, want %v", mode, got, want)
		}
		_ = os.Chmod(dir, 0o700)
	}
}
