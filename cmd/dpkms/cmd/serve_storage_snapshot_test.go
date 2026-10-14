package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// rewindLatestMigration opens the database at dbPath through serve and
// drops the newest ledger row, so the next open has one migration
// pending. The newest migration is re-runnable, so replaying it is safe.
func rewindLatestMigration(t *testing.T, c *config.Config) {
	t.Helper()
	ctx := context.Background()
	driver, err := openServeStorage(ctx, c)
	if err != nil {
		t.Fatalf("open serve storage: %v", err)
	}
	db := driver.(*sqlite.Driver).DB()
	if _, err := db.Exec(`DELETE FROM schema_version WHERE version = (SELECT MAX(version) FROM schema_version)`); err != nil {
		t.Fatalf("rewind ledger: %v", err)
	}
	if err := driver.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

// storage.migrate_snapshot reaches the driver serve opens: on by
// default, off when the config turns it off.
func TestServeStorage_MigrateSnapshotFollowsConfig(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra string
		want  int
	}{
		{name: "default on", want: 1},
		{name: "config off", extra: "  migrate_snapshot: false\n", want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dbPath := filepath.Join(t.TempDir(), "dpkms.db")
			c, _ := loadSandboxedServeConfig(t, "storage:\n  type: sqlite\n  path: "+dbPath+"\n"+tc.extra)
			rewindLatestMigration(t, c)

			driver, err := openServeStorage(ctx, c)
			if err != nil {
				t.Fatalf("reopen serve storage: %v", err)
			}
			t.Cleanup(func() { _ = driver.Close(ctx) })

			snaps, _ := filepath.Glob(filepath.Join(sqlite.SnapshotDir(dbPath), "pre-migrate-*.sqlite"))
			if len(snaps) != tc.want {
				t.Fatalf("snapshots = %v, want %d", snaps, tc.want)
			}
			if tc.want == 0 {
				if _, err := os.Stat(sqlite.SnapshotDir(dbPath)); !os.IsNotExist(err) {
					t.Fatalf("backups dir created with migrate_snapshot off (stat err %v)", err)
				}
			}
		})
	}
}
