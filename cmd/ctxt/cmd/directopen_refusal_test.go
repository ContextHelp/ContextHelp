package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/directopen"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
)

// heldRefusal returns the daemon-held refusal fix for a write on path.
func heldRefusal(t *testing.T, path string) string {
	t.Helper()
	_, err := directopen.Write(context.Background(), directopen.Target{
		Path: path, Blob: config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{Path: t.TempDir()}},
	})
	if err == nil {
		t.Fatal("write beside the daemon succeeded")
	}
	fixer, ok := explainDaemonHeld(err, remindClearCmd, path).(interface{ Fix() string })
	if !ok {
		t.Fatalf("no daemon-held refusal: %v", err)
	}
	return fixer.Fix()
}

// With no pidfile naming the database, the port comes from the pidfile
// of the process holding the lock.
func TestDaemonHeldFix_PortFromHolderPID(t *testing.T) {
	db := setupTestDB(t)
	path := instanceDB(t)
	defer withInstance(t, db, gateInstance)() // registers port 59177 for path
	runDir, err := config.RunDir(testDataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	// Re-register the holder (this process) under another database path.
	if err := os.RemoveAll(runDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := pidfile.Write(runDir, pidfile.Info{
		PID: os.Getpid(), Name: gateInstance, Port: 59199, DBPath: "/elsewhere.db", StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	holdAsDaemon(t, path)
	if fix := heldRefusal(t, path); !strings.Contains(fix, "`dpkms shutdown --port 59199`") {
		t.Errorf("fix %q does not name the holder's port", fix)
	}
}

// With no pidfile at all, the fix names the holder's pid.
func TestDaemonHeldFix_NamesPIDWhenPortUnknown(t *testing.T) {
	db := setupTestDB(t)
	path := instanceDB(t)
	defer withInstance(t, db, gateInstance)()
	runDir, err := config.RunDir(testDataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(runDir); err != nil {
		t.Fatal(err)
	}
	holdAsDaemon(t, path)
	fix := heldRefusal(t, path)
	if !strings.Contains(fix, fmt.Sprintf("pid %d", os.Getpid())) || strings.Contains(fix, "`dpkms shutdown`") {
		t.Errorf("fix %q does not name the holder's pid", fix)
	}
}
