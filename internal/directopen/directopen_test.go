package directopen

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func target(t *testing.T, dbPath string) Target {
	return Target{
		Path:  dbPath,
		Blob:  config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{Path: filepath.Join(t.TempDir(), "blobs")}},
		Label: "cli",
	}
}

// migrated returns a migrated database path, written through Write.
func migrated(t *testing.T) string {
	t.Helper()
	db := filepath.Join(t.TempDir(), "db.sqlite")
	h, err := Write(context.Background(), target(t, db))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func shortRetry(t *testing.T, d time.Duration) {
	t.Helper()
	prev := CLIRetry
	CLIRetry = d
	t.Cleanup(func() { CLIRetry = prev })
}

// A write against a database a daemon (another process) holds fails at
// once, naming the holder and the way out; it does not wait out the
// retry budget meant for other commands.
func TestWrite_DaemonHeldFailsAtOnce(t *testing.T) {
	shortRetry(t, 5*time.Second)
	db := migrated(t)
	pid := startHolder(t, db)

	start := time.Now()
	_, err := Write(context.Background(), target(t, db))
	if waited := time.Since(start); waited > 2*time.Second {
		t.Errorf("waited %v on a daemon-held lock; want an immediate refusal", waited)
	}
	var ce *ContentionError
	if !errors.As(err, &ce) || !errors.Is(err, dblock.ErrHeld) {
		t.Fatalf("Write = %v; want *ContentionError wrapping ErrHeld", err)
	}
	for _, want := range []string{db, "daemon", fmt.Sprintf("pid %d", pid), `instance "helper"`, "since"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	for _, want := range []string{"dpkms shutdown", "read command"} {
		if !strings.Contains(ce.Fix(), want) {
			t.Errorf("fix %q does not mention %q", ce.Fix(), want)
		}
	}
}

// Another command's hold is waited out when it ends within the budget.
func TestWrite_CLIHeldRetried(t *testing.T) {
	shortRetry(t, 5*time.Second)
	db := migrated(t)
	held, err := dblock.Acquire(db, dblock.Info{Role: dblock.RoleCLI, Instance: "other"})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = held.Release()
	}()
	h, err := Write(context.Background(), target(t, db))
	if err != nil {
		t.Fatalf("Write after the other command released: %v", err)
	}
	_ = h.Close(context.Background())
}

// A command that keeps the lock past the budget makes the write fail
// with the contention error, not hang.
func TestWrite_CLIHeldPastBudget(t *testing.T) {
	shortRetry(t, 300*time.Millisecond)
	db := migrated(t)
	held, err := dblock.Acquire(db, dblock.Info{Role: dblock.RoleCLI, Instance: "other"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release() })
	start := time.Now()
	_, err = Write(context.Background(), target(t, db))
	var ce *ContentionError
	if !errors.As(err, &ce) {
		t.Fatalf("Write = %v; want *ContentionError", err)
	}
	if time.Since(start) < 300*time.Millisecond {
		t.Error("gave up before the retry budget")
	}
	if !strings.Contains(ce.Fix(), "retry") {
		t.Errorf("fix %q does not say to retry", ce.Fix())
	}
}

// The write handle holds the lock until it is closed.
func TestWrite_HoldsLockUntilClose(t *testing.T) {
	db := migrated(t)
	h, err := Write(context.Background(), target(t, db))
	if err != nil {
		t.Fatal(err)
	}
	if err := dblock.Check(db); !errors.Is(err, dblock.ErrHeld) {
		t.Fatalf("lock while the write handle is open: %v; want held", err)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := dblock.Check(db); err != nil {
		t.Fatalf("lock after close: %v; want free", err)
	}
}

// A read opens beside a daemon holding the lock, without waiting, and
// cannot write.
func TestRead_BypassesHeldLock(t *testing.T) {
	shortRetry(t, 5*time.Second)
	db := migrated(t)
	startHolder(t, db)

	start := time.Now()
	h, err := Read(context.Background(), target(t, db))
	if err != nil {
		t.Fatalf("Read beside a daemon: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("read waited %v on the lock", waited)
	}
	if _, _, err := h.Driver.Jobs().List(context.Background(), storage.JobFilter{}); err != nil {
		t.Errorf("read through the read handle: %v", err)
	}
	if err := h.Driver.Init(context.Background()); err == nil {
		t.Error("read handle migrated")
	}
}

// A read never creates or migrates a database: a missing one fails
// clearly and leaves nothing behind, lock sidecar included.
func TestRead_MissingDatabase(t *testing.T) {
	db := filepath.Join(t.TempDir(), "db.sqlite")
	_, err := Read(context.Background(), target(t, db))
	if !errors.Is(err, storageutil.ErrNoSchema) {
		t.Fatalf("Read(missing) = %v; want ErrNoSchema", err)
	}
	for _, p := range []string{db, dblock.Path(db)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("read created %s: %v", p, err)
		}
	}
}

// The access gate runs before anything else, for reads and writes
// alike: a protected or public instance is refused with no lock taken
// and no database opened; a private or unmarked one goes through.
func TestAccessGate(t *testing.T) {
	for _, access := range []string{"", config.AccessPrivate, config.AccessProtected, config.AccessPublic} {
		for mode, open := range map[string]func(context.Context, Target) (*Handle, error){"read": Read, "write": Write} {
			t.Run(access+"/"+mode, func(t *testing.T) {
				db := migrated(t)
				if access != "" {
					if err := accessmarker.Write(db, accessmarker.Marker{Access: access, Instance: "home"}); err != nil {
						t.Fatal(err)
					}
				}
				// A daemon holding the lock must not change the answer
				// for a refused class: no lock probe happens.
				held, err := dblock.Acquire(db, dblock.Info{Role: dblock.RoleDaemon, Instance: "home"})
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = held.Release() }()

				h, err := open(context.Background(), target(t, db))
				refused := access == config.AccessProtected || access == config.AccessPublic
				switch {
				case refused:
					if !errors.Is(err, accessmarker.ErrRefused) {
						t.Fatalf("%s open of a %s instance = %v; want ErrRefused", mode, access, err)
					}
					if errors.Is(err, dblock.ErrHeld) {
						t.Errorf("refusal reports lock contention: %v", err)
					}
				case mode == "read":
					if err != nil {
						t.Fatalf("read of a %q instance: %v", access, err)
					}
					_ = h.Close(context.Background())
				default:
					if !errors.Is(err, dblock.ErrHeld) {
						t.Fatalf("write of a %q instance with the daemon up = %v; want contention", access, err)
					}
				}
			})
		}
	}
}

// Postgres has no file: no marker, no lock; the gate passes it.
func TestCheckAccess_NoFile(t *testing.T) {
	for _, tg := range []Target{
		{Type: "postgres", Path: "postgres://127.0.0.1:1/none"},
		{Type: "sqlite", Path: ":memory:"},
	} {
		if err := CheckAccess(tg); err != nil {
			t.Errorf("CheckAccess(%s) = %v", tg.Type, err)
		}
	}
}
