package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// A Homebrew install runs from a versioned keg; the agents must exec
// the prefix shim, which survives `brew upgrade`.
func TestDefaultScheduleEnv_RecordsHomebrewShimNotKeg(t *testing.T) {
	prefix := t.TempDir()
	keg := filepath.Join(prefix, "Cellar", "ctxt", "1.2.3", "bin", "ctxt")
	shim := filepath.Join(prefix, "bin", "ctxt")
	for _, d := range []string{filepath.Dir(keg), filepath.Dir(shim)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(keg, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // test binary must be executable
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "Cellar", "ctxt", "1.2.3", "bin", "ctxt"), shim); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())

	for name, exe := range map[string]string{"keg": keg, "shim": shim} {
		t.Run(name, func(t *testing.T) {
			prev := scheduleExecutable
			scheduleExecutable = func() (string, error) { return exe, nil }
			t.Cleanup(func() { scheduleExecutable = prev })

			env, err := defaultScheduleEnv()
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(filepath.Dir(filepath.Dir(env.binary))) == "1.2.3" {
				t.Fatalf("binary = %s, want the prefix shim", env.binary)
			}
			gi, err := os.Stat(env.binary)
			if err != nil {
				t.Fatal(err)
			}
			si, _ := os.Stat(shim)
			if !os.SameFile(gi, si) {
				t.Errorf("binary = %s, want %s", env.binary, shim)
			}
		})
	}
}
