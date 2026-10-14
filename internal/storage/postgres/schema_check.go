package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// LatestSchemaVersion is the schema version a full Migrate ends at.
func LatestSchemaVersion() int {
	return pgMigrations[len(pgMigrations)-1].Version
}

// CheckSchema reads the schema version of the database at connStr
// without migrating it: one read-only transaction, no extension, no
// ledger table, no advisory lock. A database without a ledger (version
// 0) is reported as new only when it holds no tables, the same rule the
// pre-migration warning uses.
func CheckSchema(ctx context.Context, connStr string) (storage.SchemaStatus, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return storage.SchemaStatus{}, fmt.Errorf("check schema: open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return storage.SchemaStatus{}, fmt.Errorf("check schema: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var ledger bool
	if err := tx.QueryRowContext(ctx,
		`SELECT to_regclass('schema_version') IS NOT NULL`).Scan(&ledger); err != nil {
		return storage.SchemaStatus{}, fmt.Errorf("check schema: %w", err)
	}
	current := 0
	if ledger {
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
			return storage.SchemaStatus{}, fmt.Errorf("check schema: read schema version: %w", err)
		}
	}
	latest := LatestSchemaVersion()
	st := storage.SchemaStatus{Current: current, Latest: latest, Target: pgPendingTarget(current, latest)}
	if current == 0 {
		empty, err := pgHasNoOwnTables(ctx, tx)
		if err != nil {
			return storage.SchemaStatus{}, fmt.Errorf("check schema: %w", err)
		}
		st.New = empty
	}
	return st, nil
}
