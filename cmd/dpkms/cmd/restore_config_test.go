package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// restoreFixture is a backup holding a database and the config file it
// ran with, and a target data dir and config file that dpkms restore
// resolves to.
type restoreFixture struct {
	archive, dataDir, dbPath, configPath string
}

const restoreFixtureConfig = "server:\n  port: 8180\nfederations: []\n"

func newRestoreFixture(t *testing.T) restoreFixture {
	t.Helper()
	ctx := context.Background()
	src := t.TempDir()
	srcDB := filepath.Join(src, "db.sqlite")
	drv, err := storageutil.Open("sqlite", srcDB, config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{Path: filepath.Join(src, "blobs")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := drv.Init(ctx); err != nil {
		t.Fatal(err)
	}
	drv.Close(ctx)
	srcCfg := filepath.Join(src, "dpkms.yaml")
	if err := os.WriteFile(srcCfg, []byte(restoreFixtureConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	backup, err := service.Backup(ctx, service.BackupOpts{
		DBPath:     srcDB,
		OutputDir:  filepath.Join(src, "out"),
		ConfigPath: srcCfg,
	})
	if err != nil {
		t.Fatal(err)
	}

	dst := t.TempDir()
	f := restoreFixture{
		archive:    backup.Path,
		dataDir:    filepath.Join(dst, "data"),
		dbPath:     filepath.Join(dst, "data", "db.sqlite"),
		configPath: filepath.Join(dst, "config", "contexthelp", "dpkms.yaml"),
	}
	prevCfg, prevFile := cfg, cfgFile
	t.Cleanup(func() { cfg, cfgFile = prevCfg, prevFile })
	cfg = &config.Config{Storage: config.StorageConfig{
		DataDir: f.dataDir,
		Type:    "sqlite",
		Path:    f.dbPath,
		Blob:    config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{Path: filepath.Join(dst, "blobs")}},
	}}
	cfgFile = f.configPath
	return f
}

// dpkms restore writes the backup's config file back to the config
// file this run resolves (--config here), under its own name.
func TestRestoreWritesConfigFile(t *testing.T) {
	f := newRestoreFixture(t)
	if err := runRestore(restoreCmd, []string{f.archive}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, err := os.ReadFile(f.configPath)
	if err != nil || string(got) != restoreFixtureConfig {
		t.Fatalf("config file = %q, %v; want %q", got, err, restoreFixtureConfig)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.configPath), "config.yaml")); !os.IsNotExist(err) {
		t.Errorf("archive entry name leaked into the config dir: %v", err)
	}
}

// dpkms restore refuses while a live instance of the data dir serves
// the target database, and writes nothing.
func TestRestoreRefusesWhileServing(t *testing.T) {
	f := newRestoreFixture(t)
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	runDir, err := config.RunDir(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	// The port only names the pidfile; nothing binds it.
	if err := pidfile.Write(runDir, pidfile.Info{
		PID: child.Process.Pid, Name: "home", Port: 47651, DBPath: f.dbPath, StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	err = runRestore(restoreCmd, []string{f.archive})
	if err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("want refusal naming the running instance, got %v", err)
	}
	if _, err := os.Stat(f.dbPath); !os.IsNotExist(err) {
		t.Errorf("refused restore wrote the database: %v", err)
	}
	if _, err := os.Stat(f.configPath); !os.IsNotExist(err) {
		t.Errorf("refused restore wrote the config file: %v", err)
	}
}

// dpkms restore --dry-run writes nothing: not even the data dir's run
// dir, which it only reads for live pidfiles.
func TestRestoreDryRunCreatesNoRunDir(t *testing.T) {
	f := newRestoreFixture(t)
	restoreCmd.InheritedFlags() // merge the root's persistent --dry-run
	if err := restoreCmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restoreCmd.Flags().Set("dry-run", "false") })

	if err := runRestore(restoreCmd, []string{f.archive}); err != nil {
		t.Fatalf("restore --dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "run")); !os.IsNotExist(err) {
		t.Errorf("dry run created the run dir: %v", err)
	}
	if _, err := os.Stat(f.dbPath); !os.IsNotExist(err) {
		t.Errorf("dry run wrote the database: %v", err)
	}
}
