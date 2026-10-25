// Package directopen opens the configured store for a process that is
// not the daemon: a CLI command, housekeeping, backup. It applies the
// two gates that keep such an open from undercutting the daemon.
//
//  1. Access class. An instance whose access marker (internal/accessmarker)
//     says protected or public is refused outright, reads included: its
//     daemon's API, with authentication and policy, is the only way in.
//  2. Single writer. A write open takes the database lock
//     (internal/dblock) non-blocking for the life of the handle. A lock
//     held by the daemon fails at once; one held by another command is
//     retried for a short budget. A read open takes no lock and never
//     waits: it opens the store read-only (mode=ro on SQLite,
//     default_transaction_read_only on Postgres), runs no migration and
//     refuses a store that is missing or at another schema version.
//
// Both gates guard a database file. Postgres and in-memory SQLite have
// none: they get no marker check and no lock, and keep the read/write
// split only.
package directopen

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// typeSQLite is the storage type with a database file to guard.
const typeSQLite = "sqlite"

// CLIRetry is how long a write open retries a lock another command
// holds. A daemon holding it is not retried: it holds it until it stops.
var CLIRetry = 2 * time.Second

// Target is the store to open.
type Target struct {
	// Type is the storage type; empty means sqlite.
	Type string
	// Path is the SQLite file or the Postgres connection string.
	Path string
	// Blob is storage.blob.
	Blob config.BlobConfig
	// MigrateSnapshot is storage.migrate_snapshot (write opens only).
	MigrateSnapshot bool
	// Label names the opener in the lock body ("cli", "housekeeping").
	Label string
	// Access is the configured class (server.access); empty is private.
	// The gate applies the stricter of it and the access marker.
	Access string
}

func (t Target) typ() string {
	if t.Type == "" {
		return typeSQLite
	}
	return t.Type
}

// guarded reports whether t has a database file the gates apply to.
func (t Target) guarded() bool {
	return t.typ() == typeSQLite && !sqlite.IsMemoryPath(t.Path)
}

// Handle is an open store. Close it to release the lock it holds.
type Handle struct {
	Driver storage.StorageDriver
	lock   *dblock.Lock
}

// Close closes the driver, then releases the lock: the lock is held
// until the last write is on disk.
func (h *Handle) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	var closeErr error
	if h.Driver != nil {
		closeErr = h.Driver.Close(ctx)
	}
	return errors.Join(closeErr, h.lock.Release())
}

// Class returns t's effective access: the marker when it records a
// class other than private, else the configured class (private when
// unset). A marker that cannot be read is an *accessmarker.RefusedError.
func Class(t Target) (accessmarker.Marker, error) {
	m, _, err := accessmarker.Read(t.Path)
	if err != nil {
		return accessmarker.Marker{}, &accessmarker.RefusedError{DBPath: t.Path, Cause: err}
	}
	if m.Private() && t.Access != "" && t.Access != config.AccessPrivate {
		m.Access = t.Access
	}
	return m, nil
}

// CheckAccess applies the access-class gate alone: nil for a private
// instance (by marker and by configuration) or a store with no file, an
// *accessmarker.RefusedError otherwise.
func CheckAccess(t Target) error {
	if !t.guarded() {
		return nil
	}
	m, err := Class(t)
	if err != nil {
		return err
	}
	if !m.Private() {
		return &accessmarker.RefusedError{DBPath: t.Path, Marker: m}
	}
	return nil
}

// Write opens t for a command that writes: access gate, database lock,
// open, migrate.
func Write(ctx context.Context, t Target) (*Handle, error) {
	if t.Path == "" {
		return nil, errors.New("storage path not configured")
	}
	if err := CheckAccess(t); err != nil {
		return nil, err
	}
	var lock *dblock.Lock
	if t.guarded() {
		l, err := AcquireLock(ctx, t.Path, t.Label)
		if err != nil {
			return nil, err
		}
		lock = l
	}
	return open(ctx, t, lock)
}

// open opens and migrates t, which lock (nil for no file) guards; lock
// is released when the open fails.
func open(ctx context.Context, t Target, lock *dblock.Lock) (*Handle, error) {
	driver, err := storageutil.Open(t.typ(), t.Path, t.Blob, storageutil.WithMigrateSnapshot(t.MigrateSnapshot))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("init storage: %w", err), lock.Release())
	}
	if err := driver.Init(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("init storage: %w", err), driver.Close(ctx), lock.Release())
	}
	return &Handle{Driver: driver, lock: lock}, nil
}

// Read opens t for a command that only reads: access gate, then a
// read-only open with no lock and no migration.
func Read(ctx context.Context, t Target) (*Handle, error) {
	if t.Path == "" {
		return nil, errors.New("storage path not configured")
	}
	if err := CheckAccess(t); err != nil {
		return nil, err
	}
	driver, err := storageutil.OpenReadOnly(ctx, t.typ(), t.Path, t.Blob)
	if err != nil {
		return nil, fmt.Errorf("open storage read-only: %w", err)
	}
	return &Handle{Driver: driver}, nil
}

// AcquireLock takes dbPath's database lock for a non-daemon writer
// labelled label. A daemon holder fails at once; another command's hold
// is retried for CLIRetry. Contention is a *ContentionError.
func AcquireLock(ctx context.Context, dbPath, label string) (*dblock.Lock, error) {
	info := dblock.Info{Role: dblock.RoleCLI, Instance: label}
	l, err := dblock.Acquire(dbPath, info)
	if err == nil {
		return l, nil
	}
	var held *dblock.HeldError
	if !errors.As(err, &held) {
		return nil, fmt.Errorf("lock database %s: %w", dbPath, err)
	}
	if held.Holder == nil || held.Holder.Role != dblock.RoleDaemon {
		l, err = dblock.AcquireRetry(ctx, dbPath, info, CLIRetry)
		if err == nil {
			return l, nil
		}
		if !errors.As(err, &held) {
			return nil, fmt.Errorf("lock database %s: %w", dbPath, err)
		}
	}
	return nil, &ContentionError{DBPath: dbPath, Held: held}
}

// ContentionError reports a write open refused because another process
// holds the database lock. It wraps dblock.ErrHeld: busy, retry later.
type ContentionError struct {
	DBPath string
	Held   *dblock.HeldError
}

// Error names the database and its holder.
func (e *ContentionError) Error() string {
	return fmt.Sprintf("database %s is in use: %v", e.DBPath, e.Held)
}

// Unwrap exposes the *dblock.HeldError (and so dblock.ErrHeld).
func (e *ContentionError) Unwrap() error { return e.Held }

// Fix states the resolution for the holder kind.
func (e *ContentionError) Fix() string {
	if h := e.Held.Holder; h != nil && h.Role == dblock.RoleDaemon {
		return "the daemon owns this database while it runs: stop it (`dpkms shutdown`) and retry, " +
			"or use a read command"
	}
	return "another command is writing this database: retry when it finishes"
}
