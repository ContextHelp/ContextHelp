package position

import (
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/filelock"
)

// lock takes an exclusive advisory lock on path, blocking until it is
// granted, and returns the release func. The OS drops the lock if the
// process dies. The lock file is never removed: lock state is per open
// file, and unlinking would race waiters holding their own handle (same
// rule as internal/cursor and internal/dblock).
func lock(path string) (release func(), err error) {
	// #nosec G703 -- path is the state file's fixed ".lock" sibling (see
	// Store.lockPath), derived from the operator-configured state file.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, filePerm)
	if err != nil {
		return nil, fmt.Errorf("position: open lock %s: %w", path, err)
	}
	if err := filelock.Lock(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("position: %w", err)
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
	}, nil
}
