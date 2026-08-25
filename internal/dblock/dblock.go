// Package dblock coordinates single-writer access to a database file through
// an advisory flock on a "<dbpath>.lock" sidecar file.
//
// Advisory means the gate binds only processes that acquire it. Today that is
// the CLI's direct write path (RoleCLI); the daemon's serve path does not yet
// take the lock on startup, so RoleDaemon holders appear only where daemon
// behavior is simulated (tests) — daemon-side acquisition is pending, and
// until it lands this package is one half of the single-writer story, not a
// system-wide guarantee.
//
// The flock is the authority: it is kernel-owned, released automatically on
// process exit (no stale-lock recovery needed), atomic to acquire (no
// check-then-act window), and same-host only — matching SQLite's own WAL
// constraints. After acquiring, the holder truncate-writes a small JSON body
// ({pid, role, instance, started_at}) into the sidecar; the body is purely
// diagnostic, letting a contender name the holder in error messages.
//
// The sidecar is never unlinked on release: flock state is per-fd, and
// removing the file would race other waiters that already hold an fd to it
// (see internal/cursor/lock.go for the same rule).
package dblock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// Role identifies the kind of process holding the lock.
type Role string

const (
	// RoleDaemon marks a long-lived dpkms serve process.
	RoleDaemon Role = "daemon"
	// RoleCLI marks a short-lived per-command holder.
	RoleCLI Role = "cli"
)

const (
	// sidecarSuffix is appended to the database path to form the lock path.
	sidecarSuffix = ".lock"
	// filePerm restricts the sidecar to the owning user, matching the
	// database file's access model.
	filePerm = 0o600
	// retryInterval is the poll cadence for AcquireRetry.
	retryInterval = 50 * time.Millisecond
)

// ErrHeld is the sentinel wrapped by every contention failure.
// Use errors.Is(err, ErrHeld) to detect "someone else holds the lock".
var ErrHeld = errors.New("database lock held")

// Info is the diagnostic lock body written by the holder after acquiring.
// The flock is the authority; Info exists for error attribution only.
type Info struct {
	PID       int       `json:"pid"`
	Role      Role      `json:"role"`
	Instance  string    `json:"instance,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// HeldError reports a failed acquire, carrying the holder identity when the
// lock body was readable. It wraps ErrHeld.
type HeldError struct {
	// Path is the sidecar lock path that was contended.
	Path string
	// Holder is the recorded holder identity; nil when the body could not
	// be read or parsed.
	Holder *Info
}

// Error renders the contention with holder attribution when available.
func (e *HeldError) Error() string {
	if e.Holder == nil {
		return fmt.Sprintf("database lock held: %s", e.Path)
	}
	return fmt.Sprintf("database lock held by %s (pid %d%s, since %s): %s",
		e.Holder.Role, e.Holder.PID, instanceLabel(e.Holder.Instance),
		e.Holder.StartedAt.Format(time.RFC3339), e.Path)
}

// Unwrap makes errors.Is(err, ErrHeld) true for HeldError values.
func (e *HeldError) Unwrap() error { return ErrHeld }

func instanceLabel(instance string) string {
	if instance == "" {
		return ""
	}
	return fmt.Sprintf(", instance %q", instance)
}

// Lock is a held advisory lock. Release it with Release; the kernel also
// releases it automatically when the holding process exits.
type Lock struct {
	f    *os.File
	path string
}

// Path returns the sidecar lock path guarding dbPath.
func Path(dbPath string) string { return dbPath + sidecarSuffix }

// Path returns the sidecar path this lock holds.
func (l *Lock) Path() string { return l.path }

// Acquire takes the advisory lock non-blocking (LOCK_EX|LOCK_NB) and writes
// the diagnostic body. On contention it returns a *HeldError wrapping ErrHeld,
// with the current holder's identity when the lock body is readable.
func Acquire(dbPath string, info Info) (*Lock, error) {
	path := Path(dbPath)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, filePerm) // #nosec G304 -- caller-supplied DB path
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	fd := int(f.Fd()) // #nosec G115 -- fd is a small positive int
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			holder, _ := ReadHolder(dbPath) // best effort: attribution only
			return nil, &HeldError{Path: path, Holder: holder}
		}
		return nil, fmt.Errorf("flock %s: %w", path, err)
	}

	if info.PID == 0 {
		info.PID = os.Getpid()
	}
	if info.StartedAt.IsZero() {
		info.StartedAt = time.Now()
	}
	if err := writeBody(f, info); err != nil {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("write lock body: %w", err)
	}
	return &Lock{f: f, path: path}, nil
}

// AcquireRetry retries the non-blocking acquire until it succeeds, the budget
// elapses, or ctx is cancelled. After the budget it returns the last observed
// contention error (a *HeldError naming the holder when readable).
func AcquireRetry(ctx context.Context, dbPath string, info Info, budget time.Duration) (*Lock, error) {
	deadline := time.Now().Add(budget)
	for {
		l, err := Acquire(dbPath, info)
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, ErrHeld) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("acquire database lock: %w", ctx.Err())
		case <-time.After(retryInterval):
		}
	}
}

// Release unlocks and closes the sidecar fd. The file itself is never
// unlinked: flock state is per-fd, and removing the path would race waiters
// holding their own fd to it. The stale body left behind is harmless — the
// next holder truncate-writes it, and contenders only read it after a failed
// flock.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	fd := int(l.f.Fd()) // #nosec G115 -- fd is a small positive int
	unlockErr := syscall.Flock(fd, syscall.LOCK_UN)
	closeErr := l.f.Close()
	l.f = nil
	if unlockErr != nil {
		return fmt.Errorf("funlock %s: %w", l.path, unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", l.path, closeErr)
	}
	return nil
}

// ReadHolder reads the recorded holder identity from dbPath's sidecar for
// error attribution. It does not consult the flock: a readable body proves
// nothing about whether the lock is currently held.
func ReadHolder(dbPath string) (*Info, error) {
	data, err := os.ReadFile(Path(dbPath)) // #nosec G304 -- caller-supplied DB path
	if err != nil {
		return nil, fmt.Errorf("read lock body: %w", err)
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parse lock body: %w", err)
	}
	return &info, nil
}

// writeBody truncate-writes the JSON body at the start of the lock file.
func writeBody(f *os.File, info Info) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		return err
	}
	return f.Sync()
}
