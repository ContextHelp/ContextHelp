package service

import (
	"fmt"
	"path/filepath"

	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
)

// refuseWhileServed refuses a restore while a live dpkms instance whose
// pidfile is in runDir serves dbPath: replacing the database under it
// would leave it writing to the unlinked old file, or replaying its
// WAL onto the restored one.
//
// An entry whose db_path is relative names a file relative to that
// instance's working directory, which is not known here; it counts as
// serving dbPath.
func refuseWhileServed(runDir, dbPath string) error {
	infos, err := pidfile.Scan(runDir)
	if err != nil {
		return fmt.Errorf("restore: read running instances: %w", err)
	}
	target := canonicalPath(dbPath)
	for _, info := range infos {
		if filepath.IsAbs(info.DBPath) && canonicalPath(info.DBPath) != target {
			continue
		}
		return fmt.Errorf("restore: dpkms instance %q (pid %d, port %d) is running on %s; stop it first (dpkms shutdown)",
			info.Name, info.PID, info.Port, info.DBPath)
	}
	return nil
}

// canonicalPath is p made absolute, with symlinks in its directory
// resolved when it exists, so two spellings of one file compare equal.
func canonicalPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	dir, file := filepath.Split(abs)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(real, file)
	}
	return abs
}
