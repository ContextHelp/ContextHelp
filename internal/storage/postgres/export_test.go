package postgres

import "context"

// MigrateThroughForTest runs Migrate with the ledger stopped at version,
// building a database at a historic schema.
func (d *Driver) MigrateThroughForTest(ctx context.Context, version int) error {
	return d.migrateThrough(ctx, version)
}

// LatestSchemaVersionForTest is the version a full Migrate records last.
func LatestSchemaVersionForTest() int {
	return pgMigrations[len(pgMigrations)-1].Version
}
