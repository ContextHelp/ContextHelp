package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// LatestSchemaVersion is the schema version a full Migrate ends at.
func LatestSchemaVersion() int {
	return migrations[len(migrations)-1].Version
}

// CheckSchema reads the schema version of the database at path without
// migrating it: no snapshot, no ledger table, no journal-mode switch.
// The file is opened read-only (mode=ro, query_only) and closed before
// return; a WAL-mode database gets its -wal/-shm files if they are
// missing, since SQLite cannot read it without them, but the database
// file itself is not written. A missing file is reported as a new
// database and is not created.
func CheckSchema(ctx context.Context, path string) (storage.SchemaStatus, error) {
	latest := LatestSchemaVersion()
	newDB := storage.SchemaStatus{Latest: latest, Target: pendingTarget(0, latest), New: true}
	if IsMemoryPath(path) {
		return newDB, nil
	}
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return newDB, nil
	}
	if err != nil {
		return storage.SchemaStatus{}, fmt.Errorf("check schema: %w", err)
	}
	if fi.Size() == 0 {
		return newDB, nil
	}

	db, err := sql.Open("sqlite3", "file:"+path+"?mode=ro&_query_only=true&_busy_timeout=5000")
	if err != nil {
		return storage.SchemaStatus{}, fmt.Errorf("check schema: open %s read-only: %w", path, err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	var ledger int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
		 WHERE type = 'table' AND name = 'schema_version'`).Scan(&ledger); err != nil {
		return storage.SchemaStatus{}, fmt.Errorf("check schema: read %s: %w", path, err)
	}
	current := 0
	if ledger > 0 {
		if err := db.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
			return storage.SchemaStatus{}, fmt.Errorf("check schema: read schema version: %w", err)
		}
	}
	st := storage.SchemaStatus{Current: current, Latest: latest, Target: pendingTarget(current, latest)}
	if current == 0 {
		// Same rule snapshotBeforeMigrate applies: version 0 with no
		// tables of its own has nothing to lose.
		empty, err := hasNoOwnTables(ctx, db)
		if err != nil {
			return storage.SchemaStatus{}, fmt.Errorf("check schema: %w", err)
		}
		st.New = empty
	}
	return st, nil
}

// hasNoOwnTables reports whether db holds no tables besides the
// migration ledger and SQLite's own.
func hasNoOwnTables(ctx context.Context, db *sql.DB) (bool, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
		 WHERE type = 'table' AND name <> 'schema_version' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'`,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("inspect schema: %w", err)
	}
	return n == 0, nil
}
