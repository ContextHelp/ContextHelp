// Package podrun implements the opt-in process boundary between ctxt/dpkms
// and the external `pod` CLI. It shells out via os/exec only (no Go module
// dependency on pod), enforces per-call deadlines and process-group teardown,
// probes `pod capabilities --format json` before invoking remote verbs, and
// maps pod exit codes and JSON error envelopes onto kit error classes.
package podrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"hop.top/kit/go/console/output"
)

const (
	// BinaryName is the external CLI binary looked up on PATH.
	BinaryName = "pod"

	// MaxStdoutBytes caps captured stdout per pod call (1 MiB).
	MaxStdoutBytes = 1 << 20
	// MaxStderrBytes caps captured stderr per pod call (64 KiB).
	MaxStderrBytes = 64 << 10

	// killGrace is how long a timed-out or cancelled child process group gets
	// after SIGTERM before SIGKILL is sent to the whole process group.
	killGrace = 5 * time.Second
	// waitDelay releases stdout/stderr pipes after group SIGKILL.
	waitDelay = 6 * time.Second
)

// RunOpts configures one pod subprocess invocation.
type RunOpts struct {
	// Timeout bounds the call when positive.
	Timeout time.Duration
	// Env appends KEY=VALUE pairs on top of the inherited environment and
	// NO_COLOR=1.
	Env []string
	// Stdin supplies bytes to stdin when non-nil; nil attaches /dev/null.
	Stdin []byte
	// Dir sets the child working directory when non-empty.
	Dir string
}

// Result captures the outcome of one completed pod invocation.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	TimedOut bool
}

// Proc represents a long-lived pod subprocess (for example `pod forward`).
type Proc interface {
	// PID returns the lead process ID, or 0 if not started / synthetic.
	PID() int
	// Wait blocks until the subprocess exits.
	Wait() error
	// Close terminates the subprocess group cleanly (SIGTERM -> grace -> SIGKILL).
	Close() error
}

// Runner is the single seam through which podrun starts `pod` subprocesses.
// Production uses ExecRunner; tests inject scripted fakes or xrr cassette wrappers.
type Runner interface {
	Run(ctx context.Context, argv []string, opts RunOpts) (Result, error)
	Start(ctx context.Context, argv []string, opts RunOpts) (Proc, error)
}

// Detect locates the `pod` binary on PATH for daemon.runner=pod. It never
// searches beside the current executable and treats relative PATH matches
// (exec.ErrDot) as absent. On Windows it returns a prerequisite error (exit 70).
func Detect() (string, error) {
	return DetectFor("daemon.runner", "local", exec.LookPath)
}

// DetectFor locates `pod` on PATH using lookPath, formatting the exit-70
// prerequisite message for settingName (e.g. "daemon.runner" or "jobs.executor").
func DetectFor(settingName, defaultVal string, lookPath func(string) (string, error)) (string, error) {
	if runtime.GOOS == "windows" {
		return "", windowsUnsupportedError(settingName)
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	path, err := lookPath(BinaryName)
	if err != nil {
		// Treat exec.ErrDot (relative PATH entry) identically to not found.
		e := &output.Error{
			Code:         output.CodePrerequisite,
			Message:      fmt.Sprintf("%s is \"pod\" but pod is not on PATH", settingName),
			SuggestedFix: fmt.Sprintf("install pod, or set %s: %s", settingName, defaultVal),
			ExitCode:     exitCodePrerequisite(),
		}
		if !errors.Is(err, exec.ErrNotFound) && !errors.Is(err, exec.ErrDot) {
			e.Cause = err.Error()
		}
		return "", e
	}
	return path, nil
}

func windowsUnsupportedError(settingName string) *output.Error {
	return &output.Error{
		Code:         output.CodePrerequisite,
		Message:      fmt.Sprintf("%s=pod is not supported on windows yet", settingName),
		SuggestedFix: fmt.Sprintf("set %s: local on Windows", settingName),
		ExitCode:     exitCodePrerequisite(),
	}
}

func exitCodePrerequisite() int {
	if code, ok := output.ExitCodeForClass(output.CodePrerequisite); ok {
		return code
	}
	return 70
}

// ExecRunner implements Runner via os/exec with process-group teardown,
// closed stdin by default, bounded output buffers, and NO_COLOR=1.
type ExecRunner struct {
	// KillGrace overrides killGrace when positive (useful in fast tests).
	KillGrace time.Duration
	// WaitDelay overrides waitDelay when positive (useful in fast tests).
	WaitDelay time.Duration
}

// NewExecRunner constructs a default ExecRunner.
func NewExecRunner() *ExecRunner {
	return &ExecRunner{}
}

func (r *ExecRunner) grace() time.Duration {
	if r != nil && r.KillGrace > 0 {
		return r.KillGrace
	}
	return killGrace
}

func (r *ExecRunner) waitDelay() time.Duration {
	if r != nil && r.WaitDelay > 0 {
		return r.WaitDelay
	}
	return waitDelay
}

// buildEnv inherits the parent environment (cloud/adapter credentials live
// there), adds NO_COLOR=1, and appends caller-supplied entries.
func buildEnv(extra []string) []string {
	parent := os.Environ()
	env := make([]string, 0, len(parent)+1+len(extra))
	env = append(env, parent...)
	env = append(env, "NO_COLOR=1")
	env = append(env, extra...)
	return env
}

// limitedBuffer caps captured bytes without returning write errors to the child.
type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{max: limit}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	rem := b.max - b.buf.Len()
	if rem > 0 {
		if len(p) > rem {
			_, _ = b.buf.Write(p[:rem])
		} else {
			_, _ = b.buf.Write(p)
		}
	}
	// Always report full write so excess output is dropped rather than
	// killing the subprocess with SIGPIPE.
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, b.buf.Len())
	copy(out, b.buf.Bytes())
	return out
}
