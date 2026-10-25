package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/directopen"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// dbPathOf is the database the test config points at.
func dbPathOf(db *testDB) string { return filepath.Join(filepath.Dir(db.ConfigPath), "test.db") }

// holdDaemonLock takes the database lock as a daemon would.
func holdDaemonLock(t *testing.T, dbPath string) {
	t.Helper()
	l, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleDaemon, Instance: "home"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Release() })
}

func markAccess(t *testing.T, dbPath, access string) {
	t.Helper()
	if err := accessmarker.Write(dbPath, accessmarker.Marker{Access: access, Instance: "home", DaemonAddr: "0.0.0.0:8420"}); err != nil {
		t.Fatal(err)
	}
}

// wantExit checks err's exit code and that its suggested fix names each
// of fixes.
func wantExit(t *testing.T, err error, code int, fixes ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("command succeeded; want exit %d", code)
	}
	if got := ExitCodeFor(err); got != code {
		t.Fatalf("exit %d for %v; want %d", got, err, code)
	}
	// The binary's RunE wrapper (installErrorClassification) promotes
	// the bare error; executeCommand runs the tree without it.
	var env *output.Error
	if !errors.As(classifyReturn(rootCmd, nil, err), &env) {
		t.Fatalf("error %v classifies to no envelope", err)
	}
	for _, f := range fixes {
		if !strings.Contains(env.SuggestedFix, f) {
			t.Errorf("suggested fix %q does not mention %q", env.SuggestedFix, f)
		}
	}
}

// A read command runs beside the daemon: it does not take the lock.
func TestDirectOpen_ReadBesideDaemon(t *testing.T) {
	db := setupTestDB(t)
	holdDaemonLock(t, dbPathOf(db))
	if _, err := db.run("job", "list"); err != nil {
		t.Fatalf("job list beside the daemon: %v", err)
	}
}

// A write command against a database the daemon holds fails with the
// contention class (exit 6, busy: retry), naming how to get the lock.
func TestDirectOpen_WriteRefusedWhileDaemonHolds(t *testing.T) {
	db := setupTestDB(t)
	holdDaemonLock(t, dbPathOf(db))
	_, err := db.run("job", "retry", "no-such-job")
	if !errors.Is(err, dblock.ErrHeld) {
		t.Fatalf("job retry beside the daemon = %v; want lock contention", err)
	}
	wantExit(t, err, output.ExitTransient, "dpkms shutdown")
	if !strings.Contains(err.Error(), "daemon") || !strings.Contains(err.Error(), `instance "home"`) {
		t.Errorf("error %q does not name the holder", err)
	}
}

// Housekeeping, whose VACUUM needs the database to itself, refuses
// while the daemon holds it.
func TestDirectOpen_HousekeepingRefusedWhileDaemonHolds(t *testing.T) {
	db := setupTestDB(t)
	holdDaemonLock(t, dbPathOf(db))
	_, err := db.run("housekeeping", "vacuum")
	wantExit(t, err, output.ExitTransient, "dpkms shutdown")
}

// A protected or public instance refuses every direct open, reads
// included, with the posture class (exit 5: do not retry), distinct
// from contention.
func TestDirectOpen_AccessGate(t *testing.T) {
	for _, access := range []string{config.AccessProtected, config.AccessPublic} {
		for name, args := range map[string][]string{
			"read":  {"job", "list"},
			"write": {"job", "retry", "no-such-job"},
		} {
			t.Run(access+"/"+name, func(t *testing.T) {
				db := setupTestDB(t)
				markAccess(t, dbPathOf(db), access)
				_, err := db.run(args...)
				wantExit(t, err, output.ExitUnauthorized, "daemon API", "0.0.0.0:8420", "dpkms serve", "set-access private")
				if !strings.Contains(err.Error(), access) {
					t.Errorf("error %q does not name the class", err)
				}
			})
		}
	}
}

// Housekeeping and backup on a protected instance are refused, and the
// refusal names the maintenance override.
func TestDirectOpen_MaintenanceRefused(t *testing.T) {
	for name, args := range map[string][]string{
		"housekeeping": {"housekeeping", "vacuum"},
		"backup":       {"backup", "--output-dir", "OUT"},
	} {
		t.Run(name, func(t *testing.T) {
			db := setupTestDB(t)
			markAccess(t, dbPathOf(db), config.AccessProtected)
			for i, a := range args {
				if a == "OUT" {
					args[i] = t.TempDir()
				}
			}
			_, err := db.run(args...)
			wantExit(t, err, output.ExitUnauthorized, "--maintenance-override", "set-access private")
		})
	}
}

// The override runs housekeeping on a protected instance with the
// daemon stopped, leaves its class alone and records an audit entry.
func TestDirectOpen_HousekeepingOverrideAudited(t *testing.T) {
	db := setupTestDB(t)
	path := dbPathOf(db)
	markAccess(t, path, config.AccessProtected)
	if _, err := db.run("housekeeping", "vacuum", "--maintenance-override"); err != nil {
		t.Fatalf("housekeeping vacuum --maintenance-override: %v", err)
	}
	entries, _, err := db.Driver.AuditLog().List(context.Background(),
		storage.AuditFilter{EventType: directopen.EventMaintenanceOverride})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Payload["action"].(string), "housekeeping vacuum") {
		t.Fatalf("override audit entries = %+v; want one for housekeeping vacuum", entries)
	}
	if m, _, _ := accessmarker.Read(path); m.Access != config.AccessProtected {
		t.Errorf("override changed the class to %q", m.Access)
	}
}

