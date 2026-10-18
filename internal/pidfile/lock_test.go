package pidfile

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/filelock"
)

// holdLock takes dir's lock as another holder would (a stopped process)
// until the test ends or release is called.
func holdLock(t *testing.T, dir string) (release func()) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := filelock.TryLock(f); err != nil {
		t.Fatal(err)
	}
	done := false
	release = func() {
		if !done {
			done = true
			_ = filelock.Unlock(f)
			_ = f.Close()
		}
	}
	t.Cleanup(release)
	return release
}

// A scan (every ctxt command runs one) does not stall behind a stuck
// holder: it gives up on the lock quickly, lists what it reads and
// prunes nothing.
func TestScanDoesNotWaitLongForAStuckHolder(t *testing.T) {
	dir := t.TempDir()
	child := exec.Command("true")
	if err := child.Run(); err != nil {
		t.Fatalf("run child: %v", err)
	}
	if err := Write(dir, Info{PID: child.Process.Pid, Name: "gone", Port: testPort}); err != nil {
		t.Fatal(err)
	}
	holdLock(t, dir)
	start := time.Now()
	if _, err := Scan(dir); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Errorf("Scan waited %v behind a stuck holder", took)
	}
	if _, err := os.Stat(pidPath(dir, testPort)); err != nil {
		t.Errorf("Scan pruned without the lock: %v", err)
	}
}

// Remove, serve's own deregistration, takes the lock like Write: it
// waits for a holder that lets go.
func TestRemoveTakesTheLock(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, Info{PID: os.Getpid(), Name: "home", Port: testPort}); err != nil {
		t.Fatal(err)
	}
	release := holdLock(t, dir)
	removed := make(chan error, 1)
	go func() { removed <- Remove(dir, testPort) }()
	select {
	case err := <-removed:
		t.Fatalf("Remove ran while the lock was held (err %v)", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case err := <-removed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Remove did not finish once the lock was free")
	}
	if _, err := os.Stat(pidPath(dir, testPort)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("pidfile left: %v", err)
	}
}
