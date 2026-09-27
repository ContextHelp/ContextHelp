//go:build unix

package launch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeChromeScript stands in for Chrome. Each run records its argv and
// working directory, writes into the profile it was given as Chrome
// would, starts a long-lived child in its process group, then behaves
// per FAKE_MODE: "ok" prints a complete document and lingers, "flaky"
// prints an unready document on its first run, "exit" fails without
// output, "hang" prints nothing.
//
// Timing never decides a test: the fake reports its own progress. Once
// its child is recorded it writes its run number to the FAKE_READY fifo
// (when set). Only a run that outlives its child writes exited-<n>, and
// only a child that lives its 30s writes child-exited-<n>, so a run or
// child that was killed never has one. The child is this test binary
// (TestFakeChromeChild): one process that sleeps in-process, so it is in
// the process group from the moment the fake prints anything, and no
// fork of its own can race a group kill.
const fakeChromeScript = `#!/bin/sh
n=$(( $(cat "$FAKE_DIR/count" 2>/dev/null || echo 0) + 1 ))
echo "$n" > "$FAKE_DIR/count"
: > "$FAKE_DIR/args-$n"
for a in "$@"; do
  printf '%s\n' "$a" >> "$FAKE_DIR/args-$n"
  case $a in --user-data-dir=*)
    p=${a#--user-data-dir=}
    mkdir -p "$p/Default" && echo '{}' > "$p/Default/Preferences"
    echo "$p" > "$FAKE_DIR/profile-$n";;
  esac
done
pwd > "$FAKE_DIR/cwd-$n"
if [ "$FAKE_MODE" = exit ]; then echo "fake chrome: boom" >&2; exit 3; fi
FAKE_CHILD_MARKER="$FAKE_DIR/child-exited-$n" "$FAKE_TESTBIN" -test.run='^TestFakeChromeChild$' &
echo $! > "$FAKE_DIR/child-$n"
if [ -n "$FAKE_READY" ]; then echo "$n" > "$FAKE_READY"; fi
case $FAKE_MODE in
flaky)
  if [ "$n" -lt 2 ]; then echo '<html><body>loading</body></html>'
  else echo '<html><body data-ready="1">done</body></html>'; fi;;
hang) ;;
*) echo '<html><head></head>'; echo '<body>ok</body></html>';;
esac
wait
: > "$FAKE_DIR/exited-$n"
`

type fakeChrome struct {
	t    *testing.T
	dir  string
	path string
}

func newFakeChrome(t *testing.T) *fakeChrome {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "chrome")
	if err := os.WriteFile(path, []byte(fakeChromeScript), 0o700); err != nil { //nolint:gosec // test-only executable stub
		t.Fatal(err)
	}
	return &fakeChrome{t: t, dir: dir, path: path}
}

// opts returns Options running the fake in mode, profiles under a
// directory the test can inspect.
func (f *fakeChrome) opts(mode string) Options {
	self, err := os.Executable()
	if err != nil {
		f.t.Fatal(err)
	}
	return Options{
		Chrome:  f.path,
		Env:     append(os.Environ(), "FAKE_DIR="+f.dir, "FAKE_MODE="+mode, "FAKE_TESTBIN="+self),
		TempDir: f.t.TempDir(),
	}
}

// TestFakeChromeChild is the fake Chrome's long-lived child, not a test:
// it runs only when the fake starts this binary with FAKE_CHILD_MARKER,
// holds the output pipes it inherited for 30s, then writes the marker.
func TestFakeChromeChild(t *testing.T) {
	marker := os.Getenv("FAKE_CHILD_MARKER")
	if marker == "" {
		t.Skip("helper process for the fake Chrome")
	}
	time.Sleep(30 * time.Second)
	_ = os.WriteFile(marker, nil, 0o600)
	os.Exit(0)
}

func (f *fakeChrome) read(name string, run int) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, name+"-"+strconv.Itoa(run)))
	if err != nil {
		f.t.Fatalf("run %d: %v", run, err)
	}
	return strings.TrimSpace(string(b))
}

