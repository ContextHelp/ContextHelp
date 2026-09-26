package position

import (
	"fmt"
	"os"
	"syscall"
)

// lock takes an exclusive advisory flock on path, blocking until it is
// granted, and returns the release func. The kernel drops the lock if
// the process dies. The lock file is never removed: flock state is
// per-fd, and unlinking would race waiters holding their own fd (same
// rule as internal/cursor and internal/dblock).
func lock(path string) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, filePerm) // #nosec G304 -- derived from the configured state path
	if err != nil {
		return nil, fmt.Errorf("position: open lock %s: %w", path, err)
	}
	fd := int(f.Fd()) // #nosec G115 -- fd is a small positive int
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("position: flock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
