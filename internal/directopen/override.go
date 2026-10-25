package directopen

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// EventMaintenanceOverride is the audit event an override writes.
const EventMaintenanceOverride = "maintenance.override"

// Override opens a protected or public instance for an operator
// maintenance action (housekeeping, backup) without lowering its access
// class. It is the one way past the access gate, and it is narrow:
//
//   - the process must run as the user that owns the database file, the
//     daemon's user, or for a database not created yet its directory
//     (not checked on Windows, where ownership is an ACL);
//   - it takes the database lock, so the daemon must be stopped;
//   - it appends a maintenance.override entry to the instance's audit
//     log before returning, and fails if that write fails.
//
// action names the operation in the audit entry. A private instance
// needs no override; Override opens it as Write does, without an entry.
func Override(ctx context.Context, t Target, action string) (*Handle, error) {
	if !t.guarded() {
		return Write(ctx, t)
	}
	m, err := Class(t)
	if err != nil {
		return nil, fmt.Errorf("maintenance override: %w", err)
	}
	if m.Private() {
		return Write(ctx, t)
	}
	// A database not created yet (the bootstrap of a fresh instance) is
	// owned by whoever owns the directory it will be created in.
	ownerPath := t.Path
	_, statErr := os.Stat(t.Path)
	fresh := errors.Is(statErr, fs.ErrNotExist)
	if fresh {
		ownerPath = filepath.Dir(t.Path)
	}
	owned, uid, err := accessmarker.OwnedByCurrentUser(ownerPath)
	if err != nil {
		return nil, fmt.Errorf("maintenance override: %w", err)
	}
	if !owned {
		return nil, fmt.Errorf("maintenance override: run it as the user that owns %s (uid %d), the daemon's user", ownerPath, uid)
	}
	lock, err := AcquireLock(ctx, t.Path, t.Label)
	if err != nil {
		return nil, err
	}
	// Audit first, on the store as it is: the entry exists even if the
	// migration that follows fails. A store with no schema yet holds no
	// data and no audit log: create it, then audit.
	driver, err := storageutil.Open(t.typ(), t.Path, t.Blob, storageutil.WithMigrateSnapshot(t.MigrateSnapshot))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("init storage: %w", err), lock.Release())
	}
	st, err := storageutil.CheckSchema(ctx, t.typ(), t.Path)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("maintenance override: %w", err), driver.Close(ctx), lock.Release())
	}
	migrated := false
	if st.New {
		if err := driver.Init(ctx); err != nil {
			return nil, errors.Join(fmt.Errorf("init storage: %w", err), driver.Close(ctx), lock.Release())
		}
		migrated = true
	}
	entry := &storage.AuditEntry{
		ID:        uuid.New().String(),
		EventType: EventMaintenanceOverride,
		Actor:     operator(),
		Payload: map[string]any{
			"action":   action,
			"access":   m.Access,
			"instance": m.Instance,
			"db_path":  t.Path,
			"pid":      os.Getpid(),
		},
		CreatedAt: time.Now().UTC(),
	}
	if err := driver.AuditLog().Append(ctx, entry); err != nil {
		return nil, errors.Join(fmt.Errorf("maintenance override: audit log: %w", err), driver.Close(ctx), lock.Release())
	}
	if !migrated {
		if err := driver.Init(ctx); err != nil {
			return nil, errors.Join(fmt.Errorf("init storage: %w", err), driver.Close(ctx), lock.Release())
		}
	}
	h := &Handle{Driver: driver, lock: lock}
	return h, nil
}

// operator names the account running the override for the audit log:
// "user:<name> (<id>)" from os/user, the id a uid on Unix and a SID on
// Windows; the bare process uid when the account cannot be looked up.
func operator() string {
	u, err := user.Current()
	if err != nil {
		return fmt.Sprintf("uid:%d", os.Getuid())
	}
	return fmt.Sprintf("user:%s (%s)", u.Username, u.Uid)
}
