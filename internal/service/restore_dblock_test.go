package service_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// Restore refuses, dry run included and with nothing written, while a
// process (a dpkms serve) holds the target database's lock, whatever
// the pidfiles say; once the lock is released it goes ahead.
func TestRestoreRefusesWhileDatabaseLocked(t *testing.T) {
	l := newRestoreLayout(t)
	seedTargetDB(t, l, map[string]string{"db.sqlite": "live database"})
	archive := filepath.Join(l.base, "b.tar.gz")
	writeTarGz(t, archive, []tarEntry{{name: "ctxt-backup/ctxt.db", body: "restored"}})

	held, err := dblock.Acquire(l.dbPath, dblock.Info{Role: dblock.RoleDaemon, Instance: "home"})
	if err != nil {
		t.Fatal(err)
	}
	opts := l.opts(t, archive)
	opts.RunDir = filepath.Join(l.base, "run") // no pidfiles: the lock alone refuses

	_, err = service.Restore(context.Background(), opts)
	if !errors.Is(err, dblock.ErrHeld) {
		_ = held.Release()
		t.Fatalf("restore with the database locked = %v; want ErrHeld", err)
	}
	if got := targetFiles(t, l); got["db.sqlite"] != "live database" {
		t.Errorf("db.sqlite = %q after refusal, want the live database", got["db.sqlite"])
	}
	dry := opts
	dry.DryRun = true
	if _, err := service.Restore(context.Background(), dry); !errors.Is(err, dblock.ErrHeld) {
		t.Errorf("dry run with the database locked = %v; want the same refusal", err)
	}

	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Restore(context.Background(), opts); err != nil {
		t.Fatalf("restore after release: %v", err)
	}
	if got := targetFiles(t, l); got["db.sqlite"] != "restored" {
		t.Errorf("db.sqlite = %q, want restored", got["db.sqlite"])
	}
}

// Restore holds the database lock while it writes, so a serve
// starting mid-restore cannot open the half-restored database.
func TestRestoreHoldsDatabaseLockWhileWriting(t *testing.T) {
	l := newRestoreLayout(t)
	archive := filepath.Join(l.base, "b.tar.gz")
	writeTarGz(t, archive, []tarEntry{{name: "ctxt-backup/ctxt.db", body: "restored"}})

	var during error
	service.SetDBSourceHook(t, func(r io.Reader) io.Reader {
		during = dblock.Check(l.dbPath)
		return r
	})
	if _, err := service.Restore(context.Background(), l.opts(t, archive)); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !errors.Is(during, dblock.ErrHeld) {
		t.Fatalf("lock while restore wrote the database: %v; want held", during)
	}
	if err := dblock.Check(l.dbPath); err != nil {
		t.Errorf("lock after restore returned: %v; want released", err)
	}
}

// A dry run against a database no process ever locked creates no lock
// sidecar: it writes nothing.
func TestRestoreDryRunCreatesNoLockFile(t *testing.T) {
	l := newRestoreLayout(t)
	seedTargetDB(t, l, map[string]string{"db.sqlite": "live database"})
	archive := filepath.Join(l.base, "b.tar.gz")
	writeTarGz(t, archive, []tarEntry{{name: "ctxt-backup/ctxt.db", body: "restored"}})
	opts := l.opts(t, archive)
	opts.DryRun = true
	if _, err := service.Restore(context.Background(), opts); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if _, err := os.Stat(dblock.Path(l.dbPath)); !os.IsNotExist(err) {
		t.Errorf("dry run created the lock sidecar: %v", err)
	}
}
