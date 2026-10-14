// Package safefs writes files named by untrusted input — archive entry
// names, bundle entries — so they can only ever land inside the
// directory the caller chose.
package safefs

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LocalName checks name, a slash-separated entry name from an archive,
// and returns it in OS form. It refuses anything that is not a plain
// relative path under the extraction root: empty, absolute, "." or ".."
// elements, empty elements, backslashes (a separator on Windows) and
// NUL bytes.
func LocalName(name string) (string, error) {
	if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\\x00") {
		return "", fmt.Errorf("%q is not a local path", name)
	}
	rel := filepath.FromSlash(name)
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("%q is not a local path", name)
	}
	return rel, nil
}

// WriteFile writes r to rel inside dir, creating dir and rel's parent
// directories as needed. rel must pass LocalName. The write goes
// through an os.Root on dir, so a symlink already under dir cannot
// carry it outside either. Only the permission bits of perm are kept.
func WriteFile(dir, rel string, r io.Reader, perm fs.FileMode) error {
	if _, err := LocalName(filepath.ToSlash(rel)); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if parent := filepath.Dir(rel); parent != "." {
		if err := root.MkdirAll(parent, 0o750); err != nil {
			return err
		}
	}
	f, err := root.OpenFile(rel, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
