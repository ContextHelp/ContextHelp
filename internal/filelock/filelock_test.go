package filelock_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/filelock"
)

// open returns a fresh open file on path; each call is a separate lock owner.
func open(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- t.TempDir path
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func lockPath(t *testing.T) string { return filepath.Join(t.TempDir(), "x.lock") }

func TestTryLockExcludesOtherOpenFiles(t *testing.T) {
	path := lockPath(t)
	a, b := open(t, path), open(t, path)

	if err := filelock.TryLock(a); err != nil {
		t.Fatalf("first TryLock: %v", err)
	}
	if err := filelock.TryLock(b); !errors.Is(err, filelock.ErrLocked) {
		t.Fatalf("TryLock while held exclusively: got %v, want ErrLocked", err)
	}
	if err := filelock.TryRLock(b); !errors.Is(err, filelock.ErrLocked) {
		t.Fatalf("TryRLock while held exclusively: got %v, want ErrLocked", err)
	}
	if err := filelock.Unlock(a); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if err := filelock.TryLock(b); err != nil {
		t.Fatalf("TryLock after release: %v", err)
	}
}

func TestSharedLocksCoexistAndExcludeExclusive(t *testing.T) {
	path := lockPath(t)
	a, b, c := open(t, path), open(t, path), open(t, path)

	if err := filelock.TryRLock(a); err != nil {
		t.Fatalf("TryRLock a: %v", err)
	}
	if err := filelock.TryRLock(b); err != nil {
		t.Fatalf("TryRLock b beside a shared holder: %v", err)
	}
	if err := filelock.TryLock(c); !errors.Is(err, filelock.ErrLocked) {
		t.Fatalf("TryLock beside shared holders: got %v, want ErrLocked", err)
	}
}

func TestCloseReleasesLock(t *testing.T) {
	path := lockPath(t)
	a, b := open(t, path), open(t, path)

	if err := filelock.TryLock(a); err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	_ = a.Close()
	if err := filelock.TryLock(b); err != nil {
		t.Fatalf("TryLock after holder closed: %v", err)
	}
}

func TestLockBlocksUntilRelease(t *testing.T) {
	path := lockPath(t)
	a, b := open(t, path), open(t, path)

	if err := filelock.Lock(a); err != nil {
		t.Fatalf("Lock a: %v", err)
	}
	got := make(chan error, 1)
	go func() { got <- filelock.Lock(b) }()

	select {
	case err := <-got:
		t.Fatalf("Lock b returned while a held the lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := filelock.Unlock(a); err != nil {
		t.Fatalf("Unlock a: %v", err)
	}
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("Lock b after release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Lock b still blocked 10s after release")
	}
}

// Holders write a body into the locked file that contenders read through
// their own handle (dblock, watchstate). The lock must not hide it.
func TestHeldFileContentStaysReadable(t *testing.T) {
	path := lockPath(t)
	a := open(t, path)
	if err := filelock.TryLock(a); err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	if _, err := a.WriteAt([]byte(`{"pid":1}`), 0); err != nil {
		t.Fatalf("holder write: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- t.TempDir path
	if err != nil {
		t.Fatalf("contender read while held: %v", err)
	}
	if string(data) != `{"pid":1}` {
		t.Fatalf("contender read %q", data)
	}
}
