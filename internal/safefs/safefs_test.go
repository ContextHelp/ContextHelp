package safefs_test

import (
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
