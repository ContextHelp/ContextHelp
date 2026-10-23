package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A command that opens the store directly, pointed at a path SQLite cannot
// open, fails on the built binary: non-zero exit, a message naming the
// path and the cause, and no advice to rebuild with fts5.
func TestE2EStorageOpenFailureExit(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("CTXT_DATA_DIR", "")
	t.Setenv("CTXT_INSTANCE", "")

	notADir := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(notADir, "store.sqlite")
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("storage:\n  type: sqlite\n  path: "+dbPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, out := runBuiltCtxt(t, "--config", configPath, "remind", "list")
	if code == 0 {
		t.Fatalf("ctxt remind list on an unopenable store: exit 0, want non-zero\n%s", out)
	}
	for _, want := range []string{dbPath, "not a directory"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not name %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "fts5") {
		t.Errorf("open failure blamed on fts5:\n%s", out)
	}
}
