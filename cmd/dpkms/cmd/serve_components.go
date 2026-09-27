package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	gohttp "net/http"
	"os"
	"sort"
	"syscall"
	"time"
)

// component is one long-running part of serve. run blocks until ctx is
// cancelled (returning nil, or the context error) or the component fails
// (returning that error). A nil return before cancellation means the
// component has nothing left to do; it does not stop the others.
type component struct {
	name string
	run  func(ctx context.Context) error
}

// componentRunner runs serve's components as one unit: the first failure
// or shutdown signal cancels every component, and run returns once all
// have stopped or the drain timeout expires, whichever comes first.
type componentRunner struct {
	components []component
	// signals delivers shutdown requests. It is read for the runner's
	// whole lifetime, draining included, so a signal is never swallowed
	// by a registered-but-unread channel.
	signals <-chan os.Signal
	// drain bounds the time between the stop decision and run returning.
	drain time.Duration
	// log receives failures and shutdown progress.
	log io.Writer
}

type componentExit struct {
	name string
	err  error
}

// run starts every component and blocks until they have all returned or
// the drain timeout has expired after the stop decision. It returns the
// first component failure, a drain-timeout error, or nil after a clean
// signal-initiated shutdown.
//
// A signal received while already stopping ends the wait at once: the
// operator asked twice, and the process must exit.
func (r *componentRunner) run(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	exits := make(chan componentExit, len(r.components))
	running := make(map[string]bool, len(r.components))
	for _, c := range r.components {
		running[c.name] = true
		go func(c component) {
			exits <- componentExit{name: c.name, err: c.run(ctx)}
		}(c)
	}

	var (
		failure  error
		stopping bool
		deadline <-chan time.Time
		// parentDone is cleared once seen so a closed parent does not
		// spin the loop while components drain.
		parentDone = parent.Done()
	)
	stop := func() {
		if stopping {
			return
		}
		stopping = true
		cancel()
		deadline = time.After(r.drain)
		fmt.Fprintf(r.log, "Stopping all components (drain up to %s)...\n", r.drain)
	}

	for len(running) > 0 {
		select {
		case ex := <-exits:
			delete(running, ex.name)
			if ex.err == nil || (stopping && errors.Is(ex.err, context.Canceled)) {
				continue
			}
			fmt.Fprintf(r.log, "error: %s: %v\n", ex.name, ex.err)
			if failure == nil {
				failure = fmt.Errorf("%s: %w", ex.name, ex.err)
			}
			stop()
		case sig := <-r.signals:
			if stopping {
				fmt.Fprintf(r.log, "Received %s while stopping; exiting without waiting for %v\n", sig, sortedNames(running))
				return errors.Join(failure, fmt.Errorf("shutdown interrupted by %s; still running: %v", sig, sortedNames(running)))
			}
			if sig == syscall.SIGHUP {
				fmt.Fprintln(r.log, "Received SIGHUP — restarting...")
			} else {
				fmt.Fprintln(r.log, "Shutting down gracefully...")
			}
			stop()
		case <-parentDone:
			parentDone = nil
			stop()
		case <-deadline:
			fmt.Fprintf(r.log, "error: shutdown timed out after %s; still running: %v\n", r.drain, sortedNames(running))
			return errors.Join(failure, fmt.Errorf("shutdown timed out after %s; still running: %v", r.drain, sortedNames(running)))
		}
	}
	return failure
}

func sortedNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// httpComponent serves srv on ln until ctx is cancelled, then shuts it
// down within drain. A Serve failure is returned as the component error.
func httpComponent(name string, srv *gohttp.Server, ln net.Listener, drain time.Duration) component {
	return component{name: name, run: func(ctx context.Context) error {
		served := make(chan error, 1)
		go func() { served <- srv.Serve(ln) }()
		select {
		case err := <-served:
			if errors.Is(err, gohttp.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			shutCtx, shutCancel := context.WithTimeout(context.Background(), drain)
			defer shutCancel()
			if err := srv.Shutdown(shutCtx); err != nil {
				_ = srv.Close()
				return fmt.Errorf("shutdown: %w", err)
			}
			return nil
		}
	}}
}
