package rederive

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// sqliteDB is the *sql.DB under a SQLite test driver.
func sqliteDB(t *testing.T, drv storage.StorageDriver) *sql.DB {
	t.Helper()
	d, ok := drv.(interface{ DB() *sql.DB })
	if !ok {
		t.Fatalf("%T exposes no DB()", drv)
	}
	return d.DB()
}

// skipRows reads the skip records for target straight from the table: the
// job never reads them back, so the store has no method for it.
func skipRows(t *testing.T, drv storage.StorageDriver, target string) []storage.RederiveSkip {
	t.Helper()
	rows, err := sqliteDB(t, drv).QueryContext(context.Background(),
		`SELECT object_id, target_sig, reason, detail FROM rederive_skips WHERE target_sig = ? ORDER BY object_id`, target)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []storage.RederiveSkip
	for rows.Next() {
		var k storage.RederiveSkip
		if err := rows.Scan(&k.ObjectID, &k.TargetSig, &k.Reason, &k.Detail); err != nil {
			t.Fatal(err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
