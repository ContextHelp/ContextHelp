// Package filelock takes advisory whole-file locks on an open *os.File,
// portably: flock(2) on Unix, LockFileEx on Windows.
//
// A lock belongs to the open file (not the path, not the process): two
// opens of the same path contend, even within one process. The OS drops
// the lock when the file is closed or the process exits.
//
// Holders may keep content in the locked file (a JSON body naming the
// holder) that contenders read through their own handle; both platforms
// keep that content readable while the lock is held (see the Windows
// implementation for how).
package filelock

import "errors"

// ErrLocked reports that another open file holds a conflicting lock. The
// non-blocking calls return an error wrapping it on contention.
var ErrLocked = errors.New("file is locked by another holder")
