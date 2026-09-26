// Package binpath resolves the path a service manager (launchd,
// systemd) should record for the running binary.
//
// A Homebrew install runs from a versioned keg such as
// /opt/homebrew/Cellar/ctxt/1.2.3/bin/ctxt. Recording that path pins the
// service to one version: the next `brew upgrade` removes the keg and
// the service fails to start. The prefix shim (/opt/homebrew/bin/ctxt)
// follows upgrades, so it is recorded instead whenever it points at the
// same file.
package binpath

import (
	"os"
	"path/filepath"
	"strings"
)

// Stable returns the path to record for exe, the value of
// os.Executable().
//
// When exe resolves (through symlinks) into a Homebrew keg,
// <prefix>/Cellar/<formula>/<version>/..., and <prefix>/bin/<name>
// exists and resolves to the same file, Stable returns that shim.
// In every other case it returns exe unchanged.
func Stable(exe string) string {
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe
	}
	prefix, ok := cellarPrefix(resolved)
	if !ok {
		return exe
	}
	shim := filepath.Join(prefix, "bin", filepath.Base(resolved))
	if !sameFile(shim, resolved) {
		return exe
	}
	return shim
}

// cellarPrefix returns the directory holding the last "Cellar" element
// of path, e.g. /opt/homebrew for /opt/homebrew/Cellar/ctxt/1.2.3/bin/ctxt.
func cellarPrefix(path string) (string, bool) {
	sep := string(filepath.Separator)
	marker := sep + "Cellar" + sep
	i := strings.LastIndex(path, marker)
	if i < 0 {
		return "", false
	}
	prefix := path[:i]
	if prefix == "" {
		prefix = sep
	}
	return prefix, true
}

func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}
