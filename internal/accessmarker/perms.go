package accessmarker

import (
	"errors"
	"fmt"
	"io/fs"
)

// ErrModesUnsupported is returned where file modes do not express who
// may read a file (Windows, whose ACLs this package does not inspect).
var ErrModesUnsupported = errors.New("file-mode checks are not supported on this platform")

// Finding is one instance file whose mode lets other users in.
type Finding struct {
	// Path is the file or directory.
	Path string
	// Mode is its permission bits.
	Mode fs.FileMode
}

// String renders the finding for doctor and serve output.
func (f Finding) String() string {
	return fmt.Sprintf("%s is mode %04o: readable or writable by other users (want 0600 files, 0700 directories)", f.Path, f.Mode.Perm())
}

// instanceFiles lists the files beside dbPath that carry its data or
// state: the database, its WAL and shared-memory files, and the lock and
// access-marker sidecars.
func instanceFiles(dbPath string) []string {
	return []string{dbPath, dbPath + "-wal", dbPath + "-shm", dbPath + ".lock", Path(dbPath)}
}
