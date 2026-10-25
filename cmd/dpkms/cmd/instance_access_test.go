package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dblock"
)

// A protected or public serve records its class, and later its
// address; a private serve writes nothing and never lowers a recorded
// class.
func TestRecordServeAccess(t *testing.T) {
	db := filepath.Join(t.TempDir(), "db.sqlite")
	c := sqliteServeConfig(db)
	var warn bytes.Buffer

	if err := recordServeAccess(&warn, c, "home", config.AccessPrivate, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(accessmarker.Path(db)); !os.IsNotExist(err) {
		t.Fatalf("private serve wrote a marker: %v", err)
	}

	if err := recordServeAccess(&warn, c, "home", config.AccessProtected, ""); err != nil {
		t.Fatal(err)
	}
	if err := recordServeAccess(&warn, c, "home", config.AccessProtected, "0.0.0.0:8420"); err != nil {
		t.Fatal(err)
	}
	m, found, err := accessmarker.Read(db)
	if err != nil || !found || m.Access != config.AccessProtected || m.Instance != "home" || m.DaemonAddr != "0.0.0.0:8420" {
		t.Fatalf("marker after protected serve = %+v, %v, %v", m, found, err)
	}

	if err := recordServeAccess(&warn, c, "home", config.AccessPrivate, ""); err != nil {
		t.Fatal(err)
	}
	if m, _, _ := accessmarker.Read(db); m.Access != config.AccessProtected {
		t.Errorf("private serve lowered the marker to %q", m.Access)
	}
	if !strings.Contains(warn.String(), "set-access private") {
		t.Errorf("private serve over a protected marker did not say how to lower it: %q", warn.String())
	}
}

// serve writes the marker before it opens the database, and the
// file-mode preflight under --strict-permissions refuses an instance
// other users can read, before storage is touched.
func TestRunServe_MarkerBeforeStorageAndStrictPermissions(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("file modes do not express access on Windows")
	}
	shortServeLockBudget(t, 100*time.Millisecond)
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	if err := os.WriteFile(dbPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dbPath, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BUS_TOKEN", "")
	t.Setenv("DPKMS_BUS_TOKEN", "")
	prev := cfg
	t.Cleanup(func() { cfg = prev; resetAllFlags(serveCmd) })
	cfg = sqliteServeConfig(dbPath)
	cfg.Server.Access = config.AccessProtected
	cfg.Server.PublicURL = "https://dpkms.example.com"
	for f, v := range map[string]string{"port": "0", "grpc-port": "0", "cookie-bridge-port": "0", "strict-permissions": "true", "name": "home"} {
		if err := serveCmd.Flags().Set(f, v); err != nil {
			t.Fatal(err)
		}
	}

	err := runServe(serveCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "readable by other users") {
		t.Fatalf("serve over a world-readable database with --strict-permissions = %v", err)
	}
	m, found, rerr := accessmarker.Read(dbPath)
	if rerr != nil || !found || m.Access != config.AccessProtected {
		t.Errorf("marker after refused serve = %+v, %v, %v; want protected recorded", m, found, rerr)
	}
	if fi, _ := os.Stat(dbPath); fi.Size() != 0 {
		t.Errorf("refused serve opened and migrated the database (size %d)", fi.Size())
	}
}

// set-access lowers a recorded class, with the daemon stopped only.
func TestInstanceSetAccess(t *testing.T) {
	db := setupTestDB(t)
	path := dbPathOf(db)
	markAccess(t, path, config.AccessPublic)

	out, err := db.run("instance", "set-access", "private")
	if err != nil {
		t.Fatalf("set-access private: %v", err)
	}
	if !strings.Contains(out, "public") {
		t.Errorf("output %q does not name the previous class", out)
	}
	m, _, err := accessmarker.Read(path)
	if err != nil || m.Access != config.AccessPrivate || m.Instance != "home" {
		t.Fatalf("marker after set-access = %+v, %v", m, err)
	}
	if _, err := db.run("job", "list"); err != nil {
		t.Errorf("direct open after set-access private: %v", err)
	}

	holdDaemonLock(t, path)
	_, err = db.run("instance", "set-access", "protected")
	if !errors.Is(err, dblock.ErrHeld) {
		t.Fatalf("set-access with the daemon up = %v; want lock contention", err)
	}
	if m, _, _ := accessmarker.Read(path); m.Access != config.AccessPrivate {
		t.Errorf("refused set-access changed the marker to %q", m.Access)
	}
	if _, err := db.run("instance", "set-access", "open"); err == nil {
		t.Error("set-access accepted an unknown class")
	}
}

// set-access repairs an unreadable marker, which otherwise refuses
// every direct open.
func TestInstanceSetAccess_RepairsUnreadableMarker(t *testing.T) {
	db := setupTestDB(t)
	path := dbPathOf(db)
	if err := os.WriteFile(accessmarker.Path(path), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.run("job", "list"); !errors.Is(err, accessmarker.ErrRefused) {
		t.Fatalf("job list with a torn marker = %v; want refused", err)
	}
	if _, err := db.run("instance", "set-access", "private"); err != nil {
		t.Fatalf("set-access over a torn marker: %v", err)
	}
	if _, err := db.run("job", "list"); err != nil {
		t.Errorf("job list after repair: %v", err)
	}
}

// doctor flags a protected instance other users can read; --strict
// fails on it.
func TestDoctor_Permissions(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("file modes do not express access on Windows")
	}
	db := setupTestDB(t)
	path := dbPathOf(db)
	markAccess(t, path, config.AccessProtected)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path + "-wal", path + "-shm", dblock.Path(path)} {
		_ = os.Chmod(p, 0o600)
	}
	// The blob directory is left as the blob store created it: closed.
	out, err := db.run("doctor")
	if err != nil {
		t.Fatalf("doctor on a closed instance: %v\n%s", err, out)
	}
	if !strings.Contains(out, "files closed to other users") {
		t.Errorf("doctor output %q", out)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = db.run("doctor")
	if err != nil {
		t.Fatalf("doctor without --strict failed on a warning: %v", err)
	}
	if !strings.Contains(out, "warn") || !strings.Contains(out, path) {
		t.Errorf("doctor did not flag %s: %q", path, out)
	}
	if _, err := db.run("doctor", "--strict"); err == nil {
		t.Error("doctor --strict passed a world-readable protected database")
	}
}

// config validate reports a path federation entry whose target is
// protected.
func TestConfigValidate_NonPrivateLocalFederationTarget(t *testing.T) {
	db := setupTestDB(t)
	target := filepath.Join(t.TempDir(), "peer.sqlite")
	markAccess(t, target, config.AccessProtected)
	f, err := os.OpenFile(db.ConfigPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("federations:\n  - name: vault\n    url: " + target + "\n    sync_mode: async\n    interval: 1m\n")
	_ = f.Close()

	out, err := db.run("config", "validate")
	if err == nil {
		t.Fatalf("config validate passed a protected path target:\n%s", out)
	}
	if !strings.Contains(out, "federations") || !strings.Contains(out, `"vault"`) {
		t.Errorf("validate output does not name the entry: %q", out)
	}
}

// Blobs left open by an older store are reported by doctor with the
// exact commands that close them.
func TestDoctor_LooseBlobs(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("file modes do not express access on Windows")
	}
	db := setupTestDB(t)
	path := dbPathOf(db)
	markAccess(t, path, config.AccessProtected)
	for _, p := range []string{path, path + "-wal", path + "-shm", dblock.Path(path)} {
		_ = os.Chmod(p, 0o600)
	}
	blobs := filepath.Join(t.TempDir(), "blobs")
	if err := os.MkdirAll(filepath.Join(blobs, "ab"), 0o700); err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(blobs, "ab", "abcdef")
	if err := os.WriteFile(loose, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(db.ConfigPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("  blob:\n    backend: local\n    local:\n      path: " + blobs + "\n")
	_ = f.Close()

	out, err := db.run("doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	for _, want := range []string{loose, "chmod 0600 '" + loose + "'", "find " + blobs} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output does not contain %q:\n%s", want, out)
		}
	}
}
