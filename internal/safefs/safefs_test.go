package safefs_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/safefs"
)

func TestLocalName(t *testing.T) {
	for _, name := range []string{"config.yaml", "keys/a.pub", "a/b/c"} {
		if _, err := safefs.LocalName(name); err != nil {
			t.Errorf("LocalName(%q): %v", name, err)
		}
	}
	for _, name := range []string{
		"", ".", "..", "../x", "a/../../x", "a/../x", "/etc/passwd", "a//b", "a/./b", "a/",
		`..\x`, `a\b`, "a\x00b",
	} {
		if _, err := safefs.LocalName(name); err == nil {
			t.Errorf("LocalName(%q): want error", name)
		}
	}
}

func TestWriteFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	if err := safefs.WriteFile(dir, filepath.Join("keys", "a.pub"), strings.NewReader("k"), 0o4755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	p := filepath.Join(dir, "keys", "a.pub")
	got, err := os.ReadFile(p)
	if err != nil || string(got) != "k" {
		t.Fatalf("read back = %q, %v", got, err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode()&os.ModeSetuid != 0 {
		t.Errorf("setuid bit kept: %v", fi.Mode())
	}
}

// A symlink already under dir cannot carry a write outside it.
func TestWriteFileRefusesSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "cfg")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{dir, outside} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(dir, "keys")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "f"), filepath.Join(dir, "f")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{filepath.Join("keys", "evil"), "f", filepath.Join("..", "outside", "evil")} {
		if err := safefs.WriteFile(dir, rel, strings.NewReader("x"), 0o600); err == nil {
			t.Errorf("WriteFile(%q): want error", rel)
		}
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) > 0 {
		t.Errorf("wrote outside dir: %v", entries)
	}
}

// failReader returns n bytes of 'x' and then an error, standing in for
// a source that fails part-way through.
type failReader struct{ n int }

func (r *failReader) Read(p []byte) (int, error) {
	if r.n == 0 {
		return 0, errors.New("injected read failure")
	}
	k := min(len(p), r.n)
	for i := range k {
		p[i] = 'x'
	}
	r.n -= k
	return k, nil
}

func TestReplaceFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "db.sqlite")
	for _, f := range []string{"db.sqlite", "db.sqlite-wal", "db.sqlite-shm", "other-wal"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("old "+f), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := safefs.ReplaceFile(dir, "db.sqlite", strings.NewReader("new"), 0o4644, "-wal", "-shm", "-journal"); err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}
	if got, err := os.ReadFile(p); err != nil || string(got) != "new" {
		t.Fatalf("read back = %q, %v", got, err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode() != 0o644 {
		t.Errorf("mode = %v, want 0644 (perm bits only)", fi.Mode())
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"db.sqlite", "other-wal"}; strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("dir = %v, want %v (stale companions removed, nothing else)", names, want)
	}
}

// A write that fails part-way leaves the old file and its companions
// as they were, and no temporary file behind.
func TestReplaceFileFailureKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "db.sqlite")
	if err := os.WriteFile(p, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p+"-wal", []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := safefs.ReplaceFile(dir, "db.sqlite", &failReader{n: 3}, 0o600, "-wal")
	if err == nil {
		t.Fatal("ReplaceFile: want the injected error")
	}
	if got, _ := os.ReadFile(p); string(got) != "original" {
		t.Errorf("target = %q after failed replace, want original", got)
	}
	if got, _ := os.ReadFile(p + "-wal"); string(got) != "wal" {
		t.Errorf("companion = %q after failed replace, want kept", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("dir = %v, want only db.sqlite and its -wal", names)
	}
}

func TestReplaceFileRefusesNonLocal(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "d")
	for _, rel := range []string{filepath.Join("..", "evil"), "/abs"} {
		if err := safefs.ReplaceFile(dir, rel, strings.NewReader("x"), 0o600); err == nil {
			t.Errorf("ReplaceFile(%q): want error", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "evil")); !os.IsNotExist(err) {
		t.Errorf("wrote outside dir: %v", err)
	}
}
