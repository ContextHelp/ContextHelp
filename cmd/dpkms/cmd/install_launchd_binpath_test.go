package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A Homebrew install runs from a versioned keg. The service must record
// the prefix shim, which survives `brew upgrade`, not the keg path.
func TestNewInstaller_RecordsHomebrewShimNotKeg(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("installer is darwin/linux only")
	}
	prefix := t.TempDir()
	keg := filepath.Join(prefix, "Cellar", "dpkms", "1.2.3", "bin", "dpkms")
	if err := os.MkdirAll(filepath.Dir(keg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keg, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // test binary must be executable
		t.Fatal(err)
	}
	shim := filepath.Join(prefix, "bin", "dpkms")
	if err := os.MkdirAll(filepath.Dir(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "Cellar", "dpkms", "1.2.3", "bin", "dpkms"), shim); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())

	for name, exe := range map[string]string{"keg": keg, "shim": shim} {
		t.Run(name, func(t *testing.T) {
			prev := currentExecutable
			currentExecutable = func() (string, error) { return exe, nil }
			t.Cleanup(func() { currentExecutable = prev })

			inst, err := newInstaller()
			if err != nil {
				t.Fatal(err)
			}
			var got string
			switch i := inst.(type) {
			case *darwinInstaller:
				got = i.binary
			case *linuxInstaller:
				got = i.binary
			}
			if filepath.Base(filepath.Dir(got)) != "bin" || filepath.Base(filepath.Dir(filepath.Dir(got))) == "1.2.3" {
				t.Fatalf("binary = %s, want the prefix shim", got)
			}
			gi, err := os.Stat(got)
			if err != nil {
				t.Fatal(err)
			}
			si, _ := os.Stat(shim)
			if !os.SameFile(gi, si) {
				t.Errorf("binary = %s, want %s", got, shim)
			}
		})
	}
}
