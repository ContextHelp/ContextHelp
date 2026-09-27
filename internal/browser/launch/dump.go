package launch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrRejected means every attempt produced a complete document but
// Options.Accept refused the last one.
var ErrRejected = errors.New("rendered DOM not accepted")

// Defaults for Options fields left zero.
const (
	DefaultTimeout     = 20 * time.Second
	DefaultPageTimeout = 10 * time.Second
)

// waitDelay bounds how long a killed run may hold its output pipes. A
// variable so tests can widen it past the fake Chrome's lifetime, which
// makes a run that was not group-killed on cancel outlive it visibly.
var waitDelay = 5 * time.Second

// Options tunes DumpDOM. The zero value renders once with Chrome found by
// FindChrome.
type Options struct {
	// Chrome is the executable; empty means FindChrome.
	Chrome string
	// Args are extra Chrome switches. A switch the package sets itself
	// is refused with ErrReservedFlag.
	Args []string
	// Timeout bounds one attempt, from start to a complete document.
	Timeout time.Duration
	// PageTimeout is Chrome's own budget for the page (--timeout) before
	// it dumps whatever it has.
	PageTimeout time.Duration
	// Attempts is how many renders to try before giving up; at least 1.
	Attempts int
	// Accept, when set, decides whether a complete document is the one
	// wanted; a refused document is retried. A page that fills itself in
	// from script may be dumped before it has.
	Accept func(dom string) bool
	// Env is Chrome's environment; nil inherits this process's.
	Env []string
	// Dir is Chrome's working directory; empty inherits this process's.
	Dir string
	// TempDir is where the throwaway profile is created; empty means
	// os.TempDir. The profile is removed after every attempt.
	TempDir string
}

// DumpDOM renders url in headless Chrome and returns the serialised DOM.
//
// Chrome writes the DOM and may then keep running, for instance while a
// page animates, so each attempt kills Chrome's whole process group as
// soon as the closing </html> arrives rather than waiting for it to
// exit. An attempt that fails, times out or is refused by Accept is
// retried up to Attempts. The last attempt's DOM is returned with its
// error; ErrRejected when it was complete but not accepted.
func DumpDOM(ctx context.Context, url string, opts Options) (string, error) {
	if err := checkURL(url); err != nil {
		return "", err
	}
	if opts.Chrome == "" {
		p, err := FindChrome()
		if err != nil {
			return "", err
		}
		opts.Chrome = p
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.PageTimeout <= 0 {
		opts.PageTimeout = DefaultPageTimeout
	}
	attempts := max(opts.Attempts, 1)

	var dom string
	var err error
	for range attempts {
		dom, err = dumpOnce(ctx, url, opts)
		if err == nil && (opts.Accept == nil || opts.Accept(dom)) {
			return dom, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err == nil {
		err = fmt.Errorf("%d attempt(s): %w", attempts, ErrRejected)
	}
	return dom, err
}

// dumpOnce runs one headless Chrome --dump-dom of url in a fresh profile
// and removes the profile afterwards.
func dumpOnce(ctx context.Context, url string, opts Options) (_ string, err error) {
	profile, err := os.MkdirTemp(opts.TempDir, "ctxt-chrome-")
	if err != nil {
		return "", fmt.Errorf("launch: profile dir: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(profile); rmErr != nil && err == nil {
			err = fmt.Errorf("launch: remove profile: %w", rmErr)
		}
	}()
	owned := []string{
		"--timeout=" + strconv.FormatInt(opts.PageTimeout.Milliseconds(), 10),
		"--dump-dom",
	}
	argv, err := headlessArgs(profile, needsNoSandbox(os.Geteuid(), runtime.GOOS, os.Getenv("CI") != ""),
		opts.Args, owned, []string{url})
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, opts.Chrome, argv...) //nolint:gosec // browser executable chosen by the caller or FindChrome; switches fixed here
	cmd.Env = opts.Env
	cmd.Dir = opts.Dir
	setGroup(cmd)
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = waitDelay
	var stderr tailBuffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start chrome: %w", err)
	}
	// Reap Chrome and everything it spawned before the profile goes.
	var (
		reaped  bool
		waitErr error
	)
	reap := func() {
		if !reaped {
			reaped = true
			_ = killGroup(cmd)
			waitErr = cmd.Wait()
		}
	}
	defer reap()

	var dom strings.Builder
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		dom.WriteString(sc.Text())
		dom.WriteByte('\n')
		if strings.Contains(sc.Text(), "</html>") {
			return dom.String(), nil
		}
	}
	// Stderr is copied by a goroutine that Wait joins: stdout can reach
	// EOF before that copy has run, so read the tail only once reaped.
	reap()
	if ctx.Err() != nil {
		return "", fmt.Errorf("no complete DOM: %w; chrome stderr tail:\n%s", ctx.Err(), stderr.String())
	}
	how := "chrome exited"
	if waitErr != nil {
		how += " (" + waitErr.Error() + ")"
	}
	return "", fmt.Errorf("no complete DOM: %s; stderr tail:\n%s", how, stderr.String())
}

// tailBuffer keeps the last 4 KiB written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if n := len(b.buf) - 4096; n > 0 {
		b.buf = b.buf[n:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
