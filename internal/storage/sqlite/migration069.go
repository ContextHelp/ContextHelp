package sqlite

import (
	"context"
	"fmt"
)

// objectChangeSeqUpdateTriggerSkipSig is objects_change_seq_update (see
// objectChangeSeqTriggers) except that an UPDATE which changes the
// derivation signature does not bump change_seq: the signature never goes
// on the wire, so stamping it must not re-push the object to federation
// targets. Any other column still bumps, so the signature must only ever be
// written alone: an UPDATE changing it together with another column would
// not be pushed either.
const objectChangeSeqUpdateTriggerSkipSig = `CREATE TRIGGER objects_change_seq_update AFTER UPDATE ON objects
	WHEN NEW.change_seq = OLD.change_seq AND NEW.derivation_sig IS OLD.derivation_sig
	BEGIN
		UPDATE object_change_counter SET seq = seq + 1 WHERE id = 1;
		UPDATE objects SET change_seq = (SELECT seq FROM object_change_counter WHERE id = 1)
		 WHERE id = NEW.id;
	END`

// migrate069DerivationSig adds objects.derivation_sig, the signature of the
// pipeline and step versions that derived the row, and its index. Existing
// rows keep the empty value; the daemon stamps a baseline for them.
// Idempotent.
func migrate069DerivationSig(ctx context.Context, d *Driver) error {
	if err := addTableColumnIfMissing(ctx, d, "objects", "derivation_sig", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add objects.derivation_sig: %w", err)
	}
	if _, err := d.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_objects_derivation_sig ON objects(derivation_sig)`); err != nil {
		return fmt.Errorf("index objects.derivation_sig: %w", err)
	}
	if _, err := d.db.ExecContext(ctx, `DROP TRIGGER IF EXISTS objects_change_seq_update`); err != nil {
		return fmt.Errorf("drop objects_change_seq_update: %w", err)
	}
	if _, err := d.db.ExecContext(ctx, objectChangeSeqUpdateTriggerSkipSig); err != nil {
		return fmt.Errorf("recreate objects_change_seq_update: %w", err)
	}
	return nil
}
