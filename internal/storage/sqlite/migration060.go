package sqlite

import (
	"context"
	"fmt"
)

// ownerRolesJSON is the owner principal's roles from migration 060 on:
// admin plus every approval role (ADR-083 §1). internal/auth.OwnerRoles
// is the source; its tests pin the two together.
const ownerRolesJSON = `["admin","steward-requester","steward-approver","share-approver"]`

// migrate060OwnerApprovalRoles gives the owner principal the approval
// roles beside admin (ADR-083 §1): the admin bundle no longer holds the
// steward and approver scopes, and the owner is the one principal that
// may assign them, which the ceiling rule allows only to a holder. The
// owner row is protected from every other write, so its roles are the
// seed's; setting them again is a no-op.
func migrate060OwnerApprovalRoles(ctx context.Context, d *Driver) error {
	if _, err := d.db.ExecContext(ctx, `UPDATE principals SET roles = ? WHERE kind = 'owner'`, ownerRolesJSON); err != nil {
		return fmt.Errorf("owner approval roles: %w", err)
	}
	return nil
}
