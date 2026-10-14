package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// assertUnder fails unless path sits inside root, so a data-dir test can
// never have resolved the developer's real data directory.
func assertUnder(t *testing.T, root, path string) {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("%q escapes the test tempdir %q", path, root)
	}
}

// assertDir fails unless path exists as a directory.
func assertDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
}

// CTXT_DATA_DIR is a directory: dpkms creates it, keeps its run dir in
// it and puts the database file inside it. It used to also be the
// database path, so serve could not open SQLite on a directory.
func TestDataDir_EnvIsDirectory(t *testing.T) {
	root := hermeticConfigEnv(t)
	dataDir := filepath.Join(root, "env-data")
	t.Setenv(config.EnvDataDir, dataDir)

	if _, err := runWithConfigFlag(t, "ps"); err != nil {
		t.Fatalf("ps: %v", err)
	}
	if cfg == nil {
		t.Fatal("cfg not loaded")
	}
	if cfg.Storage.DataDir != dataDir {
		t.Errorf("storage.data_dir = %q, want %q", cfg.Storage.DataDir, dataDir)
	}
	if want := filepath.Join(dataDir, config.DBFileName); cfg.Storage.Path != want {
		t.Errorf("storage.path = %q, want the file %q inside the data dir", cfg.Storage.Path, want)
	}
	assertUnder(t, root, cfg.Storage.Path)
	assertDir(t, dataDir)
	assertDir(t, filepath.Join(dataDir, "run"))
}
