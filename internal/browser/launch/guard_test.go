package launch

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// strayTokens mark a browser launch: headless Chrome switches and the
// default-browser handlers. Outside this package they mean a launch
// that bypasses its safety switches or platform table.
var strayTokens = []string{
	"--" + "headless",
	"--" + "dump-dom",
	"--" + "user-data-dir",
	"--" + "remote-debugging",
	"FileProtocol" + "Handler",
	"xdg-" + "open",
}

// scannedExt lists the source kinds that can exec a browser.
var scannedExt = map[string]bool{
	".go": true, ".sh": true, ".bash": true, ".ps1": true, ".py": true,
	".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true,
	".yml": true, ".yaml": true, ".mk": true,
}

// Every browser launch goes through this package: no other source file
// in the module carries a headless switch or a default-browser handler.
func TestNoBrowserLaunchOutsidePackage(t *testing.T) {
	root := moduleRoot(t)
	self, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "vendor", ".hop", ".tlc":
				return filepath.SkipDir
			}
			if path == self {
				return filepath.SkipDir
			}
			return nil
		}
		if !scannedExt[filepath.Ext(path)] && d.Name() != "Makefile" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, tok := range strayTokens {
			if i := bytes.Index(b, []byte(tok)); i >= 0 {
				line := 1 + bytes.Count(b[:i], []byte("\n"))
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d: %q outside internal/browser/launch; launch browsers through it", rel, line, tok)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// moduleRoot is the nearest directory above the test holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test")
		}
		dir = parent
	}
}
