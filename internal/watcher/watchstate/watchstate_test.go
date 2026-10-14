package watchstate_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/watcher"
	"github.com/ideacrafterslabs/ctxt/internal/watcher/watchstate"
)

var ctx = context.Background()

func TestPathsArePerInstanceAndFlat(t *testing.T) {
	dir := t.TempDir()
	keys := []string{"home", "laptop", "http://127.0.0.1:8080", "https://dpkms.example.ts.net:7700", "../escape", ".."}
	seen := map[string]string{}
	for _, k := range keys {
		for _, p := range []string{watchstate.StatePath(dir, k), watchstate.RunPath(dir, k)} {
			if filepath.Dir(p) != dir {
				t.Errorf("key %q: %s is not directly in %s", k, p, dir)
			}
			if other, dup := seen[p]; dup {
				t.Errorf("keys %q and %q share %s", k, other, p)
			}
			seen[p] = k
		}
	}
	if got := watchstate.StatePath(dir, "home"); got != filepath.Join(dir, "home.state") {
		t.Errorf("StatePath(home) = %s; want home.state", got)
	}
}

func TestStoreRoundTripsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.state")
	s, err := watchstate.Open(path, "home")
	if err != nil {
		t.Fatal(err)
	}
	rec := watcher.FileRecord{Path: "/w/a.md", ContentHash: "h1", JobID: "job_1", LastSeen: time.Now().UTC()}
	if err := s.PutFile(ctx, "/w", rec); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLastError(ctx, "/w", "boom"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetClipboardHash("clip"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := watchstate.Open(path, "home")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, ok, err := s2.GetFile(ctx, "/w", "/w/a.md")
	if err != nil || !ok || got.ContentHash != "h1" || got.JobID != "job_1" {
		t.Fatalf("GetFile after reopen = %+v, %v, %v", got, ok, err)
	}
	if s2.ClipboardHash() != "clip" {
		t.Errorf("clipboard hash lost across reopen")
	}

	snap, err := watchstate.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	d := snap.Dirs["/w"]
	if snap.Instance != "home" || len(d.Files) != 1 || d.LastError != "boom" || d.LastErrorAt.IsZero() || d.LastEnqueueAt.IsZero() {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestDeleteFileForgets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.state")
	s, err := watchstate.Open(path, "home")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.PutFile(ctx, "/w", watcher.FileRecord{Path: "/w/a.md", ContentHash: "h"})
	if err := s.DeleteFile(ctx, "/w", "/w/a.md"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetFile(ctx, "/w", "/w/a.md"); ok {
		t.Fatal("record survived DeleteFile")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	snap, _ := watchstate.Read(path)
	if n := len(snap.Dirs["/w"].Files); n != 0 {
		t.Fatalf("file record persisted after delete: %d", n)
	}
}

// Writes land as whole files: 0600, versioned JSON, no temp files left.
func TestFlushWritesWholeFileAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "home.state")
	s, err := watchstate.Open(path, "home")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.PutFile(ctx, "/w", watcher.FileRecord{Path: "/w/a.md", ContentHash: "h"})
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v; want 0600", fi.Mode().Perm())
	}
	var raw map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &raw); err != nil || raw["version"] != float64(1) || raw["instance"] != "home" {
		t.Fatalf("file = %s (%v)", data, err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	_ = s.Close()
}

// A corrupt file is an error and is never rewritten.
func TestCorruptFileIsRefusedAndKept(t *testing.T) {
	for name, body := range map[string]string{
		"not json":      "{nope",
		"wrong version": `{"version":99,"instance":"home"}`,
		"other key":     `{"version":1,"instance":"laptop"}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "home.state")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := watchstate.Open(path, "home"); !errors.Is(err, watchstate.ErrCorrupt) {
				t.Fatalf("Open = %v; want ErrCorrupt", err)
			}
			if _, err := watchstate.Read(path); name != "other key" && !errors.Is(err, watchstate.ErrCorrupt) {
				t.Fatalf("Read = %v; want ErrCorrupt", err)
			}
			if data, _ := os.ReadFile(path); string(data) != body {
				t.Fatalf("corrupt file rewritten: %q", data)
			}
		})
	}
}

func TestReadMissingIsEmpty(t *testing.T) {
	snap, err := watchstate.Read(filepath.Join(t.TempDir(), "none.state"))
	if err != nil || len(snap.Dirs) != 0 {
		t.Fatalf("Read(missing) = %+v, %v", snap, err)
	}
}

// Two instances keep separate files: a record under one is absent from
// the other.
func TestInstancesKeepSeparateState(t *testing.T) {
	dir := t.TempDir()
	home, err := watchstate.Open(watchstate.StatePath(dir, "home"), "home")
	if err != nil {
		t.Fatal(err)
	}
	laptop, err := watchstate.Open(watchstate.StatePath(dir, "laptop"), "laptop")
	if err != nil {
		t.Fatal(err)
	}
	_ = home.PutFile(ctx, "/w", watcher.FileRecord{Path: "/w/a.md", ContentHash: "h"})
	_ = home.Close()
	_ = laptop.Close()

	laptop, _ = watchstate.Open(watchstate.StatePath(dir, "laptop"), "laptop")
	defer laptop.Close()
	if _, ok, _ := laptop.GetFile(ctx, "/w", "/w/a.md"); ok {
		t.Fatal("laptop sees home's record")
	}
}

func TestRunLockExcludesSecondStarter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.run")
	info := watchstate.RunInfo{Instance: "home", Endpoint: "http://127.0.0.1:1", Dirs: []string{"/w"}}
	run, err := watchstate.AcquireRun(path, info)
	if err != nil {
		t.Fatal(err)
	}

	got, alive, err := watchstate.ReadRun(path)
	if err != nil || !alive || got.PID != os.Getpid() || got.Instance != "home" || len(got.Dirs) != 1 {
		t.Fatalf("ReadRun while held = %+v, %v, %v", got, alive, err)
	}

	var running *watchstate.RunningError
	if _, err := watchstate.AcquireRun(path, info); !errors.As(err, &running) || running.Info.PID != os.Getpid() {
		t.Fatalf("second AcquireRun = %v; want RunningError naming this pid", err)
	}

	if err := run.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("run file left after Release: %v", err)
	}
	if _, alive, _ := watchstate.ReadRun(path); alive {
		t.Error("alive after Release")
	}
	again, err := watchstate.AcquireRun(path, info)
	if err != nil {
		t.Fatalf("AcquireRun after Release: %v", err)
	}
	_ = again.Release()
}

// A run file whose process died without releasing it is stale: nothing
// holds its lock.
func TestStaleRunFileIsNotAlive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.run")
	if err := os.WriteFile(path, []byte(`{"version":1,"pid":999999,"instance":"home"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	info, alive, err := watchstate.ReadRun(path)
	if err != nil || alive || info.PID != 999999 {
		t.Fatalf("ReadRun(stale) = %+v, %v, %v", info, alive, err)
	}
	run, err := watchstate.AcquireRun(path, watchstate.RunInfo{Instance: "home"})
	if err != nil {
		t.Fatalf("AcquireRun over a stale file: %v", err)
	}
	_ = run.Release()
}

// The lock is held by the process, not the file: another process sees
// it alive. Checked with a real child holding the lock via flock(1)
// when available.
func TestRunLockVisibleAcrossProcesses(t *testing.T) {
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock(1) not installed")
	}
	path := filepath.Join(t.TempDir(), "home.run")
	if err := os.WriteFile(path, []byte(`{"version":1,"pid":1,"instance":"home"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("flock", path, "sleep", "5") // #nosec G204 -- test fixture
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, alive, _ := watchstate.ReadRun(path); alive {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("lock held by another process never read as alive")
}
