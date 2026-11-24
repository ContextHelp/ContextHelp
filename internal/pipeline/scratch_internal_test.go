package pipeline

import (
	"errors"
	"io/fs"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeInfo is a directory (or symlink) with the given mode, as another
// platform would report it.
type fakeInfo struct{ mode fs.FileMode }

func (f fakeInfo) Name() string       { return "scratch" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return nil }

func TestCheckRootInfo(t *testing.T) {
	yes := func(fs.FileInfo) bool { return true }
	no := func(fs.FileInfo) bool { return false }
	tests := []struct {
		name    string
		info    fs.FileInfo
		private func(fs.FileInfo) bool
		bad     bool
	}{
		// Windows reports directories as 0777 (or 0555): the mode must not
		// matter where the platform's private check accepts them.
		{"windows-style dir 0777", fakeInfo{fs.ModeDir | 0o777}, yes, false},
		{"windows-style dir 0555", fakeInfo{fs.ModeDir | 0o555}, yes, false},
		{"not private", fakeInfo{fs.ModeDir | 0o700}, no, true},
		{"symlink", fakeInfo{fs.ModeSymlink | 0o777}, yes, true},
		{"regular file", fakeInfo{0o600}, yes, true},
	}
	for _, tt := range tests {
		err := checkRootInfo("/x", tt.info, tt.private)
		if tt.bad != errors.Is(err, errUnsafeScratchRoot) {
			t.Errorf("%s: err = %v, want unsafe=%v", tt.name, err, tt.bad)
		}
	}
}

// The remedy names chmod where there are unix modes to fix, and not on
// Windows, which has no chmod.
func TestRemedyChmodHintIsUnixOnly(t *testing.T) {
	got := remedy(errUnsafeScratchRoot, "/x").Error()
	if has := strings.Contains(got, "chmod"); has == (runtime.GOOS == "windows") {
		t.Errorf("GOOS %s: remedy %q (chmod hint present = %v)", runtime.GOOS, got, has)
	}
}
