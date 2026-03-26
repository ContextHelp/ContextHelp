package storageutil_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestSQLiteSnapshot(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := filepath.Join(srcDir, "source.db")
	dst := filepath.Join(dstDir, "snapshot.db")

	// Create a minimal SQLite DB.
	db, err := sql.Open("sqlite", src)
	if err != nil {
		t.Fatalf("create source db: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		db.Close()
		t.Fatalf("create table: %v", err)
	}
	db.Close()

	if err := storageutil.SQLiteSnapshot(src, dst); err != nil {
		t.Fatalf("SQLiteSnapshot: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("snapshot file not created: %v", err)
	}
}
