package pidfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Ports recorded in the throwaway pidfiles. Nothing binds them; they only
// name the files. Kept clear of the default and reserved local ports.
const (
	testPort      = 47631
	testOtherPort = 47632
)

func readInfo(t *testing.T, dir string, port int) Info {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("%d.pid", port)))
	if err != nil {
		t.Fatalf("read pidfile: %v", err)
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("decode pidfile: %v", err)
	}
	return info
}

// sleeper starts a disposable child that stays alive for the test, so its
// pid is genuinely running and belongs to another process.
func sleeper(t *testing.T) int {
	t.Helper()
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	return child.Process.Pid
}

// A pidfile left behind by a previous run that happened to get this
// process's pid (a container's PID 1 after a crash restart) cannot
// belong to a running instance: this process has not registered yet.
// The startup check must not report it as a conflict, and serve's write
// must then replace it.
func TestCheckNameConflict_OwnPIDEntryIsStale(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	if err := Write(dir, Info{PID: os.Getpid(), Name: "home", Port: testPort, StartedAt: old}); err != nil {
		t.Fatal(err)
	}
	// Same pid under another port: the previous run fell back to a
	// different port. It is just as stale.
	if err := Write(dir, Info{PID: os.Getpid(), Name: "home", Port: testOtherPort, StartedAt: old}); err != nil {
		t.Fatal(err)
	}

	if err := CheckNameConflict(dir, "home"); err != nil {
		t.Fatalf("own-pid leftover reported as running instance: %v", err)
	}
	for _, port := range []int{testPort, testOtherPort} {
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("%d.pid", port))); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stale own-pid pidfile %d.pid not pruned: %v", port, err)
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := Write(dir, Info{PID: os.Getpid(), Name: "home", Port: testPort, StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	if got := readInfo(t, dir, testPort); !got.StartedAt.Equal(now) {
		t.Errorf("pidfile not replaced: started_at = %v, want %v", got.StartedAt, now)
	}
	infos, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Port != testPort {
		t.Errorf("live entries after restart = %+v, want only port %d", infos, testPort)
	}
}

// A different, genuinely running process holding the name is still a
// conflict, and its pidfile is left alone.
func TestCheckNameConflict_LiveOtherPIDConflicts(t *testing.T) {
	dir := t.TempDir()
	pid := sleeper(t)
	if err := Write(dir, Info{PID: pid, Name: "home", Port: testPort}); err != nil {
		t.Fatal(err)
	}

	if err := CheckNameConflict(dir, "home"); err == nil {
		t.Fatal("live instance of another process not reported as conflict")
	}
	if got := readInfo(t, dir, testPort); got.PID != pid {
		t.Errorf("live pidfile changed: pid = %d, want %d", got.PID, pid)
	}
	if err := CheckNameConflict(dir, "work"); err != nil {
		t.Errorf("unrelated name reported as conflict: %v", err)
	}
}

// Only the startup check discounts this process's own pid. Every other
// reader (ps, shutdown, reboot, ctxt) runs in a different process from
// the instance it inspects, and must keep seeing a live entry as live.
func TestScan_OwnPIDEntryStaysLive(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, Info{PID: os.Getpid(), Name: "home", Port: testPort, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	infos, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].PID != os.Getpid() {
		t.Fatalf("Scan = %+v, want the own-pid entry", infos)
	}
}

// A dead pid is pruned as before.
func TestCheckNameConflict_DeadPIDIsStale(t *testing.T) {
	dir := t.TempDir()
	child := exec.Command("true")
	if err := child.Run(); err != nil {
		t.Fatalf("run child: %v", err)
	}
	if err := Write(dir, Info{PID: child.Process.Pid, Name: "home", Port: testPort}); err != nil {
		t.Fatal(err)
	}
	if err := CheckNameConflict(dir, "home"); err != nil {
		t.Fatalf("dead pid reported as conflict: %v", err)
	}
}

// A pid that the OS handed to an unrelated process after the instance
// died is not that instance. The process started after the entry was
// written, so it cannot be the process that wrote it: startup must not
// report a conflict, and Scan (ps, shutdown, reboot, ctxt) must prune
// the entry rather than offer the stranger's pid as a signal target.
func TestReusedPIDIsStale(t *testing.T) {
	dir := t.TempDir()
	written := time.Now().Add(-time.Hour)
	pid := sleeper(t)
	if err := Write(dir, Info{PID: pid, Name: "home", Port: testPort, StartedAt: written}); err != nil {
		t.Fatal(err)
	}

	infos, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 0 {
		t.Fatalf("Scan = %+v, want reused pid %d pruned", infos, pid)
	}
	if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("%d.pid", testPort))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("reused-pid pidfile not pruned: %v", err)
	}

	if err := Write(dir, Info{PID: pid, Name: "home", Port: testPort, StartedAt: written}); err != nil {
		t.Fatal(err)
	}
	if err := CheckNameConflict(dir, "home"); err != nil {
		t.Fatalf("reused pid reported as running instance: %v", err)
	}
}

// The process that wrote the entry started before it wrote it, so an
// entry whose pid started earlier than started_at is the live instance.
func TestPIDStartedBeforeEntryIsLive(t *testing.T) {
	dir := t.TempDir()
	pid := sleeper(t)
	if err := Write(dir, Info{PID: pid, Name: "home", Port: testPort, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	infos, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].PID != pid {
		t.Fatalf("Scan = %+v, want live pid %d", infos, pid)
	}
	if err := CheckNameConflict(dir, "home"); err == nil {
		t.Fatal("live instance not reported as conflict")
	}
}

// When the OS will not report the start time, identity cannot be
// checked: the entry stays live, as it was before the check existed.
func TestUnreadableStartTimeIsLive(t *testing.T) {
	orig := processStartTime
	processStartTime = func(int) (time.Time, error) { return time.Time{}, errors.New("permission denied") }
	t.Cleanup(func() { processStartTime = orig })

	dir := t.TempDir()
	pid := sleeper(t)
	if err := Write(dir, Info{PID: pid, Name: "home", Port: testPort, StartedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	infos, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].PID != pid {
		t.Fatalf("Scan = %+v, want pid %d kept", infos, pid)
	}
	if err := CheckNameConflict(dir, "home"); err == nil {
		t.Fatal("entry with unreadable start time not reported as conflict")
	}
}

// The access class rides in the pidfile for discovery (dpkms ps).
func TestWrite_RecordsAccess(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, Info{PID: os.Getpid(), Name: "home", Port: testPort, Access: "protected", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if got := readInfo(t, dir, testPort).Access; got != "protected" {
		t.Errorf("pidfile access = %q; want protected", got)
	}
}
