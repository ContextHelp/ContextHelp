package steps

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Invocation is one run of an installed step's executable on one object.
type Invocation struct {
	// StepDir is the step's install directory.
	StepDir string
	// Executable is the program, relative to StepDir.
	Executable string
	// Image is the container image (container isolation only).
	Image string
	// Env is the protocol environment (SANDBOX_*, DPKMS_*), without
	// DPKMS_STEP_CONFIG_PATH, which the runner sets for its own layout.
	Env map[string]string
	// Config is the step config JSON the runner puts at
	// DPKMS_STEP_CONFIG_PATH.
	Config []byte
	// Stdin is the knowledge object JSON.
	Stdin []byte
	// Timeout bounds the run's wall time.
	Timeout time.Duration
	// MaxOutput caps stdout in bytes.
	MaxOutput int64
	// Limits are the step's resource limits.
	Limits limits
	// Network permits outbound network access where the runner can
	// deny it (container isolation).
	Network bool
	// WorkRoot is where the runner makes the run's private working
	// directory; empty means os.TempDir().
	WorkRoot string
}

// Result is what a run wrote.
type Result struct {
	Stdout []byte
	// StderrTail is the last bytes of stderr, for debug logs only.
	StderrTail []byte
	// ExitCode is the process exit status; -1 when it was killed.
	ExitCode int
}

// Runner runs an installed step's executable under one isolation level.
type Runner interface {
	Run(ctx context.Context, inv Invocation) (Result, error)
}

// Run failures every runner reports the same way.
var (
	// ErrStepTimeout is a run that outlived its timeout and was killed.
	ErrStepTimeout = errors.New("timed out")
	// ErrOutputTooLarge is a run whose stdout passed its cap; it was killed.
	ErrOutputTooLarge = errors.New("output exceeds max_output")
	// ErrIsolationUnavailable is an isolation level this host cannot
	// provide (no docker, or no process runner on this OS).
	ErrIsolationUnavailable = errors.New("isolation unavailable")
)

// ExitError is a run that exited non-zero.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exited with status %d", e.Code) }

const (
	stderrTailBytes = 4 << 10
	configFileName  = "config.json"
	defaultTimeout  = 30 * time.Second
	defaultOutput   = 64 << 20
)

// makeWorkDir creates the run's private working directory (mode 0700)
// holding the step config file.
func makeWorkDir(root string, cfg []byte) (string, error) {
	if root == "" {
		root = os.TempDir()
	}
	dir, err := os.MkdirTemp(root, "dpkms-step-")
	if err != nil {
		return "", fmt.Errorf("step work dir: %w", err)
	}
	if cfg == nil {
		cfg = []byte("{}")
	}
	if err := os.WriteFile(filepath.Join(dir, configFileName), cfg, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("step config file: %w", err)
	}
	return dir, nil
}

// cappedBuffer keeps up to limit bytes and reports overflow through
// onOverflow (once), failing every write past the cap.
type cappedBuffer struct {
	mu         sync.Mutex
	buf        bytes.Buffer
	limit      int64
	over       bool
	onOverflow func()
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.over {
		return 0, ErrOutputTooLarge
	}
	if int64(c.buf.Len())+int64(len(p)) > c.limit {
		c.over = true
		if c.onOverflow != nil {
			c.onOverflow()
		}
		return 0, ErrOutputTooLarge
	}
	return c.buf.Write(p)
}

func (c *cappedBuffer) overflowed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.over
}

// tailBuffer keeps the last n bytes written.
type tailBuffer struct {
	mu  sync.Mutex
	n   int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.n {
		t.buf = t.buf[len(t.buf)-t.n:]
	}
	return len(p), nil
}

func (t *tailBuffer) bytes() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return bytes.Clone(t.buf)
}

// CommandFunc runs name with args, env, stdin and the two writers, and
// returns its exit status as *exec.ExitError (or any error). It is the
// container runner's seam over the docker CLI.
type CommandFunc func(ctx context.Context, name string, args, env []string, stdin io.Reader, stdout, stderr io.Writer) error

// ExecCommand is the real CommandFunc.
func ExecCommand(ctx context.Context, name string, args, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed docker CLI with argv built by ContainerRunner
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = 5 * time.Second
	return cmd.Run()
}

// ContainerRunner runs the executable inside a container through the
// docker CLI: no network unless the step declares it, read-only root
// and step files, all capabilities dropped, no new privileges, an
// unprivileged user, a pid limit, a private tmpfs, and the step's
// memory and CPU limits.
type ContainerRunner struct {
	// Docker is the docker CLI; empty means "docker" on PATH.
	Docker string
	// Command runs the CLI; nil means ExecCommand.
	Command CommandFunc
	// LookPath finds the CLI; nil means exec.LookPath.
	LookPath func(string) (string, error)
}

