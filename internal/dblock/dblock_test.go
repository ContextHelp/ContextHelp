package dblock_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dblock"
)

// tempDBPath returns a fake database path inside a per-test temp dir.
func tempDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

func TestPath_AppendsSidecarSuffix(t *testing.T) {
	if got, want := dblock.Path("/x/y/db.sqlite"), "/x/y/db.sqlite.lock"; got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestAcquire_FreeLockSucceeds(t *testing.T) {
	dbPath := tempDBPath(t)

	l, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleCLI})
	if err != nil {
		t.Fatalf("Acquire on free lock: %v", err)
	}
	defer l.Release()

	if _, err := os.Stat(dblock.Path(dbPath)); err != nil {
		t.Fatalf("sidecar lockfile not created: %v", err)
	}
}

func TestAcquire_SecondFdFailsImmediately(t *testing.T) {
	dbPath := tempDBPath(t)

	l, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleDaemon, Instance: "main"})
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer l.Release()

	// flock is per-fd: a second Acquire opens its own fd, so contention is
	// observable within one process.
	start := time.Now()
	_, err = dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleCLI})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("second Acquire succeeded; want contention error")
	}
	if !errors.Is(err, dblock.ErrHeld) {
		t.Fatalf("second Acquire error = %v; want errors.Is(err, ErrHeld)", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("non-blocking Acquire took %v; want immediate failure", elapsed)
	}
}

func TestAcquire_ContenderReadsHolderBody(t *testing.T) {
	dbPath := tempDBPath(t)

	l, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleDaemon, Instance: "main"})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer l.Release()

	// Direct holder read.
	holder, err := dblock.ReadHolder(dbPath)
	if err != nil {
		t.Fatalf("ReadHolder: %v", err)
	}
	if holder.PID != os.Getpid() {
		t.Errorf("holder.PID = %d, want %d", holder.PID, os.Getpid())
	}
	if holder.Role != dblock.RoleDaemon {
		t.Errorf("holder.Role = %q, want %q", holder.Role, dblock.RoleDaemon)
	}
	if holder.Instance != "main" {
		t.Errorf("holder.Instance = %q, want %q", holder.Instance, "main")
	}
	if holder.StartedAt.IsZero() {
		t.Error("holder.StartedAt is zero; want acquire timestamp")
	}

	// The contention error itself carries the holder for error attribution.
	_, err = dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleCLI})
	var held *dblock.HeldError
	if !errors.As(err, &held) {
		t.Fatalf("contention error = %T (%v); want *HeldError", err, err)
	}
	if held.Holder == nil {
		t.Fatal("HeldError.Holder is nil; want holder identity")
	}
	if held.Holder.Role != dblock.RoleDaemon || held.Holder.Instance != "main" {
		t.Errorf("HeldError.Holder = %+v; want daemon/main", held.Holder)
	}
}

func TestRelease_MakesLockReacquirable(t *testing.T) {
	dbPath := tempDBPath(t)

	l, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleCLI})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	l2, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleCLI})
	if err != nil {
		t.Fatalf("re-Acquire after Release: %v", err)
	}
	defer l2.Release()
}

func TestRelease_NeverUnlinksLockfile(t *testing.T) {
	dbPath := tempDBPath(t)

	l, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleCLI})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if _, err := os.Stat(dblock.Path(dbPath)); err != nil {
		t.Fatalf("lockfile missing after Release: %v — release must not unlink", err)
	}
}

