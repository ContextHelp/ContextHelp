//go:build e2e && unix

package searchgraph_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/browser/launch"
)

// runTimeout bounds every one-shot invocation; a hang fails the test
// instead of the run.
const runTimeout = 60 * time.Second

// home is a hermetic HOME: config, database and XDG roots all live under
// dir, and a fake browser opener shadows the system one on PATH.
type home struct {
	dir     string
	cfg     string
	db      string
	openLog string // one line per fake browser launch
	fakeBin string
}

// newHome writes a config pointing storage at dir/data/ctxt.db, the
// dpkms client at serverURL and the embedding provider at embedURL.
func newHome(dir, serverURL, embedURL string) (*home, error) {
	h := &home{
		dir:     dir,
		cfg:     filepath.Join(dir, "ctxt.yaml"),
		db:      filepath.Join(dir, "data", "ctxt.db"),
		openLog: filepath.Join(dir, "browser-open.log"),
		fakeBin: filepath.Join(dir, "fakebin"),
	}
	for _, d := range []string{filepath.Dir(h.db), h.fakeBin} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	cfg := fmt.Sprintf(`storage:
  path: %q
server:
  url: %q
providers:
  embedding:
    backend: ollama
    endpoint: %q
`, h.db, serverURL, embedURL)
	if err := os.WriteFile(h.cfg, []byte(cfg), 0o600); err != nil {
		return nil, err
	}
	// The viewer opens a browser with the platform handler launch.Open
	// execs, which resolves through PATH, so this records the launch
	// instead.
	name, _, ok := launch.OpenCommand(runtime.GOOS, "")
	if !ok {
		return nil, fmt.Errorf("no default browser handler on %s", runtime.GOOS)
	}
	opener := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\n", h.openLog)
	if err := os.WriteFile(filepath.Join(h.fakeBin, name), []byte(opener), 0o700); err != nil { //nolint:gosec // test-only executable stub
		return nil, err
	}
	return h, nil
}

// environ is the process environment: nothing ctxt, dpkms, kit, XDG or
// git inherited from the caller.
func (h *home) environ() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(k, "CTXT_"), strings.HasPrefix(k, "CH_"),
			strings.HasPrefix(k, "DPKMS_"), strings.HasPrefix(k, "KIT_"),
			strings.HasPrefix(k, "XDG_"), strings.HasPrefix(k, "GIT_"),
			k == "HOME", k == "NO_COLOR", k == "TERM":
			continue
		case k == "PATH":
			kv = "PATH=" + h.fakeBin + string(os.PathListSeparator) + v
		}
		out = append(out, kv)
	}
	return append(out,
		"HOME="+h.dir,
		"XDG_CONFIG_HOME="+filepath.Join(h.dir, "config"),
		"XDG_DATA_HOME="+filepath.Join(h.dir, "share"),
		"XDG_STATE_HOME="+filepath.Join(h.dir, "state"),
		"XDG_CACHE_HOME="+filepath.Join(h.dir, "cache"),
		"CTXT_CONFIG="+h.cfg,
		"CTXT_NO_CLIPBOARD=1",
		"TERM=dumb",
	)
}

// browserLaunches returns what the fake opener recorded.
func (h *home) browserLaunches() []string {
	b, err := os.ReadFile(h.openLog)
	if err != nil {
		return nil
	}
	return strings.Fields(string(b))
}

// env is one test's home, seeded with a private copy of the corpus.
type env struct {
	t *testing.T
	*home
}

type envOption func(*envConfig)

type envConfig struct{ embedURL string }

// withEmbedding routes the embedding provider to url instead of a closed
// port.
func withEmbedding(url string) envOption {
	return func(c *envConfig) { c.embedURL = url }
}

func newEnv(t *testing.T, opts ...envOption) *env {
	t.Helper()
	c := envConfig{embedURL: closedURL()}
	for _, o := range opts {
		o(&c)
	}
	h, err := newHome(t.TempDir(), closedURL(), c.embedURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := suite.corpus.copyDB(h.db); err != nil {
		t.Fatal(err)
	}
	return &env{t: t, home: h}
}

// result is one finished invocation.
type result struct {
	stdout, stderr string
	code           int
}

func (r result) String() string {
	return fmt.Sprintf("exit %d\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
}

// command returns an unstarted invocation of the binary in its own
// process group, so a cleanup can reap it and anything it spawned.
func (e *env) command(ctx context.Context, args ...string) *exec.Cmd {
	return e.commandOf(ctx, suite.bin, args...)
}

// commandOf is command for any program, run in the test's environment.
func (e *env) commandOf(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // the binary under test or a test tool
	cmd.Env = e.environ()
	cmd.Dir = e.dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// run executes the binary to completion.
func (e *env) run(args ...string) result {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := e.command(ctx, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	r := result{stdout: out.String(), stderr: errb.String(), code: exitCode(err)}
	if ctx.Err() != nil {
		e.t.Fatalf("ctxt %s: timed out after %s\n%s", strings.Join(args, " "), runTimeout, r)
	}
	if r.code < 0 {
		e.t.Fatalf("ctxt %s: %v\n%s", strings.Join(args, " "), err, r)
	}
	return r
}

// mustRun executes the binary and fails unless it exits 0.
func (e *env) mustRun(args ...string) result {
	e.t.Helper()
	r := e.run(args...)
	if r.code != 0 {
		e.t.Fatalf("ctxt %s: want exit 0\n%s", strings.Join(args, " "), r)
	}
	return r
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.Exited() {
		return ee.ExitCode()
	}
	return -1
}

// killGroup SIGKILLs the process group led by cmd.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// closedURL returns a loopback URL nothing listens on: the port is
// reserved, then released.
func closedURL() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return "http://" + addr
}
