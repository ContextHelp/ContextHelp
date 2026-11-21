package postgres

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// MigrateThroughForTest runs Migrate with the ledger stopped at version,
// building a database at a historic schema.
func (d *Driver) MigrateThroughForTest(ctx context.Context, version int) error {
	return d.migrateThrough(ctx, version)
}

// LatestSchemaVersionForTest is the version a full Migrate records last.
func LatestSchemaVersionForTest() int {
	return pgMigrations[len(pgMigrations)-1].Version
}

// ReplayEntitlementKindForTest re-runs migration 33 on a database that
// already has it, pinning that a replay is a no-op.
func (d *Driver) ReplayEntitlementKindForTest(ctx context.Context) error {
	return migrateEntitlementKind(ctx, d)
}

// ReplayContentHashPerProfileForTest re-runs migration 34 on a database
// that already has it, pinning that a replay is a no-op.
func (d *Driver) ReplayContentHashPerProfileForTest(ctx context.Context) error {
	return migrateContentHashPerProfile(ctx, d)
}

// AppendOutboxHeldForTest appends ev in a transaction and runs held after
// the row is written but before commit, so a test can observe how
// concurrent appends and readers behave while the append is in flight.
func (d *Driver) AppendOutboxHeldForTest(ctx context.Context, ev *storage.OutboxEvent, held func()) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := writeOutbox(ctx, tx, ev); err != nil {
		return err
	}
	held()
	return tx.Commit()
}

// ReplayAccessReceiptForTest re-runs the statements of migration 38 on a
// database that already has it, pinning that a replay is a no-op.
func (d *Driver) ReplayAccessReceiptForTest(ctx context.Context) error {
	for _, m := range pgMigrations {
		if m.Version != 38 {
			continue
		}
		for _, stmt := range m.Statements {
			if _, err := d.db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReplayStatementsForTest re-runs the statements of migration version
// on a database that already has it, pinning that a replay is a no-op.
func (d *Driver) ReplayStatementsForTest(ctx context.Context, version int) error {
	for _, m := range pgMigrations {
		if m.Version != version {
			continue
		}
		for _, stmt := range m.Statements {
			if _, err := d.db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
	}
	return nil
}
