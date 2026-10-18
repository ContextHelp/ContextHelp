package pidfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/filelock"
)

// lockName is the run dir's lock file. Write holds it while it replaces
// a pidfile and Scan while it reads and prunes, so a scan never removes
// a pidfile a new instance renamed in after the scan judged the old one
// stale, and a rename never meets a pidfile a scan holds open (Windows
// refuses to rename over an open file). The name is not a .pid entry.
const lockName = ".pidfile.lock"

// lockWait bounds the wait for the lock. A holder takes it for a
// rename or a scan of a few small files; one stuck past this (a stopped
// process) is gone around: a write goes ahead unlocked, a scan reads
// without pruning anything.
const lockWait = 2 * time.Second

// lockDir takes dir's lock, waiting up to lockWait, and reports whether
// it holds it. release is never nil; the OS releases the lock when the
// process exits, so a crashed holder blocks no one.
func lockDir(dir string) (release func(), locked bool) {
	f, err := openLock(filepath.Join(dir, lockName))
	if err != nil {
		return func() {}, false
	}
	deadline := time.Now().Add(lockWait)
	for {
		err := filelock.TryLock(f)
		if err == nil {
			return func() {
				_ = filelock.Unlock(f)
				_ = f.Close()
			}, true
		}
		if !errors.Is(err, filelock.ErrLocked) || time.Now().After(deadline) {
			_ = f.Close()
			return func() {}, false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// openLock opens the lock file at path, creating it group-writable (the
// umask has the last word). In a shared run dir it may be another
// user's and not writable here (sudo created it): a read-only open
// takes the same lock (flock and LockFileEx need no write access), so
// every user of the dir keeps locking.
func openLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o660) // #nosec G302 G304 -- the run dir's lock file, empty, shared on purpose
	if errors.Is(err, fs.ErrPermission) {
		f, err = os.Open(path) // #nosec G304 -- the run dir's lock file
	}
	return f, err
}
