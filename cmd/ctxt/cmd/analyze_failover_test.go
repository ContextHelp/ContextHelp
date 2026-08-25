package cmd

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// deadServerURL points at a port nothing listens on.
const deadServerURL = "http://127.0.0.1:19999"

var jobIDRe = regexp.MustCompile(`Job ID: (\S+)`)

// tempDB returns a temp DB file path; pass storageOverride(dbPath) to
// executeCommand so the command under test uses it.
func tempDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "db.sqlite")
}

// storageOverride renders the -c key=value override routing storage to dbPath.
func storageOverride(dbPath string) []string {
	return []string{"-c", "storage.path=" + dbPath}
}

// TestAnalyzeDaemonDownFallsBackToLocalEnqueue: with no daemon listening the
// write must not fail — it enqueues directly into local storage, gated by the
// advisory database lock, and the job is present locally afterwards.
func TestAnalyzeDaemonDownFallsBackToLocalEnqueue(t *testing.T) {
	dbPath := tempDB(t)

	out, err := executeCommand(append([]string{"analyze", "offline insight", "--server", deadServerURL}, storageOverride(dbPath)...)...)
	if err != nil {
		t.Fatalf("analyze with daemon down should fall back locally: %v", err)
	}
	m := jobIDRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("output lacks Job ID line: %q", out)
	}
	jobID := m[1]

	// The job row must exist in the local queue.
	driver, err := storageutil.NewDriver("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open local storage: %v", err)
	}
	ctx := context.Background()
	if err := driver.Init(ctx); err != nil {
		t.Fatalf("init local storage: %v", err)
	}
	defer driver.Close(ctx)

	job, err := driver.Jobs().Get(ctx, jobID)
	if err != nil {
		t.Fatalf("job %s not found in local queue: %v", jobID, err)
	}
	if job.Payload != "offline insight" {
		t.Errorf("job payload = %q, want the analyzed content", job.Payload)
	}

	// The write path must have taken (and released) the advisory lock:
	// the sidecar exists and records this process as last CLI holder.
	holder, err := dblock.ReadHolder(dbPath)
	if err != nil {
		t.Fatalf("lock sidecar unreadable after local enqueue: %v", err)
	}
	if holder.Role != dblock.RoleCLI {
		t.Errorf("lock holder role = %q, want %q", holder.Role, dblock.RoleCLI)
	}
}

// TestAnalyzeDaemonDownLocalWaitDoesNotHang: --wait against the local queue
// has no worker to wait on; the CLI must say so and exit promptly.
func TestAnalyzeDaemonDownLocalWaitDoesNotHang(t *testing.T) {
	dbPath := tempDB(t)

	start := time.Now()
	out, err := executeCommand(append([]string{"analyze", "queued content", "--wait", "--server", deadServerURL}, storageOverride(dbPath)...)...)
	if err != nil {
		t.Fatalf("analyze --wait with daemon down should enqueue locally: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("analyze --wait took %v; must not poll a dead daemon", elapsed)
	}
	if !strings.Contains(out, "Job ID:") {
		t.Errorf("output lacks Job ID: %q", out)
	}
	if !strings.Contains(out, "dpkms serve") {
		t.Errorf("output should tell the operator the job runs when the daemon starts: %q", out)
	}
}

// TestAnalyzeDaemonHoldsLockButNotHealth: the false-negative window — a
// daemon that holds the DB lock (e.g. still inside storage init) while not
// answering /health. The CLI must fail fast with an instructive, attributed
// error: no hang, no second writer, no local enqueue.
func TestAnalyzeDaemonHoldsLockButNotHealth(t *testing.T) {
	dbPath := tempDB(t)

	l, lockErr := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleDaemon, Instance: "main"})
	if lockErr != nil {
		t.Fatalf("daemon-side lock: %v", lockErr)
	}
	defer l.Release()

	start := time.Now()
	out, err := executeCommand(append([]string{"analyze", "contended content", "--server", deadServerURL}, storageOverride(dbPath)...)...)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("analyze must fail while a daemon holds the lock and health is down; got %q", out)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("contention error took %v; must fail fast, not hang", elapsed)
	}
	msg := err.Error()
	if !strings.Contains(msg, "dpkms") {
		t.Errorf("error must attribute the holder as the daemon: %v", err)
	}
	if !strings.Contains(msg, "pid") {
		t.Errorf("error must name the holder pid: %v", err)
	}
	if strings.Contains(out, "Job ID:") {
		t.Errorf("no Job ID may be printed when the write was refused: %q", out)
	}
	// Storage must never have been opened: the gate fires before the DB.
	if _, statErr := os.Stat(dbPath); statErr == nil {
		t.Error("database file created despite held lock; direct write happened")
	}
}

// TestAnalyzeAnotherCLIReleasesWithinBudget: contention against another CLI
// is transient; the bounded retry must win once the peer releases.
func TestAnalyzeAnotherCLIReleasesWithinBudget(t *testing.T) {
	dbPath := tempDB(t)

	l, lockErr := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleCLI})
	if lockErr != nil {
		t.Fatalf("peer-CLI lock: %v", lockErr)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = l.Release()
	}()

	out, err := executeCommand(append([]string{"analyze", "patient content", "--server", deadServerURL}, storageOverride(dbPath)...)...)
	if err != nil {
		t.Fatalf("analyze should win the lock once the peer CLI releases: %v", err)
	}
	if !strings.Contains(out, "Job ID:") {
		t.Errorf("output lacks Job ID: %q", out)
	}
}

// TestAnalyzeAnotherCLIHoldsPastBudget: a peer CLI that never releases gets
// the instructive contention error after the bounded retry.
func TestAnalyzeAnotherCLIHoldsPastBudget(t *testing.T) {
	dbPath := tempDB(t)

	l, lockErr := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleCLI})
	if lockErr != nil {
		t.Fatalf("peer-CLI lock: %v", lockErr)
	}
	defer l.Release()

	out, err := executeCommand(append([]string{"analyze", "blocked content", "--server", deadServerURL}, storageOverride(dbPath)...)...)
	if err == nil {
		t.Fatalf("analyze must fail when the peer CLI never releases; got %q", out)
	}
	if !strings.Contains(err.Error(), "ctxt") {
		t.Errorf("error should attribute the holder as another ctxt command: %v", err)
	}
}

// TestAnalyzeLiveDaemonLeavesLocalStorageUntouched: routed via the daemon API,
// the CLI must not open (or create) the local database at all.
func TestAnalyzeLiveDaemonLeavesLocalStorageUntouched(t *testing.T) {
	dbPath := tempDB(t)

	srv := startMockDPKMS(t)
	defer srv.Close()

	out, err := executeCommand(append([]string{"analyze", "routed content", "--server", srv.URL}, storageOverride(dbPath)...)...)
	if err != nil {
		t.Fatalf("analyze via live daemon: %v", err)
	}
	if !strings.Contains(out, "Job ID: job_12345678") {
		t.Errorf("output should carry the daemon's job ID: %q", out)
	}
	if _, statErr := os.Stat(dbPath); statErr == nil {
		t.Error("local database opened despite live daemon; write must route via API")
	}
	if _, statErr := os.Stat(dblock.Path(dbPath)); statErr == nil {
		t.Error("lock sidecar created despite live daemon; gate must not fire on the remote path")
	}
}
