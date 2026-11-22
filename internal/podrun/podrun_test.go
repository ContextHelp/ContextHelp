package podrun

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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
	"hop.top/kit/go/console/output"
)

func TestMain(m *testing.M) {
	testguard.Main(m, nil, "ctxt", "dpkms")
}

// fakeCall records one scripted invocation.
type fakeCall struct {
	argv []string
	opts RunOpts
}

type fakeResponse struct {
	res Result
	err error
}

type fakeRunner struct {
	mu          sync.Mutex
	calls       []fakeCall
	startCalls  []fakeCall
	runHandler  func(ctx context.Context, argv []string, opts RunOpts) (Result, error)
	startFn     func(ctx context.Context, argv []string, opts RunOpts) (Proc, error)
	byVerb      map[string]fakeResponse
	startErrors []error
}

func (f *fakeRunner) Run(ctx context.Context, argv []string, opts RunOpts) (Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{argv: append([]string(nil), argv...), opts: opts})
	handler := f.runHandler
	byVerb := f.byVerb
	f.mu.Unlock()

	if handler != nil {
		return handler(ctx, argv, opts)
	}
	if len(argv) >= 2 && byVerb != nil {
		if resp, ok := byVerb[argv[1]]; ok {
			return resp.res, resp.err
		}
	}
	return Result{}, fmt.Errorf("unexpected pod call: %v", argv)
}

func (f *fakeRunner) Start(ctx context.Context, argv []string, opts RunOpts) (Proc, error) {
	f.mu.Lock()
	f.startCalls = append(f.startCalls, fakeCall{argv: append([]string(nil), argv...), opts: opts})
	fn := f.startFn
	var err error
	if len(f.startErrors) > 0 {
		err = f.startErrors[0]
		f.startErrors = f.startErrors[1:]
	}
	f.mu.Unlock()

	if err != nil {
		return nil, err
	}
	if fn != nil {
		return fn(ctx, argv, opts)
	}
	return newFakeProc(4242), nil
}

type fakeProc struct {
	pid    int
	done   chan error
	once   sync.Once
	closed bool
	mu     sync.Mutex
}

func newFakeProc(pid int) *fakeProc {
	return &fakeProc{pid: pid, done: make(chan error, 1)}
}

func (p *fakeProc) PID() int    { return p.pid }
func (p *fakeProc) Wait() error { return <-p.done }
func (p *fakeProc) Close() error {
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		p.done <- nil
		close(p.done)
	})
	return nil
}

func TestDetectFor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix detection test")
	}

	got, err := DetectFor("daemon.runner", "local", func(name string) (string, error) {
		if name != "pod" {
			t.Fatalf("looked up %q, want pod", name)
		}
		return "/usr/local/bin/pod", nil
	})
	if err != nil || got != "/usr/local/bin/pod" {
		t.Fatalf("DetectFor success: got (%q, %v)", got, err)
	}

	for _, tc := range []struct {
		name string
		err  error
	}{
		{"not found", exec.ErrNotFound},
		{"relative path ErrDot", exec.ErrDot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DetectFor("daemon.runner", "local", func(string) (string, error) {
				return "", tc.err
			})
			var oe *output.Error
			if !errors.As(err, &oe) {
				t.Fatalf("want *output.Error, got %T (%v)", err, err)
			}
			if oe.ExitCode != 70 {
				t.Errorf("ExitCode = %d, want 70", oe.ExitCode)
			}
			if !strings.Contains(oe.Message, `daemon.runner is "pod" but pod is not on PATH`) {
				t.Errorf("unexpected message: %q", oe.Message)
			}
			if !strings.Contains(oe.SuggestedFix, "daemon.runner: local") {
				t.Errorf("unexpected fix: %q", oe.SuggestedFix)
			}
		})
	}
}