func (f *fakeChrome) runs() int {
	b, _ := os.ReadFile(filepath.Join(f.dir, "count"))
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// assertReaped checks run's child, which shared Chrome's process group,
// is dead. The child is not ours to wait for: once killed it stays a
// zombie until init reaps it, so a zombie counts as dead. SIGKILL is
// pending when DumpDOM returns; the loop only covers the kernel
// finishing delivery.
func (f *fakeChrome) assertReaped(run int) {
	f.t.Helper()
	pid, err := strconv.Atoi(f.read("child", run))
	if err != nil {
		f.t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			f.t.Fatalf("run %d: child %d outlived the render", run, pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// alive reports whether pid exists and is not a zombie.
func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output() //nolint:gosec // fixed tool; pid is the fake's child
	if err != nil {
		return false // ps exits non-zero once the pid is gone
	}
	return !strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}

// assertKilled checks run and its child were killed: neither reached its
// natural exit.
func (f *fakeChrome) assertKilled(run int) {
	f.t.Helper()
	for _, marker := range []string{"exited", "child-exited"} {
		if _, err := os.Stat(filepath.Join(f.dir, marker+"-"+strconv.Itoa(run))); !errors.Is(err, os.ErrNotExist) {
			f.t.Errorf("run %d: %s marker present (stat err %v); Chrome was waited for, not killed", run, marker, err)
		}
	}
}

// widenWaitDelay lifts waitDelay past the fake child's 30s for one test.
// A cancelled run that only killed Chrome, not its group, then keeps
// blocking on the child's pipes until the child writes its natural-exit
// marker, which assertKilled reports; exec's 5s default would close the
// pipes first and hide it.
func widenWaitDelay(t *testing.T) {
	t.Helper()
	old := waitDelay
	waitDelay = time.Minute
	t.Cleanup(func() { waitDelay = old })
}

// readyFIFO makes a fifo the fake writes its run number to once its
// child is recorded, and returns its path and a channel receiving each
// run number read from it.
func (f *fakeChrome) readyFIFO() (string, <-chan int) {
	f.t.Helper()
	path := filepath.Join(f.dir, "ready")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		f.t.Fatal(err)
	}
	ch := make(chan int, 1)
	go func() {
		// Opening blocks until the fake opens its end.
		b, err := os.ReadFile(path)
		if err != nil {
			close(ch)
			return
		}
		n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		ch <- n
	}()
	return path, ch
}

// assertProfileRemoved checks run's profile existed under parent and is
// gone.
func (f *fakeChrome) assertProfileRemoved(run int, parent string) {
	f.t.Helper()
	p := f.read("profile", run)
	if filepath.Dir(p) != parent {
		f.t.Errorf("run %d: profile %s not under TempDir %s", run, p, parent)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		f.t.Errorf("run %d: profile %s left behind (stat err %v)", run, p, err)
	}
}

func TestDumpDOM_ReturnsDocumentAndCleansUp(t *testing.T) {
	f := newFakeChrome(t)
	o := f.opts("ok")
	o.Args = []string{"--window-size=800,600"}
	o.Dir = t.TempDir()
	// An attempt timeout past the fake's 30s linger, so only the kill on
	// a complete document can end the run before the fake exits by itself.
	o.Timeout = time.Minute
	dom, err := DumpDOM(context.Background(), "http://127.0.0.1:1/page", o)
	if err != nil {
		t.Fatal(err)
	}
	// DumpDOM has reaped the fake, so no natural-exit marker means the
	// whole group was killed, not waited for.
	f.assertKilled(1)
	if want := "<html><head></head>\n<body>ok</body></html>\n"; dom != want {
		t.Errorf("dom = %q, want %q", dom, want)
	}
	argv := strings.Split(f.read("args", 1), "\n")
	assertMandatory(t, argv, f.read("profile", 1), o.Args)
	for _, w := range []string{"--dump-dom", "--timeout=10000", "--window-size=800,600"} {
		if !slices.Contains(argv, w) {
			t.Errorf("argv lacks %s: %q", w, argv)
		}
	}
	if argv[len(argv)-1] != "http://127.0.0.1:1/page" {
		t.Errorf("target not last: %q", argv)
	}
	if got, _ := filepath.EvalSymlinks(f.read("cwd", 1)); got != mustEval(t, o.Dir) {
		t.Errorf("cwd = %s, want %s", got, o.Dir)
	}
	f.assertProfileRemoved(1, o.TempDir)
	f.assertReaped(1)
}

func TestDumpDOM_RetriesUntilAccepted(t *testing.T) {
	f := newFakeChrome(t)
	o := f.opts("flaky")
	o.Attempts = 3
	o.Accept = func(dom string) bool { return strings.Contains(dom, "data-ready") }
	dom, err := DumpDOM(context.Background(), "file:///tmp/graph.html", o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dom, "done") || f.runs() != 2 {
		t.Errorf("dom = %q after %d runs, want the second run's", dom, f.runs())
	}
	for run := 1; run <= 2; run++ {
		f.assertProfileRemoved(run, o.TempDir)
		f.assertReaped(run)
	}
	if f.read("profile", 1) == f.read("profile", 2) {
		t.Error("attempts shared a profile")
	}
}

func TestDumpDOM_RejectedReturnsLastDOM(t *testing.T) {
	f := newFakeChrome(t)
	o := f.opts("ok")
	o.Attempts = 2
	o.Accept = func(string) bool { return false }
	dom, err := DumpDOM(context.Background(), "http://127.0.0.1:1/", o)
	if !errors.Is(err, ErrRejected) || !strings.Contains(dom, "ok") || f.runs() != 2 {
		t.Errorf("dom=%q err=%v runs=%d, want last DOM, ErrRejected, 2 runs", dom, err, f.runs())
	}
}

func TestDumpDOM_ChromeExitsWithoutDocument(t *testing.T) {
	f := newFakeChrome(t)
	o := f.opts("exit")
	o.Attempts = 2
	_, err := DumpDOM(context.Background(), "http://127.0.0.1:1/", o)
	if err == nil || !strings.Contains(err.Error(), "chrome exited (exit status 3)") || !strings.Contains(err.Error(), "fake chrome: boom") {
		t.Errorf("err = %v, want exit status with stderr tail", err)
	}
	if f.runs() != 2 {
		t.Errorf("runs = %d, want 2", f.runs())
	}
	f.assertProfileRemoved(2, o.TempDir)
}

// The attempt timeout fires however far the fake got, so this asserts
// only what holds at any point: the deadline error, a killed run and an
// empty profile parent. Group kill and profile cleanup on a cancelled
// run in a known state are pinned by TestDumpDOM_CancelKillsGroup.
func TestDumpDOM_AttemptTimeout(t *testing.T) {
	f := newFakeChrome(t)
	o := f.opts("hang")
	o.Timeout = 300 * time.Millisecond
	widenWaitDelay(t)
	_, err := DumpDOM(context.Background(), "http://127.0.0.1:1/", o)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want deadline exceeded", err)
	}
	if f.runs() > 1 {
		t.Errorf("runs = %d, want at most 1", f.runs())
	}
	f.assertKilled(1)
	if left, _ := os.ReadDir(o.TempDir); len(left) != 0 {
		t.Errorf("profile left behind: %v", left)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "child-1")); err == nil {
		f.assertReaped(1)
	}
}

