package sqlite

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Option configures a Driver at New.
type Option func(*Driver)

// WithMigrateSnapshot turns the pre-migration snapshot on or off
// (storage.migrate_snapshot). It is on unless this option turns it off.
func WithMigrateSnapshot(enabled bool) Option {
	return func(d *Driver) { d.skipMigrateSnapshot = !enabled }
}

const (
	// snapshotDirName is the directory beside the database file that
	// holds pre-migration snapshots.
	snapshotDirName = "backups"
	// snapshotKeep is how many pre-migration snapshots a backups
	// directory keeps; older ones are deleted after a new one is written.
	snapshotKeep = 3
	// snapshotTimeLayout is the UTC timestamp in a snapshot's name.
	snapshotTimeLayout = "20060102T150405Z"
	// goosWindows is runtime.GOOS on Windows, where directories cannot
	// be fsynced and permission bits do not restrict access.
	goosWindows = "windows"
)

// snapshotNameRE matches the files writeMigrateSnapshot produces:
// pre-migrate-v<from>-v<to>-<UTC timestamp>[-<n>].sqlite. Retention
// only ever touches names that match it.
var snapshotNameRE = regexp.MustCompile(`^pre-migrate-v(\d+)-v(\d+)-(\d{8}T\d{6}Z)(?:-(\d+))?\.sqlite$`)

// SnapshotDir returns the directory pre-migration snapshots of the
// database at dbPath are written to: backups/ beside the database file.
// With the default storage.path that is <data dir>/backups.
func SnapshotDir(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), snapshotDirName)
}

// pendingTarget returns the highest migration version above current and
// at most target, or 0 when no migration is pending.
func pendingTarget(current, target int) int {
	to := 0
	for _, m := range migrations {
		if m.Version > current && m.Version <= target && m.Version > to {
			to = m.Version
		}
	}
	return to
}

// IsMemoryPath reports whether path names an in-memory database, which
// has nothing on disk to protect and no directory to write beside.
func IsMemoryPath(path string) bool {
	return path == "" || path == ":memory:" || strings.Contains(path, "mode=memory")
}

// snapshotBeforeMigrate writes a consistent copy of the database before
// any migration from version from runs, when one is pending up to
// target. A brand-new database (version 0, no tables of its own) has
// nothing to lose and gets none. Any failure is returned: the caller
// must not migrate without the snapshot.
func (d *Driver) snapshotBeforeMigrate(ctx context.Context, from, target int) error {
	to := pendingTarget(from, target)
	if to == 0 || d.skipMigrateSnapshot || IsMemoryPath(d.path) {
		return nil
	}
	if from == 0 {
		empty, err := hasNoOwnTables(ctx, d.db)
		if err != nil {
			return fmt.Errorf("snapshot before migration: %w", err)
		}
		if empty {
			return nil
		}
	}

	dir := SnapshotDir(d.path)
	path, err := writeMigrateSnapshot(ctx, d, dir, from, to, time.Now())
	if err != nil {
		return fmt.Errorf("snapshot before migrating schema v%d to v%d into %s (nothing migrated; free disk space or fix the directory's permissions, or set storage.migrate_snapshot=false to migrate without a snapshot): %w",
			from, to, dir, err)
	}
	slog.Info("database snapshot written before schema migration",
		"from", from, "to", to, "snapshot", path)

	// Retention runs only after the new snapshot is safely on disk. A
	// failure to delete an old snapshot costs disk space, not data, so
	// it does not block the migration.
	if err := pruneMigrateSnapshots(dir, snapshotKeep); err != nil {
		slog.Warn("pre-migration snapshot retention failed", "dir", dir, "error", err)
	}
	return nil
}

// writeMigrateSnapshot copies the database into dir with SQLite's online
// backup (VACUUM INTO: one read transaction, so the copy is consistent
// even with WAL content and concurrent writers) and fsyncs it. The copy
// is written under a temporary name and renamed into place once synced,
// so a crash never leaves a partial file under a snapshot name.
func writeMigrateSnapshot(ctx context.Context, d *Driver, dir string, from, to int, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create backups dir: %w", err)
	}

	// VACUUM INTO accepts an existing empty file, so CreateTemp both
	// reserves a unique name and gives the copy owner-only permissions.
	tmpf, err := os.CreateTemp(dir, ".pre-migrate-*.partial")
	if err != nil {
		return "", fmt.Errorf("create snapshot file: %w", err)
	}
	tmp := tmpf.Name()
	if err := tmpf.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("create snapshot file: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmp)
		}
	}()

	if _, err := d.db.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		return "", fmt.Errorf("vacuum into: %w", err)
	}
	if err := syncPath(tmp); err != nil {
		return "", fmt.Errorf("fsync snapshot: %w", err)
	}

	final, err := freeSnapshotName(dir, from, to, now)
	if err != nil {
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		return "", fmt.Errorf("rename snapshot: %w", err)
	}
	committed = true
	if runtime.GOOS != goosWindows {
		if err := syncPath(dir); err != nil {
			return final, fmt.Errorf("fsync backups dir: %w", err)
		}
	}
	return final, nil
}

// freeSnapshotName returns the first snapshot path in dir for this
// migration and time that no file holds yet.
func freeSnapshotName(dir string, from, to int, now time.Time) (string, error) {
	base := fmt.Sprintf("pre-migrate-v%d-v%d-%s", from, to, now.UTC().Format(snapshotTimeLayout))
	for i := 0; i < 1000; i++ {
		name := base + ".sqlite"
		if i > 0 {
			name = base + "-" + strconv.Itoa(i) + ".sqlite"
		}
		p := filepath.Join(dir, name)
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			return p, nil
		} else if err != nil {
			return "", fmt.Errorf("stat %s: %w", p, err)
		}
	}
	return "", fmt.Errorf("no free snapshot name for %s", base)
}

// syncPath fsyncs the file or directory at p.
func syncPath(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	serr := f.Sync()
	cerr := f.Close()
	if serr != nil {
		return serr
	}
	return cerr
}

// pruneMigrateSnapshots deletes all but the newest keep pre-migration
// snapshots in dir. Files whose names are not snapshot names are left
// alone.
func pruneMigrateSnapshots(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type snap struct {
		name string
		ts   string
		seq  int
	}
	snaps := make([]snap, 0, len(entries))
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		m := snapshotNameRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		seq := 0
		if m[4] != "" {
			seq, _ = strconv.Atoi(m[4])
		}
		snaps = append(snaps, snap{name: e.Name(), ts: m[3], seq: seq})
	}
	// Newest first: the fixed-width UTC timestamp sorts as text; the
	// collision suffix orders snapshots taken in the same second.
	sort.Slice(snaps, func(i, j int) bool {
		if snaps[i].ts != snaps[j].ts {
			return snaps[i].ts > snaps[j].ts
		}
		return snaps[i].seq > snaps[j].seq
	})
	var errs []error
	for i := keep; i < len(snaps); i++ {
		if err := os.Remove(filepath.Join(dir, snaps[i].name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
