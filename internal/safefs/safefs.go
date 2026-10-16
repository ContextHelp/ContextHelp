// Package safefs writes files named by untrusted input — archive entry
// names, bundle entries — so they can only ever land inside the
// directory the caller chose.
package safefs

import (
	"crypto/rand"
	"errors"
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

// ReplaceFile atomically replaces rel inside dir with r's content,
// through an os.Root on dir like WriteFile. The content goes to a
// temporary file beside rel, is fsynced and then renamed over rel, so
// a failed or interrupted write leaves the old rel as it was (the
// temporary file is removed on failure). After the rename, each
// companion rel+suffix (for every suffix in staleSuffixes) is removed,
// and the directory is fsynced so the rename and removals are
// durable. Only the permission bits of perm are kept.
func ReplaceFile(dir, rel string, r io.Reader, perm fs.FileMode, staleSuffixes ...string) error {
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
	parent := filepath.Dir(rel)
	if parent != "." {
		if err := root.MkdirAll(parent, 0o750); err != nil {
			return err
		}
	}
	tmp := filepath.Join(parent, "."+filepath.Base(rel)+".tmp-"+rand.Text())
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm.Perm())
	if err != nil {
		return err
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = f.Close()
			_ = root.Remove(tmp)
		}
	}()
	if _, err := io.Copy(f, r); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := root.Rename(tmp, rel); err != nil {
		return err
	}
	renamed = true
	for _, suffix := range staleSuffixes {
		if err := root.Remove(rel + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove stale %s: %w", rel+suffix, err)
		}
	}
	d, err := root.Open(parent)
	if err != nil {
		return err
	}
	serr := d.Sync()
	if cerr := d.Close(); serr == nil {
		serr = cerr
	}
	return serr
}
