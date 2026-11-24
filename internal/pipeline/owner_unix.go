//go:build unix

package pipeline

import (
	"io/fs"
	"os"
	"syscall"
)

// ownedByCurrentUser reports whether info's owner is the current user.
func ownedByCurrentUser(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}

// privateToUser reports whether info is owned by the current user, grants
// group and other nothing, and gives the owner read, write and search: a
// root the owner cannot write to (0500) is no use for scratch dirs.
func privateToUser(info fs.FileInfo) bool {
	perm := info.Mode().Perm()
	return ownedByCurrentUser(info) && perm&0o077 == 0 && perm&0o700 == 0o700
}
