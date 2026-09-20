package cmd

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
)

// hashDB returns the sha256 of the durable database after folding the
// write-ahead log into it.
//
// The checkpoint is essential. These stores run in WAL mode, so a
// write lands in the -wal sidecar and the .db file can stay
// byte-identical long after data has changed. Hashing the .db alone
// reports "unchanged" for a command that really did mutate, which
// makes the whole assertion vacuous. Checkpointing first moves every
// committed page into the main file, so the hash reflects actual
// committed state.
//
// The -wal/-shm files themselves are deliberately excluded: they churn
// whenever any reader opens the store and say nothing about data.
func hashDB(t *testing.T, db *testDB, path string) string {
	t.Helper()
	sqlDB, ok := db.Driver.(interface{ DB() *sql.DB })
	if !ok {
		t.Fatalf("driver does not expose *sql.DB")
	}
	if _, err := sqlDB.DB().ExecContext(
		context.Background(), "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatalf("wal_checkpoint: %v", err)
	}
	f, err := os.Open(path) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() {
		//nolint:errcheck // read-only handle in a test
		f.Close()
	}()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("hash %s: %v", path, err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// dbPathFor derives the sqlite path the test config points at.
func dbPathFor(db *testDB) string {
	return filepath.Join(filepath.Dir(db.ConfigPath), "test.db")
}

// seedObjects inserts objects dated before the prune cutoff so prune
// has real targets to report.
//
// It also churns the store: a block of bulky rows is written and then
// deleted, leaving pages on the freelist. Without that, VACUUM is a
// byte-level no-op on a freshly-built database and a hash comparison
// cannot tell a real preview from one that secretly runs the
// operation. The churn is what gives the non-mutation assertion teeth.
func seedObjects(t *testing.T, db *testDB, ids ...string) {
	t.Helper()
	ctx := context.Background()
	old := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	filler := strings.Repeat("x", 4096)
	churn := make([]string, 0, 64)
	for i := 0; i < 64; i++ {
		id := fmt.Sprintf("obj_churn_%03d", i)
		churn = append(churn, id)
		obj := &storage.KnowledgeObject{
			ID:         id,
			Type:       "note",
			Status:     "active",
			RawContent: filler,
			CreatedAt:  old,
			UpdatedAt:  old,
		}
		if err := db.Driver.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("seed churn %s: %v", id, err)
		}
	}
	for _, id := range churn {
		if err := db.Driver.Objects().Delete(ctx, id); err != nil {
			t.Fatalf("churn delete %s: %v", id, err)
		}
	}

	for _, id := range ids {
		obj := &storage.KnowledgeObject{
			ID:        id,
			Type:      "note",
			Status:    "active",
			CreatedAt: old,
			UpdatedAt: old,
		}
		if err := db.Driver.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	// Assert the fixture actually produced reclaimable pages, so the
	// non-mutation check below is capable of failing.
	var free int64
	sqlDB, ok := db.Driver.(interface{ DB() *sql.DB })
	if !ok {
		t.Fatalf("driver does not expose *sql.DB")
	}
	if err := sqlDB.DB().QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&free); err != nil {
		t.Fatalf("freelist_count: %v", err)
	}
	if free == 0 {
		t.Fatalf("fixture produced no free pages; a VACUUM would be a byte-level no-op " +
			"and the non-mutation assertion would be vacuous")
	}
}

// TestDryRunDeclaredOnEveryLeaf asserts every runnable dpkms leaf
// declares a kit/side-effect tier. Kit refuses --dry-run on any leaf
// that has not, which is exactly the Factor 6 failure this guards.
func TestDryRunDeclaredOnEveryLeaf(t *testing.T) {
	var missing []string
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			walk(sub)
		}
		if !c.Runnable() || len(c.Commands()) > 0 {
			return
		}
		switch c.Name() {
		case "completion", "help", "__complete", "__completeNoDesc":
			return
		}
		if p := c.Parent(); p != nil && p.Name() == "completion" {
			return
		}
		if _, ok := kitcli.GetSideEffect(c); !ok {
			missing = append(missing, c.CommandPath())
		}
	}
	walk(rootCmd)
	if len(missing) > 0 {
		t.Errorf("leaves missing kit/side-effect (--dry-run will be refused): %v", missing)
	}
}

