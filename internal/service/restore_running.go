package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
)

// refuseWhileLocked refuses a restore while a process holds dbPath's
// database lock: every dpkms serve holds it on its database for its
// whole lifetime, pidfile or not. It only probes, creating nothing, so
// a dry run stays write-free.
func refuseWhileLocked(dbPath string) error {
	if err := dblock.Check(dbPath); err != nil {
		return lockRefusal(dbPath, err)
	}
	return nil
}

// acquireRestoreLock takes dbPath's database lock for the writing pass, so
// a serve starting mid-restore cannot open the half-restored database.
// The database's directory is created first, as writing the database
// would. Release the returned lock when the restore returns.
func acquireRestoreLock(dbPath string) (*dblock.Lock, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, fmt.Errorf("restore: database dir: %w", err)
	}
	l, err := dblock.Acquire(dbPath, dblock.Info{Role: dblock.RoleCLI, Instance: "restore"})
	if err != nil {
		return nil, lockRefusal(dbPath, err)
	}
	return l, nil
}

func lockRefusal(dbPath string, err error) error {
	if errors.Is(err, dblock.ErrHeld) {
		return fmt.Errorf("restore: database %s is in use: %w; stop that process first (dpkms shutdown)", dbPath, err)
	}
	return fmt.Errorf("restore: database lock: %w", err)
}

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
