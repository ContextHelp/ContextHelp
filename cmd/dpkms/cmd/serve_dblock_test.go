package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dblock"
)

func sqliteServeConfig(dbPath string) *config.Config {
	return &config.Config{Storage: config.StorageConfig{Type: "sqlite", Path: dbPath}}
}

// A second serve on the same SQLite database fails fast, naming the
// database and the instance holding it.
func TestAcquireServeLock_SecondServeRefused(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	c := sqliteServeConfig(dbPath)

	first, err := acquireServeLock(c, "home")
	if err != nil {
		t.Fatalf("first serve lock: %v", err)
	}
	t.Cleanup(func() { _ = first.Release() })
	if first == nil {
		t.Fatal("first serve took no lock on a sqlite database")
	}

	_, err = acquireServeLock(c, "other")
	if !errors.Is(err, dblock.ErrHeld) {
		t.Fatalf("second serve lock = %v; want ErrHeld", err)
	}
	for _, want := range []string{dbPath, `instance "home"`, "dpkms shutdown"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// Releasing serve's lock (serve exiting) lets the next serve start.
func TestAcquireServeLock_ReleasedLockReacquirable(t *testing.T) {
	c := sqliteServeConfig(filepath.Join(t.TempDir(), "db.sqlite"))
	first, err := acquireServeLock(c, "home")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := acquireServeLock(c, "home")
	if err != nil {
		t.Fatalf("serve lock after release: %v", err)
	}
	_ = second.Release()
}

// Postgres and in-memory SQLite have no database file to guard: no
// lock, no sidecar.
func TestAcquireServeLock_NoFileNoLock(t *testing.T) {
	dir := t.TempDir()
	for name, c := range map[string]*config.Config{
		"postgres": {Storage: config.StorageConfig{Type: "postgres", Path: filepath.Join(dir, "pg")}},
		"memory":   sqliteServeConfig(":memory:"),
	} {
		t.Run(name, func(t *testing.T) {
			l, err := acquireServeLock(c, "home")
			if err != nil || l != nil {
				t.Fatalf("acquireServeLock = %v, %v; want nil, nil", l, err)
			}
		})
	}
	if _, err := os.Stat(dblock.Path(filepath.Join(dir, "pg"))); !os.IsNotExist(err) {
		t.Errorf("postgres serve created a lock sidecar: %v", err)
	}
}

// dpkms serve takes the lock before it opens the database: with the
// lock held it refuses before touching storage. Without BUS_TOKEN a
// serve that skipped the lock would stop at bus auth, long before any
// listener, and every port is OS-assigned besides.
func TestRunServe_RefusesWhileDatabaseLocked(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	held, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleDaemon, Instance: "home"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release() })

	t.Setenv("BUS_TOKEN", "")
	t.Setenv("DPKMS_BUS_TOKEN", "")
	prev := cfg
	t.Cleanup(func() { cfg = prev; resetAllFlags(serveCmd) })
	cfg = sqliteServeConfig(dbPath)
	for _, f := range []string{"port", "grpc-port", "cookie-bridge-port"} {
		if err := serveCmd.Flags().Set(f, "0"); err != nil {
			t.Fatal(err)
		}
	}

	err = runServe(serveCmd, nil)
	if !errors.Is(err, dblock.ErrHeld) {
		t.Fatalf("serve with the database locked = %v; want ErrHeld", err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Errorf("refused serve opened the database: %v", err)
	}
}
