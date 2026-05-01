package cursor

import (
	"fmt"
	"os"
	"syscall"
)

// acquireLock takes an exclusive advisory POSIX flock on path, blocking
// until acquired. Returns a release func that unlocks + closes + removes
// the lock file. Caller must defer the returned func.
func acquireLock(path string) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, filePerm) // #nosec G304 -- caller-supplied
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	fd := int(f.Fd()) // #nosec G115 -- fd is small positive int
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("flock: %w", err)
	}
	return func() {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
		// Lock file persists; flock state is per-fd, removing here would
		// race with other waiters.
	}, nil
}
