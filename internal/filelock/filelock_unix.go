//go:build !windows

package filelock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// Lock takes an exclusive lock on f, blocking until it is granted.
func Lock(f *os.File) error { return flock(f, syscall.LOCK_EX) }

// TryLock takes an exclusive lock on f without blocking. It returns an
// error wrapping ErrLocked when another open file holds any lock on it.
func TryLock(f *os.File) error { return flock(f, syscall.LOCK_EX|syscall.LOCK_NB) }

// TryRLock takes a shared lock on f without blocking. It returns an error
// wrapping ErrLocked when another open file holds an exclusive lock.
func TryRLock(f *os.File) error { return flock(f, syscall.LOCK_SH|syscall.LOCK_NB) }

// Unlock releases the lock f holds.
func Unlock(f *os.File) error { return flock(f, syscall.LOCK_UN) }

func flock(f *os.File, how int) error {
	err := syscall.Flock(int(f.Fd()), how) // #nosec G115 -- fd is a small positive int
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return fmt.Errorf("%w: %s", ErrLocked, f.Name())
	}
	return fmt.Errorf("flock %s: %w", f.Name(), err)
}
