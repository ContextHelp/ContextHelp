//go:build windows

package filelock

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// Windows byte-range locks are mandatory: a range locked through one handle
// cannot be read or written through another. Holders write their body at
// offset 0 and contenders read it, so the lock covers a single byte far
// past any content instead (SQLite's lock-byte trick). Locking past EOF is
// allowed and does not grow the file.
const (
	lockOffsetLow  = 0
	lockOffsetHigh = 0x7fffffff
)

// Lock takes an exclusive lock on f, blocking until it is granted.
func Lock(f *os.File) error { return lock(f, windows.LOCKFILE_EXCLUSIVE_LOCK) }

// TryLock takes an exclusive lock on f without blocking. It returns an
// error wrapping ErrLocked when another open file holds any lock on it.
func TryLock(f *os.File) error {
	return lock(f, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY)
}

// TryRLock takes a shared lock on f without blocking. It returns an error
// wrapping ErrLocked when another open file holds an exclusive lock.
func TryRLock(f *os.File) error { return lock(f, windows.LOCKFILE_FAIL_IMMEDIATELY) }

// Unlock releases the lock f holds.
func Unlock(f *os.File) error {
	ol := overlapped()
	if err := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol); err != nil {
		return fmt.Errorf("unlock %s: %w", f.Name(), err)
	}
	return nil
}

func lock(f *os.File, flags uint32) error {
	ol := overlapped()
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &ol)
	if err == nil {
		return nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return fmt.Errorf("%w: %s", ErrLocked, f.Name())
	}
	return fmt.Errorf("lock %s: %w", f.Name(), err)
}

func overlapped() windows.Overlapped {
	return windows.Overlapped{Offset: lockOffsetLow, OffsetHigh: lockOffsetHigh}
}