// Cancelling a run whose Chrome is up and has a child kills the whole
// group and removes the profile. The fake signals readiness itself.
func TestDumpDOM_CancelKillsGroup(t *testing.T) {
	f := newFakeChrome(t)
	o := f.opts("hang")
	fifo, ready := f.readyFIFO()
	o.Env = append(o.Env, "FAKE_READY="+fifo)
	widenWaitDelay(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		if n, ok := <-ready; ok && n == 1 {
			cancel()
		}
	}()
	_, err := DumpDOM(ctx, "http://127.0.0.1:1/", o)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
	f.assertKilled(1)
	f.assertProfileRemoved(1, o.TempDir)
	f.assertReaped(1)
}

func TestDumpDOM_ReservedSwitchNeverStartsChrome(t *testing.T) {
	f := newFakeChrome(t)
	o := f.opts("ok")
	o.Args = []string{"--password-store=keychain"}
	if _, err := DumpDOM(context.Background(), "http://127.0.0.1:1/", o); !errors.Is(err, ErrReservedFlag) {
		t.Errorf("err = %v, want ErrReservedFlag", err)
	}
	if f.runs() != 0 {
		t.Errorf("chrome ran %d times", f.runs())
	}
	if left, _ := os.ReadDir(o.TempDir); len(left) != 0 {
		t.Errorf("profile left behind: %v", left)
	}
}

func TestDumpDOM_UsesEnvChrome(t *testing.T) {
	f := newFakeChrome(t)
	o := f.opts("ok")
	o.Chrome = ""
	t.Setenv(EnvChrome, f.path)
	if _, err := DumpDOM(context.Background(), "http://127.0.0.1:1/", o); err != nil {
		t.Fatal(err)
	}
	if f.runs() != 1 {
		t.Errorf("fake from %s ran %d times, want 1", EnvChrome, f.runs())
	}
	t.Setenv(EnvChrome, "off")
	if _, err := DumpDOM(context.Background(), "http://127.0.0.1:1/", o); !errors.Is(err, ErrDisabled) {
		t.Errorf("err = %v, want ErrDisabled", err)
	}
}

func TestDumpDOM_RefusesSwitchLikeURL(t *testing.T) {
	f := newFakeChrome(t)
	if _, err := DumpDOM(context.Background(), "--remote-debugging-port=9222", f.opts("ok")); err == nil {
		t.Error("switch-like target accepted")
	}
	if f.runs() != 0 {
		t.Errorf("chrome ran %d times", f.runs())
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