func TestDecodeEnvelopeAndMapError(t *testing.T) {
	stderr := []byte(`{
  "code": "NOT_FOUND",
  "message": "pod \"dev\" not found",
  "suggested_fix": "run ` + "`pod ps`" + ` to list known pods",
  "exit_code": 3
}

   ERROR
   Not_found: pod "dev" not found.
`)
	env, ok := DecodeEnvelope(stderr)
	if !ok {
		t.Fatal("DecodeEnvelope returned false")
	}
	if env.Code != "NOT_FOUND" || env.ExitCode != 3 || !strings.Contains(env.SuggestedFix, "pod ps") {
		t.Fatalf("unexpected envelope: %+v", env)
	}

	cases := []struct {
		name     string
		res      Result
		runErr   error
		wantExit int
		wantCode string
	}{
		{"cannot start", Result{}, errors.New("fork/exec: no such file"), 70, output.CodePrerequisite},
		{"timeout", Result{TimedOut: true}, context.DeadlineExceeded, 6, output.CodeTransient},
		{"exit 2 unknown verb", Result{ExitCode: 2, Stderr: []byte("USAGE: unknown command")}, errors.New("exit 2"), 70, output.CodePrerequisite},
		{"exit 3 not found", Result{ExitCode: 3, Stderr: stderr}, errors.New("exit 3"), 3, output.CodeNotFound},
		{"exit 4 conflict", Result{ExitCode: 4, Stderr: []byte("Error: policy denied")}, errors.New("exit 4"), 4, output.CodeConflict},
		{"exit 5 unauthorized", Result{ExitCode: 5, Stderr: []byte("Error: confirm required")}, errors.New("exit 5"), 5, output.CodeUnauthorized},
		{"exit 6 transient", Result{ExitCode: 6, Stderr: []byte("Error: unreachable")}, errors.New("exit 6"), 6, output.CodeTransient},
		{"exit 70 prerequisite", Result{ExitCode: 70, Stderr: []byte("Error: missing dep")}, errors.New("exit 70"), 70, output.CodePrerequisite},
		{"exit 130 cancelled", Result{ExitCode: 130}, errors.New("exit 130"), 130, output.CodeGeneric},
		{"exit 1 plain", Result{ExitCode: 1, Stderr: []byte("line 1\nboom\n")}, errors.New("exit 1"), 1, output.CodeGeneric},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oe := MapError("show", "dev", 15*time.Second, tc.res, tc.runErr)
			if oe.ExitCode != tc.wantExit || oe.Code != tc.wantCode {
				t.Fatalf("got (exit=%d, code=%s), want (exit=%d, code=%s); msg=%q",
					oe.ExitCode, oe.Code, tc.wantExit, tc.wantCode, oe.Message)
			}
		})
	}
}

func TestValidateTokenP9(t *testing.T) {
	valid := []string{"dev-pod", "my_pod.1", "/usr/local/bin/ctxd", "http://127.0.0.1:8080", "a+b=c@d"}
	for _, v := range valid {
		if err := ValidateToken("tok", v); err != nil {
			t.Errorf("ValidateToken(%q) unexpected err: %v", v, err)
		}
	}
	invalid := []string{"", "a b", "pod;rm", "$(id)", "a`b`", "a\nb", "a'b", `a"b`, "a|b", "a&b"}
	for _, v := range invalid {
		if err := ValidateToken("tok", v); err == nil {
			t.Errorf("ValidateToken(%q) want error, got nil", v)
		}
	}
}

func TestProbeAndRequireLeaves(t *testing.T) {
	fr := &fakeRunner{
		byVerb: map[string]fakeResponse{
			"capabilities": {
				res: Result{Stdout: []byte(`{"tool":"pod","version":"dev","schema_version":"1.0","leaves":[{"path":"pod show"},{"path":"pod start"},{"path":"pod stop"}]}`)},
			},
		},
	}
	c := NewClient("/bin/pod", fr, DefaultTimeouts())
	caps, err := c.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if err := RequireLeaves(caps, "pod show", "pod start"); err != nil {
		t.Fatalf("RequireLeaves existing: %v", err)
	}
	err = RequireLeaves(caps, "pod show", "pod exec")
	var oe *output.Error
	if !errors.As(err, &oe) || oe.ExitCode != 70 || !strings.Contains(oe.Message, `"pod exec"`) {
		t.Fatalf("expected exit 70 naming pod exec, got: %v", err)
	}

	// Wrong tool rejected with exit 70.
	fr.byVerb["capabilities"] = fakeResponse{
		res: Result{Stdout: []byte(`{"tool":"other","schema_version":"1.0","leaves":[]}`)},
	}
	if _, err := c.Probe(context.Background()); !errors.As(err, &oe) || oe.ExitCode != 70 {
		t.Fatalf("expected exit 70 for wrong tool, got: %v", err)
	}

	// Schema major != 1 rejected with exit 70.
	fr.byVerb["capabilities"] = fakeResponse{
		res: Result{Stdout: []byte(`{"tool":"pod","schema_version":"2.0","leaves":[]}`)},
	}
	if _, err := c.Probe(context.Background()); !errors.As(err, &oe) || oe.ExitCode != 70 {
		t.Fatalf("expected exit 70 for schema 2.0, got: %v", err)
	}
}

