package storageutil

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteSnapshot creates a consistent copy of the SQLite database at srcPath
// by executing VACUUM INTO dstPath. Safe to call while the DB is open and
// being written to. Requires SQLite >= 3.27.
func SQLiteSnapshot(srcPath, dstPath string) error {
	db, err := sql.Open("sqlite3", srcPath)
	if err != nil {
		return fmt.Errorf("snapshot: open: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec("VACUUM INTO ?", dstPath); err != nil {
		return fmt.Errorf("snapshot: vacuum into: %w", err)
	}
	return nil
}
