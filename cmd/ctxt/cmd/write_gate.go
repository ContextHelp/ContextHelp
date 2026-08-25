package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// cliRetryBudget bounds how long a write command waits for another CLI to
// release the database lock. Daemon contention never waits here: if the
// daemon held the lock and were answering its API, the request would have
// routed to it instead of reaching the direct path.
const cliRetryBudget = 2 * time.Second

// localDirectAnalyze enqueues content straight into local storage — the
// fallback when no daemon answers. The advisory database lock is taken
// BEFORE storage opens, so concurrent lock-taking writers (other CLI
// commands today) serialize here, and a daemon mid-startup that holds the
// lock is not raced. That daemon-side half is pending: the serve path does
// not yet acquire the lock, so this gate alone does not rule out a daemon
// writing concurrently — it rules out every writer that takes the lock.
func localDirectAnalyze(ctx context.Context, req service.AnalyzeRequest) (string, error) {
	dbPath, err := resolveStoragePath()
	if err != nil {
		return "", err
	}
	if dbPath == "" {
		return "", fmt.Errorf("storage path not configured")
	}

	lock, err := acquireWriteLock(ctx, dbPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.Release() }()

	svc, cleanup, err := newService()
	if err != nil {
		return "", err
	}
	defer cleanup()

	return svc.Analyze(ctx, req)
}

// acquireWriteLock takes the CLI-side database lock non-blocking. Contention
// against another CLI gets a brief bounded retry (CLI commands are short);
// contention against a daemon fails immediately with an instructive error —
// the daemon path was already probed and did not answer.
func acquireWriteLock(ctx context.Context, dbPath string) (*dblock.Lock, error) {
	lock, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleCLI})
	if err == nil {
		return lock, nil
	}
	var held *dblock.HeldError
	if !errors.As(err, &held) {
		return nil, err
	}
	if held.Holder != nil && held.Holder.Role == dblock.RoleCLI {
		lock, err = dblock.AcquireRetry(ctx, dbPath, dblock.Info{Role: dblock.RoleCLI}, cliRetryBudget)
		if err == nil {
			return lock, nil
		}
		if !errors.As(err, &held) {
			return nil, err
		}
	}
	return nil, contentionError(held)
}

// contentionError renders a held database lock as an instructive, attributed
// error: name the holder, state the resolution, never silently degrade the
// write.
func contentionError(held *dblock.HeldError) error {
	h := held.Holder
	switch {
	case h != nil && h.Role == dblock.RoleDaemon:
		return fmt.Errorf(
			"database is in use by dpkms (pid %d%s, since %s) and the daemon is not answering its API\n"+
				"  → wait for the daemon to finish starting, or stop it (`dpkms shutdown`) and retry",
			h.PID, daemonInstanceLabel(h.Instance), h.StartedAt.Format("15:04:05"))
	case h != nil:
		return fmt.Errorf(
			"database is in use by another ctxt command (pid %d, since %s)\n"+
				"  → retry when it finishes",
			h.PID, h.StartedAt.Format("15:04:05"))
	default:
		return fmt.Errorf("database is locked (%s)\n  → retry shortly", held.Path)
	}
}

func daemonInstanceLabel(instance string) string {
	if instance == "" {
		return ""
	}
	return fmt.Sprintf(", instance %q", instance)
}
