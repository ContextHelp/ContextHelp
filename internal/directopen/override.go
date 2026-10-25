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
)

// EventMaintenanceOverride is the audit event an override writes.
const EventMaintenanceOverride = "maintenance.override"

// Override opens a protected or public instance for an operator
// maintenance action (housekeeping, backup) without lowering its access
// class. It is the one way past the access gate, and it is narrow:
//
//   - the process must run as the user that owns the database file, the
//     daemon's user (not checked on Windows, where ownership is an ACL);
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
	owned, uid, err := accessmarker.OwnedByCurrentUser(t.Path)
	if err != nil {
		return nil, fmt.Errorf("maintenance override: %w", err)
	}
	if !owned {
		return nil, fmt.Errorf("maintenance override: run it as the user that owns %s (uid %d), the daemon's user", t.Path, uid)
	}
	lock, err := AcquireLock(ctx, t.Path, t.Label)
	if err != nil {
		return nil, err
	}
	h, err := open(ctx, t, lock)
	if err != nil {
		return nil, err
	}
	entry := &storage.AuditEntry{
		ID:        uuid.New().String(),
		EventType: EventMaintenanceOverride,
		Actor:     fmt.Sprintf("uid:%d", os.Getuid()),
		Payload: map[string]any{
			"action":   action,
			"access":   m.Access,
			"instance": m.Instance,
			"db_path":  t.Path,
			"pid":      os.Getpid(),
		},
		CreatedAt: time.Now().UTC(),
	}
	if err := h.Driver.AuditLog().Append(ctx, entry); err != nil {
		return nil, errors.Join(fmt.Errorf("maintenance override: audit log: %w", err), h.Close(ctx))
	}
	return h, nil
}
