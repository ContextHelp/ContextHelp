//go:build !windows

package accessmarker

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// groupOtherBits are the permission bits that let a user other than the
// owner read, write or traverse a file.
const groupOtherBits = 0o077

// CheckPermissions reports each instance file of dbPath (database, WAL,
// shared memory, sidecars) and the blob directory blobDir (skipped when
// empty) whose mode grants group or other any access. Missing files are
// skipped. A blob directory closed to others keeps every blob in it
// closed, so its entries are not walked.
func CheckPermissions(dbPath, blobDir string) ([]Finding, error) {
	paths := instanceFiles(dbPath)
	if blobDir != "" {
		paths = append(paths, blobDir)
	}
	var out []Finding
	for _, p := range paths {
		fi, err := os.Stat(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("check permissions: %w", err)
		}
		if fi.Mode().Perm()&groupOtherBits != 0 {
			out = append(out, Finding{Path: p, Mode: fi.Mode().Perm()})
		}
	}
	return out, nil
}

// OwnedByCurrentUser reports whether the running process's uid owns
// path: a maintenance override runs as the daemon's user.
func OwnedByCurrentUser(path string) (bool, int, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return false, 0, fmt.Errorf("check owner: %w", err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false, 0, fmt.Errorf("check owner of %s: no uid available", path)
	}
	return int(st.Uid) == os.Getuid(), int(st.Uid), nil
}
