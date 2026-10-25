package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// migratedDB returns the path of a migrated database holding one job,
// with a writer handle still open on it when keepWriter is set.
func migratedDB(t *testing.T, keepWriter bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	w, err := New(path, testBlobStore(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := w.Init(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := w.Jobs().Create(ctx, &storage.Job{
		ID: "job-1", Type: "analyze", Pipeline: "default", Status: storage.JobPending,
		Payload: "{}", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if keepWriter {
		t.Cleanup(func() { _ = w.Close(ctx) })
	} else if err := w.Close(ctx); err != nil {
		t.Fatal(err)
	}
	return path
}

// A read-only handle reads alongside a live writer (WAL readers do not
// block on it) and sees its committed rows.
func TestNewReadOnly_ReadsAlongsideWriter(t *testing.T) {
	path := migratedDB(t, true)
	ro, err := NewReadOnly(path, testBlobStore(t))
	if err != nil {
		t.Fatalf("read-only open beside a writer: %v", err)
	}
	t.Cleanup(func() { _ = ro.Close(context.Background()) })

	job, err := ro.Jobs().Get(context.Background(), "job-1")
	if err != nil || job == nil {
		t.Fatalf("read job through read-only handle: %v, %v", job, err)
	}
}

// No statement on a read-only handle writes: DML and DDL both fail, on
// every pooled connection, and Init refuses instead of migrating.
func TestNewReadOnly_RefusesWrites(t *testing.T) {
	path := migratedDB(t, false)
	ro, err := NewReadOnly(path, testBlobStore(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ro.Close(context.Background()) })
	ctx := context.Background()

	for name, stmt := range map[string]string{
		"dml": `DELETE FROM jobs`,
		"ddl": `CREATE TABLE ro_probe (a INTEGER)`,
	} {
		t.Run(name, func(t *testing.T) {
			for range 3 { // several pooled connections, not just the first
				if _, err := ro.DB().ExecContext(ctx, stmt); err == nil {
					t.Fatalf("%s succeeded on a read-only handle", stmt)
				}
			}
		})
	}
	if err := ro.Jobs().Cancel(ctx, "job-1"); err == nil {
		t.Error("store write succeeded on a read-only handle")
	}
	if err := ro.Init(ctx); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Init on a read-only handle = %v; want ErrReadOnly", err)
	}
	job, err := ro.Jobs().Get(ctx, "job-1")
	if err != nil || job.Status != storage.JobPending {
		t.Errorf("job after refused writes = %+v, %v; want still pending", job, err)
	}
}

// A missing database is an error, and opening it read-only creates
// nothing.
func TestNewReadOnly_MissingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.sqlite")
	if _, err := NewReadOnly(path, testBlobStore(t)); err == nil {
		t.Fatal("read-only open of a missing database succeeded")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("read-only open created the database: %v", err)
	}
}
