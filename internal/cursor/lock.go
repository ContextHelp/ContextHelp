package cursor

import (
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/filelock"
)

// acquireLock takes an exclusive advisory lock on path, blocking until
// acquired. Returns a release func that unlocks + closes the lock file.
// Caller must defer the returned func.
func acquireLock(path string) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, filePerm) // #nosec G304 -- caller-supplied
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := filelock.Lock(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
		// Lock file persists; lock state is per open file, removing here
		// would race with other waiters.
	}, nil
}
