package pidfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func pidPath(dir string, port int) string {
	return filepath.Join(dir, fmt.Sprintf("%d.pid", port))
}

// A file Scan cannot parse may be one a writer has not finished (or a
// writer this version does not know): Scan skips it and leaves it be.
func TestScanKeepsUnparseablePidfile(t *testing.T) {
	for name, body := range map[string]string{"empty": "", "partial": `{"pid":`} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(pidPath(dir, testPort), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			infos, err := Scan(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(infos) != 0 {
				t.Errorf("Scan = %+v, want no entry for an unparseable file", infos)
			}
			if _, err := os.Stat(pidPath(dir, testPort)); err != nil {
				t.Errorf("Scan removed a pidfile it could not parse: %v", err)
			}
		})
	}
}

// Write replaces the pidfile with a new file, never rewriting the old
// one in place: a reader holding the old file reads it whole, and no
// staging file is left beside it.
func TestWriteReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, Info{PID: os.Getpid(), Name: "old", Port: testPort}); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(pidPath(dir, testPort))
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(pidPath(dir, testPort))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := Write(dir, Info{PID: os.Getpid(), Name: "new", Port: testPort}); err != nil {
		t.Fatal(err)
	}
	held, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(held) != string(old) {
		t.Errorf("open pidfile rewritten in place: read %q, want %q", held, old)
	}
	if got := readInfo(t, dir, testPort); got.Name != "new" {
		t.Errorf("pidfile name = %q, want new", got.Name)
	}
	entries, _ := os.ReadDir(dir)
	entries = slices.DeleteFunc(entries, func(e os.DirEntry) bool { return e.Name() == lockName })
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("dir holds %v, want only the pidfile", names)
	}
}

// A live instance rewriting its pidfile while readers scan never loses
// it: no Scan sees a half-written file and prunes it.
func TestConcurrentWriteScanKeepsLivePidfile(t *testing.T) {
	dir := t.TempDir()
	info := Info{PID: os.Getpid(), Name: "home", Port: testPort, DBPath: filepath.Join(dir, "db.sqlite")}
	if err := Write(dir, info); err != nil {
		t.Fatal(err)
	}
	var stop atomic.Bool
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				_, _ = Scan(dir)
			}
		}()
	}
	var lost int
	for i := 0; i < 300 && lost == 0; i++ { // an in-place write loses the file within a few
		if err := Write(dir, info); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		if _, err := os.Stat(pidPath(dir, testPort)); errors.Is(err, os.ErrNotExist) {
			lost = i + 1
		}
	}
	stop.Store(true)
	wg.Wait()
	if lost > 0 {
		t.Fatalf("a concurrent Scan removed the live pidfile (write %d)", lost)
	}
	if _, err := os.Stat(pidPath(dir, testPort)); err != nil {
		t.Fatalf("live pidfile gone after concurrent scans: %v", err)
	}
}

// Scan judges an entry stale from the file it read. A new instance
// registering on the same port meanwhile waits for the scan (the run
// dir's lock): Scan removes the stale file it judged, never the one
// that replaces it.
func TestScanKeepsPidfileReplacedWhileJudgedStale(t *testing.T) {
	dir := t.TempDir()
	written := time.Now().Add(-time.Hour)
	if err := Write(dir, Info{PID: os.Getpid(), Name: "old", Port: testPort, StartedAt: written}); err != nil {
		t.Fatal(err)
	}
	orig := processStartTime
	t.Cleanup(func() { processStartTime = orig })
	wrote := make(chan error, 1)
	processStartTime = func(int) (time.Time, error) {
		// A new instance on the same port registers while Scan is
		// deciding about the old entry, whose pid looks reused. Give
		// its write the time to land, if nothing holds it back.
		go func() { wrote <- Write(dir, Info{PID: os.Getpid(), Name: "new", Port: testPort}) }()
		select {
		case err := <-wrote:
			wrote <- err
		case <-time.After(300 * time.Millisecond):
		}
		return time.Now(), nil
	}

	if _, err := Scan(dir); err != nil {
		t.Fatal(err)
	}
	if err := <-wrote; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pidPath(dir, testPort)); err != nil {
		t.Fatalf("Scan removed the pidfile that replaced a stale one: %v", err)
	}
	if got := readInfo(t, dir, testPort); got.Name != "new" {
		t.Errorf("pidfile name = %q, want new", got.Name)
	}
}

// A pid this user may not signal (EPERM: pid 1, owned by root) is a
// running process: an instance another user runs from a shared run dir
// (sudo, a shared --data-dir) stays listed and its pidfile stays.
func TestScanKeepsAnotherUsersInstance(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a pid this user may not signal: pid 1 as a non-root user")
	}
	dir := t.TempDir()
	if err := Write(dir, Info{PID: 1, Name: "shared", Port: testPort}); err != nil {
		t.Fatal(err)
	}
	infos, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].PID != 1 {
		t.Errorf("Scan = %+v, want pid 1 live", infos)
	}
	if _, err := os.Stat(pidPath(dir, testPort)); err != nil {
		t.Errorf("pidfile of a running process removed: %v", err)
	}
}