// TestHousekeepingDryRunDoesNotMutate is the load-bearing assertion:
// each previewable maintenance command must leave the database file
// byte-identical.
func TestHousekeepingDryRunDoesNotMutate(t *testing.T) {
	cases := []struct {
		name string
		want string
		args []string
	}{
		{"vacuum", "Would run VACUUM", []string{"housekeeping", "vacuum", "--dry-run"}},
		{"compact", "Would compact database", []string{"housekeeping", "compact", "--dry-run"}},
		{"reindex", "Would rebuild search indexes", []string{"housekeeping", "reindex", "--dry-run"}},
		{"prune", "Would delete", []string{"housekeeping", "prune", "--before", "2024-01-01", "--dry-run"}},
		// housekeeping run narrates to STDERR by design: a full run is
		// chatty and that chatter must not interleave with the structured
		// document a --format json caller parses. The harness captures
		// stdout, so assert only non-mutation here; the stderr contract is
		// covered by the format tests.
		{"run", "", []string{"housekeeping", "run", "--dry-run"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			seedObjects(t, db, "obj_dryrun_a", "obj_dryrun_b")
			path := dbPathFor(db)

			before := hashDB(t, db, path)
			out, err := db.run(tc.args...)
			if err != nil {
				t.Fatalf("%v should succeed: %v", tc.args, err)
			}
			after := hashDB(t, db, path)

			if before != after {
				t.Errorf("dry-run mutated the database\n before %s\n after  %s", before, after)
			}
			if tc.want == "" {
				return
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("preview should report what would happen (want %q), got:\n%s", tc.want, out)
			}
			if !strings.Contains(out, "dry-run") {
				t.Errorf("preview should announce dry-run, got:\n%s", out)
			}
		})
	}
}

// TestPruneDryRunNamesItsTargets asserts the prune preview reports the
// specific object IDs it would delete, not just a count.
func TestPruneDryRunNamesItsTargets(t *testing.T) {
	db := setupTestDB(t)
	seedObjects(t, db, "obj_target_one", "obj_target_two")

	out, err := db.run("housekeeping", "prune", "--before", "2024-01-01", "--dry-run")
	if err != nil {
		t.Fatalf("prune --dry-run should succeed: %v", err)
	}
	for _, id := range []string{"obj_target_one", "obj_target_two"} {
		if !strings.Contains(out, id) {
			t.Errorf("preview should name target %s, got:\n%s", id, out)
		}
	}
}

// TestBackupDryRunCreatesNothing asserts the backup preview neither
// creates the output directory nor writes an archive.
func TestBackupDryRunCreatesNothing(t *testing.T) {
	db := setupTestDB(t)
	outDir := filepath.Join(t.TempDir(), "backups")

	out, err := db.run("backup", "--output-dir", outDir, "--dry-run")
	if err != nil {
		t.Fatalf("backup --dry-run should succeed: %v", err)
	}
	if _, statErr := os.Stat(outDir); !os.IsNotExist(statErr) {
		t.Errorf("dry-run must not create the output directory %s", outDir)
	}
	if !strings.Contains(out, "Would create archive") {
		t.Errorf("preview should report the archive it would write, got:\n%s", out)
	}
}

// TestDryRunPreviewIsByteStable asserts repeated previews over an
// unchanged store produce identical output, so agents can diff them.
func TestDryRunPreviewIsByteStable(t *testing.T) {
	for _, args := range [][]string{
		{"housekeeping", "vacuum", "--dry-run"},
		{"housekeeping", "compact", "--dry-run"},
		{"housekeeping", "reindex", "--dry-run"},
	} {
		t.Run(args[1], func(t *testing.T) {
			db := setupTestDB(t)
			seedObjects(t, db, "obj_stable")

			first, err := db.run(args...)
			if err != nil {
				t.Fatalf("first run: %v", err)
			}
			second, err := db.run(args...)
			if err != nil {
				t.Fatalf("second run: %v", err)
			}
			if first != second {
				t.Errorf("preview not byte-stable:\nfirst:\n%s\nsecond:\n%s", first, second)
			}
		})
	}
}
