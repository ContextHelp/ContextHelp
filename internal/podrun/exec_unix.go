//go:build !windows

package podrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Run executes argv[0] with argv[1:] in its own process group, enforces
// opts.Timeout, and terminates the entire process group (SIGTERM -> grace ->
// SIGKILL) when the deadline expires or ctx is cancelled.
func (r *ExecRunner) Run(ctx context.Context, argv []string, opts RunOpts) (Result, error) {
	if len(argv) == 0 || argv[0] == "" {
		return Result{}, fmt.Errorf("podrun: empty argv")
	}

	callCtx := ctx
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	// #nosec G204 -- argv[0] is the LookPath-resolved pod binary and argv is
	// constructed from fixed verb templates without a local shell.
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = opts.Dir
	cmd.Env = buildEnv(opts.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = r.waitDelay()

	var devNull *os.File
	if opts.Stdin != nil {
		cmd.Stdin = bytes.NewReader(opts.Stdin)
	} else {
		f, err := os.Open(os.DevNull)
		if err != nil {
			return Result{}, fmt.Errorf("podrun: open %s: %w", os.DevNull, err)
		}
		devNull = f
		cmd.Stdin = f
	}
	if devNull != nil {
		defer func() { _ = devNull.Close() }()
	}

	stdout := newLimitedBuffer(MaxStdoutBytes)
	stderr := newLimitedBuffer(MaxStderrBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return Result{}, err
	}

	pgid := cmd.Process.Pid
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var waitErr error
	timedOut := false
	select {
	case waitErr = <-done:
	case <-callCtx.Done():
		timedOut = errors.Is(callCtx.Err(), context.DeadlineExceeded)
		waitErr = terminateGroup(pgid, r.grace(), done)
		if waitErr == nil {
			waitErr = callCtx.Err()
		}
	}

	res := Result{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		TimedOut: timedOut,
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		}
	}
	if timedOut {
		return res, context.DeadlineExceeded
	}
	if callCtx.Err() != nil {
		return res, callCtx.Err()
	}
	return res, waitErr
}

// Start launches a long-lived child (such as `pod forward`) in its own
// process group with stdin closed unless opts.Stdin is set.
func (r *ExecRunner) Start(ctx context.Context, argv []string, opts RunOpts) (Proc, error) {
	if len(argv) == 0 || argv[0] == "" {
		return nil, fmt.Errorf("podrun: empty argv")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// #nosec G204 -- argv[0] is the LookPath-resolved pod binary and argv is
	// constructed from fixed verb templates without a local shell.
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = opts.Dir
	cmd.Env = buildEnv(opts.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = r.waitDelay()

	var devNull *os.File
	if opts.Stdin != nil {
		cmd.Stdin = bytes.NewReader(opts.Stdin)
	} else {
		f, err := os.Open(os.DevNull)
		if err != nil {
			return nil, fmt.Errorf("podrun: open %s: %w", os.DevNull, err)
		}
		devNull = f
		cmd.Stdin = f
	}
	cmd.Stdout = newLimitedBuffer(MaxStdoutBytes)
	cmd.Stderr = newLimitedBuffer(MaxStderrBytes)

	if err := cmd.Start(); err != nil {
		if devNull != nil {
			_ = devNull.Close()
		}
		return nil, err
	}
	if devNull != nil {
		_ = devNull.Close()
	}

	p := &unixProc{
		cmd:   cmd,
		pgid:  cmd.Process.Pid,
		grace: r.grace(),
		done:  make(chan error, 1),
	}
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.waitErr = err
		p.waited = true
		p.mu.Unlock()
		p.done <- err
		close(p.done)
	}()

	// Stop the group if the launching context is cancelled.
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				_ = p.Close()
			case err := <-p.done:
				_ = err
			}
		}()
	}
	return p, nil
}

type unixProc struct {
	cmd     *exec.Cmd
	pgid    int
	grace   time.Duration
	done    chan error
	mu      sync.Mutex
	closed  bool
	waited  bool
	waitErr error
}

func (p *unixProc) PID() int { return p.pgid }

func (p *unixProc) Wait() error {
	for range p.done {
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

func (p *unixProc) Close() error {
	p.mu.Lock()
	if p.closed || p.waited {
		err := p.waitErr
		p.mu.Unlock()
		return err
	}
	p.closed = true
	p.mu.Unlock()

	return terminateGroup(p.pgid, p.grace, p.done)
}

// terminateGroup sends SIGTERM to the entire process group (-pgid), waits up
// to grace for exit, and escalates to SIGKILL on the group so grandchildren
// (ssh, docker, sky, gcloud) cannot outlive the parent pod process.
func terminateGroup(pgid int, grace time.Duration, done <-chan error) error {
	if pgid <= 0 {
		return nil
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)

	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		killWait := time.NewTimer(waitDelay)
		defer killWait.Stop()
		select {
		case err := <-done:
			return err
		case <-killWait.C:
			return context.DeadlineExceeded
		}
	}
}
