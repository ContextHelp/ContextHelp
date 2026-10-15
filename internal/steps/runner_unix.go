//go:build unix

package steps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

// ProcessRunner runs the executable as a child process of dpkms:
//
//   - environment: only PATH=/usr/bin:/bin, HOME and TMPDIR (the run's
//     private working directory), LC_ALL=C and the protocol variables;
//     nothing from dpkms's own environment;
//   - working directory: a fresh 0700 directory holding the step config
//     file, removed after the run;
//   - descriptors: stdin, stdout and stderr only (Go opens every other
//     descriptor close-on-exec);
//   - its own process group, killed whole on timeout or output overflow;
//   - rlimits set by /bin/sh before exec: CPU seconds (the timeout),
//     open files, file size, and on Linux address space (max_memory).
//
// It does not isolate the filesystem or the network: the step can read
// whatever the dpkms user can, and reach any host. Steps that must not
// need container isolation.
type ProcessRunner struct{}

// processPath is the PATH a step process gets.
const processPath = "/usr/bin:/bin"

// rlimitScript is the /bin/sh prologue that applies inv's limits, then
// execs the executable ("$0") with no arguments.
func rlimitScript(inv Invocation, timeout time.Duration) string {
	cpu := int64(timeout.Seconds()) + 1
	parts := []string{
		fmt.Sprintf("ulimit -t %d", cpu),
		"ulimit -n 256",
		// File size in 512- or 1024-byte blocks, depending on the
		// shell: at least 1 GiB either way.
		"ulimit -f 2097152",
	}
	// RLIMIT_AS is not settable on macOS; Linux enforces it.
	if runtime.GOOS == "linux" && inv.Limits.MaxMemory > 0 {
		parts = append(parts, fmt.Sprintf("ulimit -v %d", inv.Limits.MaxMemory/1024))
	}
	return strings.Join(parts, " && ") + ` && exec "$0"`
}

// ProcessEnv is the complete environment a process-isolated step runs with.
func ProcessEnv(inv Invocation, workDir string) []string {
	env := map[string]string{
		"PATH":                   processPath,
		"HOME":                   workDir,
		"TMPDIR":                 workDir,
		"LC_ALL":                 "C",
		"DPKMS_STEP_CONFIG_PATH": filepath.Join(workDir, configFileName),
	}
	for k, v := range inv.Env {
		env[k] = v
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// Run runs inv as a child process.
func (ProcessRunner) Run(ctx context.Context, inv Invocation) (Result, error) {
	workDir, err := makeWorkDir(inv.WorkRoot, inv.Config)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	defer os.RemoveAll(workDir)

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

	exe := filepath.Join(inv.StepDir, filepath.FromSlash(inv.Executable))
	cmd := exec.CommandContext(runCtx, "/bin/sh", "-c", rlimitScript(inv, timeout), exe) // #nosec G204 -- fixed shell prologue; exe is the installed, digest-checked executable passed as $0
	cmd.Env = ProcessEnv(inv, workDir)
	cmd.Dir = workDir
	cmd.Stdin = bytes.NewReader(inv.Stdin)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// The whole group: the step may have forked.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second

	runErr := cmd.Run()
	res := Result{Stdout: stdout.buf.Bytes(), StderrTail: stderr.bytes()}
	return classify(res, runErr, stdout.overflowed(), ctx.Err(), errors.Is(runCtx.Err(), context.DeadlineExceeded), timeout)
}