func TestPodGateStartRefusals(t *testing.T) {
	// Guarantee 7: destroyed and provisioning NEVER see pod start.
	t.Run("destroyed never starts and deletes record", func(t *testing.T) {
		fr := &fakeRunner{byVerb: map[string]fakeResponse{
			"show": {res: Result{Stdout: []byte(`{"name":"p1","provider":"docker","status":"destroyed"}`)}},
		}}
		c := NewClient("/bin/pod", fr, DefaultTimeouts())
		deleted := false
		_, err := c.GateForStart(context.Background(), "p1", true, "note", func() { deleted = true })
		var oe *output.Error
		if !errors.As(err, &oe) || oe.ExitCode != 3 {
			t.Fatalf("want exit 3, got %v", err)
		}
		if !deleted {
			t.Error("expected local record callback to run for destroyed pod")
		}
		if len(fr.calls) != 1 || fr.calls[0].argv[1] != "show" {
			t.Fatalf("expected only show call, got %v", fr.calls)
		}
	})

	t.Run("provisioning never starts", func(t *testing.T) {
		fr := &fakeRunner{byVerb: map[string]fakeResponse{
			"show": {res: Result{Stdout: []byte(`{"name":"p1","provider":"docker","status":"provisioning"}`)}},
		}}
		c := NewClient("/bin/pod", fr, DefaultTimeouts())
		_, err := c.GateForStart(context.Background(), "p1", true, "note", nil)
		var oe *output.Error
		if !errors.As(err, &oe) || oe.ExitCode != 6 {
			t.Fatalf("want exit 6, got %v", err)
		}
		if len(fr.calls) != 1 {
			t.Fatalf("expected only show call, got %v", fr.calls)
		}
	})

	t.Run("stopped with autostart false refuses with exit 2", func(t *testing.T) {
		fr := &fakeRunner{byVerb: map[string]fakeResponse{
			"show": {res: Result{Stdout: []byte(`{"name":"p1","provider":"docker","status":"stopped"}`)}},
		}}
		c := NewClient("/bin/pod", fr, DefaultTimeouts())
		_, err := c.GateForStart(context.Background(), "p1", false, "note", nil)
		var oe *output.Error
		if !errors.As(err, &oe) || oe.ExitCode != 2 {
			t.Fatalf("want exit 2, got %v", err)
		}
		if len(fr.calls) != 1 {
			t.Fatalf("expected only show call, got %v", fr.calls)
		}
	})
}

func TestPodGateAutostartAndQuery(t *testing.T) {
	t.Run("stopped with autostart true warns and starts once", func(t *testing.T) {
		fr := &fakeRunner{byVerb: map[string]fakeResponse{
			"show":  {res: Result{Stdout: []byte(`{"name":"p1","provider":"docker","status":"stopped"}`)}},
			"start": {res: Result{Stdout: []byte(`{"name":"p1","provider":"docker","status":"running"}`)}},
		}}
		var warn bytes.Buffer
		c := NewClient("/bin/pod", fr, DefaultTimeouts())
		c.Warn = &warn
		info, err := c.GateForStart(context.Background(), "p1", true, "ctxt daemon", nil)
		if err != nil {
			t.Fatalf("GateForStart: %v", err)
		}
		if info.Status != "running" {
			t.Fatalf("status = %q, want running", info.Status)
		}
		if !strings.Contains(warn.String(), "docker") {
			t.Errorf("expected billing warning naming provider, got %q", warn.String())
		}
		if len(fr.calls) != 2 || fr.calls[1].argv[1] != "start" {
			t.Fatalf("expected show+start calls, got %v", fr.calls)
		}
	})

	t.Run("query on stopped and destroyed reports unreachable without error", func(t *testing.T) {
		for _, status := range []string{"stopped", "destroyed"} {
			fr := &fakeRunner{byVerb: map[string]fakeResponse{
				"show": {res: Result{Stdout: []byte(`{"name":"p1","provider":"docker","status":"` + status + `"}`)}},
			}}
			c := NewClient("/bin/pod", fr, DefaultTimeouts())
			deleted := false
			info, ok, err := c.GateForQuery(context.Background(), "p1", func() { deleted = true })
			if err != nil || ok || info.Status != status {
				t.Fatalf("status=%s: got (%+v, %v, %v)", status, info, ok, err)
			}
			if status == "destroyed" && !deleted {
				t.Errorf("destroyed pod should delete local record")
			}
		}
	})
}