// dockerClientEnv is what the docker CLI itself needs from dpkms's
// environment to reach its daemon. None of it reaches the container:
// the step sees only the -e variables.
var dockerClientEnv = []string{"PATH", "HOME", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY"}

// ContainerArgs is the docker argv for inv, with the run's working
// directory mounted read-only at /run/dpkms and the container named name.
func ContainerArgs(inv Invocation, workDir, name string) []string {
	network := "none"
	if inv.Network {
		network = "bridge"
	}
	args := []string{
		"run", "--rm", "-i",
		"--name", name,
		"--network", network,
		"--read-only",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", "128",
		"--user", "65534:65534",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=64m",
		"-v", inv.StepDir + ":/step:ro",
		"-v", workDir + ":/run/dpkms:ro",
		"-w", "/tmp",
	}
	if inv.Limits.MaxMemory > 0 {
		args = append(args, "--memory", strconv.FormatUint(inv.Limits.MaxMemory, 10)+"b")
	}
	if inv.Limits.MaxCPU > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(inv.Limits.MaxCPU, 'f', -1, 64))
	}
	env := map[string]string{"HOME": "/tmp", "TMPDIR": "/tmp", "DPKMS_STEP_CONFIG_PATH": "/run/dpkms/" + configFileName}
	for k, v := range inv.Env {
		env[k] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+env[k])
	}
	return append(args, "--entrypoint", "/step/"+inv.Executable, inv.Image)
}

// Run runs inv in a fresh container.
func (r ContainerRunner) Run(ctx context.Context, inv Invocation) (Result, error) {
	if inv.Image == "" {
		return Result{ExitCode: -1}, fmt.Errorf("%w: container isolation needs execution.container_image in SKILL.md", ErrIsolationUnavailable)
	}
	docker := r.Docker
	if docker == "" {
		docker = "docker"
	}
	look := r.LookPath
	if look == nil {
		look = exec.LookPath
	}
	bin, err := look(docker)
	if err != nil {
		return Result{ExitCode: -1}, fmt.Errorf("%w: container isolation needs the docker CLI: %w", ErrIsolationUnavailable, err)
	}
	run := r.Command
	if run == nil {
		run = ExecCommand
	}

	workDir, err := makeWorkDir(inv.WorkRoot, inv.Config)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	defer os.RemoveAll(workDir)

	var suffix [6]byte
	_, _ = rand.Read(suffix[:])
	name := "dpkms-step-" + hex.EncodeToString(suffix[:])

	timeout := inv.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	maxOut := inv.MaxOutput
	if maxOut <= 0 {
		maxOut = defaultOutput
	}
	stdout := &cappedBuffer{limit: maxOut, onOverflow: cancel}
	stderr := &tailBuffer{n: stderrTailBytes}

	var env []string
	for _, k := range dockerClientEnv {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	runErr := run(runCtx, bin, ContainerArgs(inv, workDir, name), env, bytes.NewReader(inv.Stdin), stdout, stderr)
	res := Result{Stdout: stdout.buf.Bytes(), StderrTail: stderr.bytes()}
	if runCtx.Err() != nil {
		// Killing the CLI leaves the container running: remove it.
		rmCtx, rmCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		_ = run(rmCtx, bin, []string{"rm", "-f", name}, env, nil, io.Discard, io.Discard)
		rmCancel()
	}
	return classify(res, runErr, stdout.overflowed(), ctx.Err(), errors.Is(runCtx.Err(), context.DeadlineExceeded), timeout)
}

// classify turns a finished run into the shared errors: parentErr is
// the caller's context error, timedOut whether the run's own deadline
// passed.
func classify(res Result, runErr error, overflow bool, parentErr error, timedOut bool, timeout time.Duration) (Result, error) {
	switch {
	case overflow:
		res.ExitCode = -1
		return res, ErrOutputTooLarge
	case parentErr != nil:
		res.ExitCode = -1
		return res, parentErr
	case timedOut:
		res.ExitCode = -1
		return res, fmt.Errorf("%w after %s", ErrStepTimeout, timeout)
	}
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			res.ExitCode = ee.ExitCode()
			return res, &ExitError{Code: ee.ExitCode()}
		}
		res.ExitCode = -1
		return res, runErr
	}
	return res, nil
}
