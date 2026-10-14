package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
)

// CTXT_DATA_DIR is a directory: ctxt discovers local instances in its
// run/ dir, keeps the `instance use` selection there, and defaults the
// database to a file inside it.
func TestDataDir_EnvIsDirectory(t *testing.T) {
	root := hermeticConfigEnv(t)
	dataDir := filepath.Join(root, "env-data")
	t.Setenv(config.EnvDataDir, dataDir)

	runDir := filepath.Join(dataDir, "run")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := pidfile.Write(runDir, pidfile.Info{PID: os.Getpid(), Name: "work", Port: 18094}); err != nil {
		t.Fatal(err)
	}

	if out, err := runWithConfigFlag(t, "instance", "use", "work"); err != nil {
		t.Fatalf("instance use work: %v\n%s", err, out)
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
	rel, err := filepath.Rel(root, cfg.Storage.Path)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("storage.path %q escapes the test tempdir %q", cfg.Storage.Path, root)
	}

	got, err := os.ReadFile(filepath.Join(runDir, "current-instance"))
	if err != nil {
		t.Fatalf("selection not saved under CTXT_DATA_DIR/run: %v", err)
	}
	if strings.TrimSpace(string(got)) != "work" {
		t.Errorf("current-instance = %q, want work", got)
	}
}