func TestHolderProcessExit_ReleasesLock(t *testing.T) {
	dbPath := tempDBPath(t)

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperHoldLock$", "-test.v") // #nosec G204
	cmd.Env = append(os.Environ(),
		"DBLOCK_TEST_HELPER=1",
		"DBLOCK_TEST_DBPATH="+dbPath,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()

	// Wait until the helper reports it holds the lock.
	scanner := bufio.NewScanner(stdout)
	locked := false
	for scanner.Scan() {
		if scanner.Text() == "LOCKED" {
			locked = true
			break
		}
	}
	if !locked {
		t.Fatal("helper never reported LOCKED")
	}

	// While the helper lives, the lock is held.
	if _, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleCLI}); !errors.Is(err, dblock.ErrHeld) {
		t.Fatalf("Acquire while helper holds lock: err = %v; want ErrHeld", err)
	}

	// Helper exits without calling Release: the kernel must release the flock.
	if err := stdin.Close(); err != nil {
		t.Fatalf("close helper stdin: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper exit: %v", err)
	}

	l, err := dblock.AcquireRetry(context.Background(), dbPath,
		dblock.Info{Role: dblock.RoleCLI}, 2*time.Second)
	if err != nil {
		t.Fatalf("Acquire after holder exit: %v — kernel should have released the flock", err)
	}
	defer l.Release()
}

// TestHelperHoldLock is not a test: it is the subprocess body for
// TestHolderProcessExit_ReleasesLock. It acquires the lock, reports LOCKED,
// then exits WITHOUT releasing when stdin closes.
func TestHelperHoldLock(t *testing.T) {
	if os.Getenv("DBLOCK_TEST_HELPER") != "1" {
		t.Skip("helper process body; skipped in normal runs")
	}
	dbPath := os.Getenv("DBLOCK_TEST_DBPATH")
	_, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleDaemon, Instance: "helper"})
	if err != nil {
		fmt.Println("ERR:", err)
		os.Exit(1)
	}
	fmt.Println("LOCKED")
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0) // deliberately no Release: exit must free the kernel flock
}

func TestAcquireRetry_SucceedsWhenHolderReleasesWithinBudget(t *testing.T) {
	dbPath := tempDBPath(t)

	l, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleCLI})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = l.Release()
	}()

	l2, err := dblock.AcquireRetry(context.Background(), dbPath,
		dblock.Info{Role: dblock.RoleCLI}, 2*time.Second)
	if err != nil {
		t.Fatalf("AcquireRetry within budget: %v", err)
	}
	defer l2.Release()
}

func TestAcquireRetry_FailsWithHolderIdentityAfterBudget(t *testing.T) {
	dbPath := tempDBPath(t)

	l, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleDaemon, Instance: "main"})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer l.Release()

	start := time.Now()
	_, err = dblock.AcquireRetry(context.Background(), dbPath,
		dblock.Info{Role: dblock.RoleCLI}, 300*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("AcquireRetry succeeded against a held lock; want error")
	}
	if !errors.Is(err, dblock.ErrHeld) {
		t.Fatalf("AcquireRetry error = %v; want errors.Is(err, ErrHeld)", err)
	}
	var held *dblock.HeldError
	if !errors.As(err, &held) {
		t.Fatalf("AcquireRetry error = %T; want *HeldError", err)
	}
	if held.Holder == nil || held.Holder.Role != dblock.RoleDaemon || held.Holder.Instance != "main" {
		t.Errorf("HeldError.Holder = %+v; want daemon/main identity", held.Holder)
	}
	if elapsed < 300*time.Millisecond {
		t.Errorf("AcquireRetry returned after %v; want it to spend the full budget", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("AcquireRetry took %v; want prompt failure after budget", elapsed)
	}
}

func TestAcquireRetry_ContextCancelStopsEarly(t *testing.T) {
	dbPath := tempDBPath(t)

	l, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleCLI})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer l.Release()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err = dblock.AcquireRetry(ctx, dbPath, dblock.Info{Role: dblock.RoleCLI}, 10*time.Second)
	if err == nil {
		t.Fatal("AcquireRetry succeeded; want context cancellation error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("AcquireRetry ran %v after cancel; want early return", elapsed)
	}
}

func TestReadHolder_NoLockfile(t *testing.T) {
	dbPath := tempDBPath(t)
	if _, err := dblock.ReadHolder(dbPath); err == nil {
		t.Fatal("ReadHolder with no lockfile: want error")
	}
}