func TestForwardRetriesBusyLocalPortF13(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	goodPort := ln.Addr().(*net.TCPAddr).Port

	fr := &fakeRunner{
		startErrors: []error{errors.New("bind: address already in use"), nil},
	}
	ports := []int{19991, goodPort}
	idx := 0
	c := NewClient("/bin/pod", fr, DefaultTimeouts())
	c.ForwardReadyTimeout = 2 * time.Second
	c.PickLocalPort = func() (int, error) {
		p := ports[idx]
		idx++
		return p, nil
	}
	proc, bound, err := c.Forward(context.Background(), "dev", 0, 8080)
	if err != nil {
		t.Fatalf("Forward retry failed: %v", err)
	}
	defer func() { _ = proc.Close() }()
	if bound != goodPort {
		t.Fatalf("bound port = %d, want %d", bound, goodPort)
	}
	if len(fr.startCalls) != 2 {
		t.Fatalf("expected 2 forward attempts, got %d", len(fr.startCalls))
	}
}

// TestGuarantee4NoHangNoLeftovers exercises ExecRunner against real Unix shell
// scripts: sleeping script, SIGTERM-ignoring script, stdin-reading script, and
// a script that spawns a grandchild process.
func TestGuarantee4NoHangNoLeftovers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix process-group guarantee test")
	}
	dir := t.TempDir()
	runner := &ExecRunner{KillGrace: 150 * time.Millisecond, WaitDelay: 500 * time.Millisecond}

	writeScript := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("sleeper killed at deadline", func(t *testing.T) {
		s := writeScript("sleep.sh", "sleep 30")
		start := time.Now()
		res, err := runner.Run(context.Background(), []string{s}, RunOpts{Timeout: 120 * time.Millisecond})
		elapsed := time.Since(start)
		if !errors.Is(err, context.DeadlineExceeded) || !res.TimedOut {
			t.Fatalf("want DeadlineExceeded + TimedOut, got (res=%+v, err=%v)", res, err)
		}
		if elapsed > 3*time.Second {
			t.Fatalf("took too long: %s", elapsed)
		}
	})

	t.Run("sigterm-ignoring script sigkilled after grace", func(t *testing.T) {
		s := writeScript("ignore_term.sh", "trap '' TERM\nsleep 30")
		start := time.Now()
		res, err := runner.Run(context.Background(), []string{s}, RunOpts{Timeout: 100 * time.Millisecond})
		elapsed := time.Since(start)
		if !errors.Is(err, context.DeadlineExceeded) || !res.TimedOut {
			t.Fatalf("want DeadlineExceeded + TimedOut, got (res=%+v, err=%v)", res, err)
		}
		if elapsed > 3*time.Second {
			t.Fatalf("took too long: %s", elapsed)
		}
	})

	t.Run("stdin-blocking script receives /dev/null EOF immediately", func(t *testing.T) {
		s := writeScript("read_stdin.sh", "read line || exit 0\necho \"$line\"")
		start := time.Now()
		res, err := runner.Run(context.Background(), []string{s}, RunOpts{Timeout: 2 * time.Second})
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("want immediate exit 0, got (res=%+v, err=%v)", res, err)
		}
		if time.Since(start) > time.Second {
			t.Fatalf("stdin read stalled: %s", time.Since(start))
		}
	})

	t.Run("grandchild dies with process group SIGKILL", func(t *testing.T) {
		pidFile := filepath.Join(dir, "grandchild.pid")
		s := writeScript("spawn.sh", "trap '' TERM\n(trap '' TERM; sleep 30) &\necho $! > "+pidFile+"\nwait")
		_, err := runner.Run(context.Background(), []string{s}, RunOpts{Timeout: 150 * time.Millisecond})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("want DeadlineExceeded, got %v", err)
		}
		raw, rerr := os.ReadFile(pidFile)
		if rerr != nil {
			t.Fatalf("grandchild pidfile missing: %v", rerr)
		}
		pid, perr := strconv.Atoi(strings.TrimSpace(string(raw)))
		if perr != nil || pid <= 0 {
			t.Fatalf("bad grandchild pid %q: %v", string(raw), perr)
		}
		// Give the kernel a brief moment to reap the SIGKILLed grandchild.
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if err := exec.Command("kill", "-0", strconv.Itoa(pid)).Run(); err != nil {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("grandchild pid %d survived process-group SIGKILL", pid)
	})
}