// The backup override snapshots a protected instance and audits it; it
// still refuses while the daemon runs.
func TestDirectOpen_BackupOverride(t *testing.T) {
	db := setupTestDB(t)
	path := dbPathOf(db)
	markAccess(t, path, config.AccessProtected)
	out := t.TempDir()
	if _, err := db.run("backup", "--output-dir", out, "--maintenance-override"); err != nil {
		t.Fatalf("backup --maintenance-override: %v", err)
	}
	archives, _ := filepath.Glob(filepath.Join(out, "*.tar.gz"))
	if len(archives) != 1 {
		t.Errorf("archives = %v; want one", archives)
	}
	entries, _, err := db.Driver.AuditLog().List(context.Background(),
		storage.AuditFilter{EventType: directopen.EventMaintenanceOverride})
	if err != nil || len(entries) != 1 {
		t.Fatalf("backup override audit entries = %d, %v; want 1", len(entries), err)
	}

	holdDaemonLock(t, path)
	_, err = db.run("backup", "--output-dir", t.TempDir(), "--maintenance-override")
	wantExit(t, err, output.ExitTransient, "dpkms shutdown")
}

// server.access protected refuses direct opens before serve has ever
// written the marker.
func TestDirectOpen_ConfiguredProtectedWithoutMarker(t *testing.T) {
	db := setupTestDB(t)
	f, err := os.OpenFile(db.ConfigPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("server:\n  access: protected\n")
	_ = f.Close()
	_, err = db.run("job", "list")
	wantExit(t, err, output.ExitUnauthorized, "server.access")
}

// The host-side bootstrap and recovery commands stay reachable on a
// protected instance through the audited override: refused without it
// (the fix names it), done with it, with an audit entry each.
func TestDirectOpen_BootstrapCommandsTakeOverride(t *testing.T) {
	db := setupTestDB(t)
	path := dbPathOf(db)
	markAccess(t, path, config.AccessProtected)

	for _, args := range [][]string{
		{"auth", "key", "create", "--principal", "owner"},
		{"session", "revoke", "--principal", "owner"},
	} {
		_, err := db.run(args...)
		wantExit(t, err, output.ExitUnauthorized, "--maintenance-override")
		if _, err := db.run(append(args, "--maintenance-override")...); err != nil {
			t.Fatalf("%v --maintenance-override: %v", args, err)
		}
	}
	entries, _, err := db.Driver.AuditLog().List(context.Background(),
		storage.AuditFilter{EventType: directopen.EventMaintenanceOverride})
	if err != nil || len(entries) != 2 {
		t.Fatalf("override audit entries = %d, %v; want 2", len(entries), err)
	}
}

// freshConfig writes a config whose database does not exist yet.
func freshConfig(t *testing.T) *testDB {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("storage:\n  type: sqlite\n  path: "+filepath.Join(dir, "test.db")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &testDB{ConfigPath: cfgPath}
}

// config validate never needs an openable store: on a fresh install, a
// protected instance or another schema version it checks overrides
// without the installed steps and says so, instead of failing.
func TestConfigValidate_StoreUnavailable(t *testing.T) {
	cases := map[string]func(t *testing.T) *testDB{
		"fresh": freshConfig,
		"protected": func(t *testing.T) *testDB {
			db := setupTestDB(t)
			markAccess(t, dbPathOf(db), config.AccessProtected)
			return db
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			db := mk(t)
			out, err := db.run("config", "validate")
			if err != nil {
				t.Fatalf("config validate: %v\n%s", err, out)
			}
			if !strings.Contains(out, "installed steps not checked") {
				t.Errorf("validate did not warn that installed steps were skipped: %q", out)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(db.ConfigPath), "test.db")); name == "fresh" && !os.IsNotExist(err) {
				t.Errorf("validate created the database: %v", err)
			}
		})
	}
}

// A read command on a database that does not exist yet fails, and says
// what to do.
func TestDirectOpen_ReadOnFreshInstallSaysWhatToDo(t *testing.T) {
	_, err := freshConfig(t).run("job", "list")
	if err == nil {
		t.Fatal("job list on a missing database succeeded")
	}
	var env *output.Error
	if !errors.As(classifyReturn(rootCmd, nil, err), &env) || !strings.Contains(env.SuggestedFix, "dpkms serve") {
		t.Errorf("error %v carries no fix naming dpkms serve", err)
	}
}

// A torn marker under --maintenance-override says how to repair the
// marker, not to rerun with the flag already given.
func TestDirectOpen_OverrideWithTornMarker(t *testing.T) {
	db := setupTestDB(t)
	if err := os.WriteFile(accessmarker.Path(dbPathOf(db)), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := db.run("housekeeping", "vacuum", "--maintenance-override")
	wantExit(t, err, output.ExitUnauthorized, "unreadable", "dpkms instance set-access")
	var env *output.Error
	if errors.As(classifyReturn(rootCmd, nil, err), &env) && strings.Contains(env.SuggestedFix, "--maintenance-override") {
		t.Errorf("fix %q tells the operator to pass the flag they passed", env.SuggestedFix)
	}
}

// An unreachable Postgres store is an unreadable store to validation:
// warn and check against built-in steps, never fail on it. Port 1 on
// loopback refuses at once; nothing listens there.
func TestConfigValidate_PostgresUnreachable(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("storage:\n  type: postgres\n  path: postgres://127.0.0.1:1/none?sslmode=disable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := (&testDB{ConfigPath: cfgPath}).run("config", "validate")
	if err != nil {
		t.Fatalf("config validate with Postgres down: %v\n%s", err, out)
	}
	if !strings.Contains(out, "installed steps not checked") {
		t.Errorf("no warning: %q", out)
	}
}
