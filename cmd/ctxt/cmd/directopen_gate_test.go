package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/directopen"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/spf13/viper"
	"hop.top/kit/go/console/output"
)

// gateInstance is the instance name the direct-open gate tests route to.
const gateInstance = "gated"

// instanceDB registers a running local instance named gateInstance
// serving a fresh, migrated database, and returns that database's path.
// The configured database (db) stays separate, so a hit on it would not
// count.
func instanceDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "instance.db")
	d, err := storageutil.Open("sqlite", path, config.BlobConfig{
		Backend: "local", Local: config.BlobLocalConfig{Path: t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	registerInstance(t, path)
	return path
}

// registerInstance writes the pidfile that names path's daemon.
func registerInstance(t *testing.T, path string) {
	t.Helper()
	runDir, err := config.RunDir(testDataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := pidfile.Write(runDir, pidfile.Info{
		PID: os.Getpid(), Name: gateInstance, Port: 59177, DBPath: path, StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

// holdAsDaemon takes path's database lock as the daemon would, for the
// rest of the test.
func holdAsDaemon(t *testing.T, path string) {
	t.Helper()
	l, err := dblock.Acquire(path, dblock.Info{Role: dblock.RoleDaemon, Instance: gateInstance})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Release() })
}

// A protected instance is closed to direct opens: reads and writes both
// refuse, exit 5, and the refusal says how to get in.
func TestInstanceGate_ProtectedRefused(t *testing.T) {
	db := setupTestDB(t)
	path := instanceDB(t)
	if err := accessmarker.Write(path, accessmarker.Marker{Access: config.AccessProtected, Instance: gateInstance}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"remind", "list"},
		{"remind", "clear", "obj-1"},
	} {
		_, err := execInstance(db, gateInstance, args...)
		if !errors.Is(err, accessmarker.ErrRefused) {
			t.Fatalf("%v on a protected instance: err = %v, want the access refusal", args, err)
		}
		if got := ExitCodeFor(err); got != output.ExitUnauthorized {
			t.Errorf("%v: exit %d, want %d", args, got, output.ExitUnauthorized)
		}
		// Execute's classification wraps every RunE; the test tree runs
		// without it, so apply it here.
		var env *output.Error
		if !errors.As(classifyReturn(err), &env) || env.SuggestedFix == "" {
			t.Errorf("%v: no suggested fix on the refusal: %v", args, err)
		}
	}
}

// A write against a private instance whose daemon holds the lock fails
// at once with exit 6; it does not wait out the budget meant for other
// commands, and never opens the database beside the daemon.
func TestInstanceGate_DaemonHeldWriteRefusedFast(t *testing.T) {
	db := setupTestDB(t)
	path := instanceDB(t)
	holdAsDaemon(t, path)
	prev := directopen.CLIRetry
	directopen.CLIRetry = 5 * time.Second
	t.Cleanup(func() { directopen.CLIRetry = prev })

	start := time.Now()
	_, err := execInstance(db, gateInstance, "remind", "clear", "obj-1")
	if waited := time.Since(start); waited > 2*time.Second {
		t.Errorf("waited %v on a daemon-held lock; want an immediate refusal", waited)
	}
	if !errors.Is(err, dblock.ErrHeld) {
		t.Fatalf("write beside the daemon: err = %v, want lock contention", err)
	}
	if got := ExitCodeFor(err); got != output.ExitTransient {
		t.Errorf("exit %d, want %d", got, output.ExitTransient)
	}
	// The fix names the command that cannot run beside the daemon and
	// the exact commands to stop the daemon and start it again.
	var env *output.Error
	if !errors.As(classifyReturn(err), &env) {
		t.Fatalf("no envelope: %v", err)
	}
	for _, want := range []string{"ctxt remind clear", "`dpkms shutdown --port 59177`", "`dpkms serve`"} {
		if !strings.Contains(env.SuggestedFix, want) {
			t.Errorf("suggested fix %q does not name %q", env.SuggestedFix, want)
		}
	}
}

// A read command on a private instance opens read-only beside the
// daemon: it succeeds while the daemon holds the lock, and the handle
// it gets cannot write.
func TestInstanceGate_ReadOpensReadOnly(t *testing.T) {
	db := setupTestDB(t)
	path := instanceDB(t)
	holdAsDaemon(t, path)

	if out, err := execInstance(db, gateInstance, "remind", "list"); err != nil {
		t.Fatalf("remind list beside the daemon: %v\n%s", err, out)
	}

	restore := withInstance(t, db, gateInstance)
	defer restore()
	svc, cleanup, err := newService(remindListCmd)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ctx := asOwner(context.Background())
	err = svc.Store(ctx).AuditLog().Append(ctx, &storage.AuditEntry{
		ID: "probe", EventType: "probe", Actor: "test", CreatedAt: time.Now(),
	})
	if err == nil {
		t.Error("a read command's store accepted a write; want a read-only open")
	}
}

// A read command never migrates or creates the database: pointed at an
// instance whose database does not exist, it refuses and leaves no file.
func TestInstanceGate_ReadNeverMigrates(t *testing.T) {
	db := setupTestDB(t)
	path := filepath.Join(t.TempDir(), "absent.db")
	registerInstance(t, path)

	_, err := execInstance(db, gateInstance, "remind", "list")
	if !errors.As(err, new(*directopen.NotReadyError)) {
		t.Fatalf("remind list on a missing database: err = %v, want the not-ready refusal", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("read command created %s (stat: %v)", path, statErr)
	}
}

// A write command holds the database lock for as long as its store is
// open, and releases it on cleanup.
func TestInstanceGate_WriteTakesLock(t *testing.T) {
	db := setupTestDB(t)
	path := instanceDB(t)
	restore := withInstance(t, db, gateInstance)
	defer restore()

	svc, cleanup, err := newService(remindClearCmd)
	if err != nil {
		t.Fatal(err)
	}
	if svc == nil {
		t.Fatal("nil service")
	}
	if l, err := dblock.Acquire(path, dblock.Info{Role: dblock.RoleCLI, Instance: "probe"}); !errors.Is(err, dblock.ErrHeld) {
		if l != nil {
			_ = l.Release()
		}
		t.Fatalf("lock while a write command's store is open: err = %v, want held", err)
	}
	cleanup()
	l, err := dblock.Acquire(path, dblock.Info{Role: dblock.RoleCLI, Instance: "probe"})
	if err != nil {
		t.Fatalf("lock after cleanup: %v", err)
	}
	_ = l.Release()
}

// withInstance points newService at instance name with db's
// configuration loaded, as `--config <db> --instance <name>` would; the
// returned func undoes it.
func withInstance(t *testing.T, db *testDB, name string) func() {
	t.Helper()
	prev := cfg
	loaded, err := config.Load("ctxt", db.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg = loaded
	viper.Set("instance", name)
	return func() {
		viper.Set("instance", "")
		cfg = prev
	}
}

// A --dry-run of an embeddings write takes the read path: beside the
// daemon it is not refused for the lock, and it never creates or
// migrates a database.
func TestInstanceGate_DryRunReads(t *testing.T) {
	db := setupTestDB(t)
	path := instanceDB(t)
	holdAsDaemon(t, path)
	for _, args := range [][]string{
		{"embeddings", "purge", "m@1", "--dry-run"},
		{"embeddings", "set-default", "m@1", "--dry-run"},
		{"embeddings", "deprecate", "m@1", "--dry-run"},
		{"embeddings", "migrate", "--to", "m@1", "--dry-run"},
	} {
		if _, err := execInstance(db, gateInstance, args...); errors.Is(err, dblock.ErrHeld) {
			t.Errorf("%v beside the daemon took the lock: %v", args, err)
		}
	}

	absent := filepath.Join(t.TempDir(), "absent.db")
	registerAt(t, "fresh", absent)
	_, err := execInstance(db, "fresh", "embeddings", "purge", "m@1", "--dry-run")
	if !errors.As(err, new(*directopen.NotReadyError)) {
		t.Errorf("dry run on a missing database: %v, want the not-ready refusal", err)
	}
	if _, statErr := os.Stat(absent); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("dry run created %s", absent)
	}
}

// registerAt writes a pidfile naming a running instance name at path.
func registerAt(t *testing.T, name, path string) {
	t.Helper()
	runDir, err := config.RunDir(testDataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := pidfile.Write(runDir, pidfile.Info{
		PID: os.Getpid(), Name: name, Port: 59178, DBPath: path, StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}
