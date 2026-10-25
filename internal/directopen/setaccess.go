package directopen

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// EventAccessSet is the audit event SetAccess writes.
const EventAccessSet = "access.set"

// SetAccess records access as the class of t's database (an operator
// action: `dpkms instance set-access`). It holds to the override's
// rules: the database must exist and the process must run as its owner
// (not checked on Windows); it takes the database lock, so the daemon
// must be stopped; and it appends an access.set audit entry before the
// marker changes, changing nothing when that fails. The marker keeps its
// instance name, or takes name when it has none. It returns the previous
// marker; an unreadable one, which this repairs, comes back empty.
func SetAccess(ctx context.Context, t Target, access, name string) (accessmarker.Marker, error) {
	if !t.guarded() {
		return accessmarker.Marker{}, errors.New("set-access needs a SQLite database file: postgres and in-memory SQLite have no access marker")
	}
	if _, err := os.Stat(t.Path); err != nil {
		return accessmarker.Marker{}, fmt.Errorf("set-access: database %s: %w", t.Path, err)
	}
	owned, uid, err := accessmarker.OwnedByCurrentUser(t.Path)
	if err != nil {
		return accessmarker.Marker{}, fmt.Errorf("set-access: %w", err)
	}
	if !owned {
		return accessmarker.Marker{}, fmt.Errorf("set-access: run it as the user that owns %s (uid %d), the daemon's user", t.Path, uid)
	}
	lock, err := AcquireLock(ctx, t.Path, t.Label)
	if err != nil {
		return accessmarker.Marker{}, err
	}
	defer func() { _ = lock.Release() }()

	prev, _, err := accessmarker.Read(t.Path)
	if err != nil {
		prev = accessmarker.Marker{}
	}
	if prev.Instance != "" {
		name = prev.Instance
	}
	if err := auditAccessSet(ctx, t, prev.Access, access, name); err != nil {
		return accessmarker.Marker{}, err
	}
	if err := accessmarker.Write(t.Path, accessmarker.Marker{Access: access, Instance: name}); err != nil {
		return accessmarker.Marker{}, err
	}
	return prev, nil
}

// auditAccessSet appends the access.set entry, on the store as it is
// (no migration).
func auditAccessSet(ctx context.Context, t Target, from, to, name string) error {
	driver, err := storageutil.Open(t.typ(), t.Path, t.Blob, storageutil.WithMigrateSnapshot(false))
	if err != nil {
		return fmt.Errorf("set-access: open audit log: %w", err)
	}
	entry := &storage.AuditEntry{
		ID:        uuid.New().String(),
		EventType: EventAccessSet,
		Actor:     operator(),
		Payload: map[string]any{
			"from":     from,
			"to":       to,
			"instance": name,
			"db_path":  t.Path,
			"pid":      os.Getpid(),
		},
		CreatedAt: time.Now().UTC(),
	}
	appendErr := driver.AuditLog().Append(ctx, entry)
	closeErr := driver.Close(ctx)
	if appendErr != nil {
		return fmt.Errorf("set-access: audit log: %w", appendErr)
	}
	return closeErr
}
