package binpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// brewTree builds <prefix>/Cellar/<name>/1.2.3/bin/<name> and returns
// the prefix and the Cellar binary.
func brewTree(t *testing.T, name string) (prefix, cellarBin string) {
	t.Helper()
	prefix = t.TempDir()
	cellarBin = filepath.Join(prefix, "Cellar", name, "1.2.3", "bin", name)
	if err := os.MkdirAll(filepath.Dir(cellarBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cellarBin, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // test binary must be executable
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	return prefix, cellarBin
}

func link(t *testing.T, target, at string) {
	t.Helper()
	if err := os.Symlink(target, at); err != nil {
		t.Fatal(err)
	}
}

func sameFileT(t *testing.T, a, b string) bool {
	t.Helper()
	ai, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	bi, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(ai, bi)
}

func TestStable_KegPathPrefersPrefixShim(t *testing.T) {
	prefix, cellarBin := brewTree(t, "ctxt")
	shim := filepath.Join(prefix, "bin", "ctxt")
	// Homebrew links bin/<name> to the keg with a relative path.
	link(t, filepath.Join("..", "Cellar", "ctxt", "1.2.3", "bin", "ctxt"), shim)

	for name, exe := range map[string]string{
		"invoked from the Cellar":   cellarBin,
		"invoked through the shim":  shim,
		"invoked through /opt link": filepath.Join(prefix, "opt-link"),
	} {
		t.Run(name, func(t *testing.T) {
			if name == "invoked through /opt link" {
				link(t, cellarBin, exe)
			}
			got := Stable(exe)
			if strings.Contains(strings.TrimPrefix(got, prefix), "Cellar") {
				t.Fatalf("Stable(%s) = %s, want the prefix shim", exe, got)
			}
			if !sameFileT(t, got, shim) {
				t.Errorf("Stable(%s) = %s, want %s", exe, got, shim)
			}
			if resolved, _ := filepath.EvalSymlinks(got); resolved == got {
				t.Errorf("Stable returned a resolved path %s, want the shim itself", got)
			}
		})
	}
}

func TestStable_NoShimKeepsExecutable(t *testing.T) {
	_, cellarBin := brewTree(t, "ctxt")
	if got := Stable(cellarBin); got != cellarBin {
		t.Errorf("Stable = %s, want %s unchanged when no shim exists", got, cellarBin)
	}
}

func TestStable_ShimToAnotherVersionIgnored(t *testing.T) {
	prefix, cellarBin := brewTree(t, "ctxt")
	other := filepath.Join(prefix, "Cellar", "ctxt", "9.9.9", "bin", "ctxt")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("other"), 0o755); err != nil { //nolint:gosec // test binary must be executable
		t.Fatal(err)
	}
	link(t, other, filepath.Join(prefix, "bin", "ctxt"))
	if got := Stable(cellarBin); got != cellarBin {
		t.Errorf("Stable = %s, want %s: the shim resolves to a different file", got, cellarBin)
	}
}

func TestStable_OutsideKegReturnedAsIs(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real", "ctxt")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("x"), 0o755); err != nil { //nolint:gosec // test binary must be executable
		t.Fatal(err)
	}
	viaLink := filepath.Join(dir, "ctxt")
	link(t, real, viaLink)

	for _, exe := range []string{real, viaLink, filepath.Join(dir, "does-not-exist")} {
		if got := Stable(exe); got != exe {
			t.Errorf("Stable(%s) = %s, want it unchanged outside a Cellar", exe, got)
		}
	}
}
