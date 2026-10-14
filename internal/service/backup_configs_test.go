package service_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// seedBackupDB writes a minimal SQLite database to back up.
func seedBackupDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
}

// --include-configs archives the active config file once, as
// config.yaml, when it lives in the config dir being archived (dpkms
// always runs that way: its config dir is the config file's
// directory); the dir's other files still go under config/. Restore
// then writes it once, to the target's own config file.
func TestBackupIncludeConfigsArchivesActiveConfigOnce(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	seedBackupDB(t, dbPath)
	cfgDir := filepath.Join(dir, "contexthelp")
	cfgPath := filepath.Join(cfgDir, "dpkms.yaml")
	const body = "server:\n  port: 8180\n"
	if err := os.MkdirAll(filepath.Join(cfgDir, "keys"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "keys", "k.pub"), []byte("ssh-ed25519 AAAA"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := service.Backup(context.Background(), service.BackupOpts{
		DBPath:         dbPath,
		OutputDir:      filepath.Join(dir, "out"),
		ConfigPath:     cfgPath,
		IncludeConfigs: true,
		ConfigDir:      cfgDir,
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	assertArchiveContains(t, res.Path, "ctxt-backup/config.yaml", "ctxt-backup/config/keys/k.pub")
	assertArchiveNotContains(t, res.Path, "ctxt-backup/config/dpkms.yaml")

	l := newRestoreLayout(t)
	opts := l.opts(t, res.Path)
	opts.ConfigFile = "dpkms.yaml"
	rr, err := service.Restore(context.Background(), opts)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if rr.ConfigCount != 2 {
		t.Errorf("restored %d config files, want 2 (dpkms.yaml, keys/k.pub)", rr.ConfigCount)
	}
	if got, err := os.ReadFile(filepath.Join(l.configDir, "dpkms.yaml")); err != nil || string(got) != body {
		t.Errorf("restored dpkms.yaml = %q, %v; want %q", got, err, body)
	}
}

// A config file outside the archived config dir is not a duplicate:
// config.yaml and every file of the dir are archived.
func TestBackupIncludeConfigsKeepsConfigDirWhenConfigFileElsewhere(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	seedBackupDB(t, dbPath)
	cfgDir := filepath.Join(dir, "contexthelp")
	if err := os.MkdirAll(cfgDir, 0o750); err != nil {
		t.Fatal(err)
	}
	// Same name, different file.
	if err := os.WriteFile(filepath.Join(cfgDir, "dpkms.yaml"), []byte("dir: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "elsewhere", "dpkms.yaml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("active: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := service.Backup(context.Background(), service.BackupOpts{
		DBPath:         dbPath,
		OutputDir:      filepath.Join(dir, "out"),
		ConfigPath:     cfgPath,
		IncludeConfigs: true,
		ConfigDir:      cfgDir,
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	assertArchiveContains(t, res.Path, "ctxt-backup/config.yaml", "ctxt-backup/config/dpkms.yaml")
}
