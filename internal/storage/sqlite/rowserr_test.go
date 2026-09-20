package sqlite

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// errMidScan is the driver error injected partway through a result set.
var errMidScan = errors.New("disk I/O error mid-scan")

// failingRows delivers failAt rows, then reports errMidScan. That is how a
// real driver signals a truncated result set: Next returns a non-io.EOF error
// and database/sql exposes it only via Rows.Err. A `for rows.Next()` loop with
// no trailing rows.Err check ends identically on a clean result and on a
// truncated one, so the caller returns partial data as if it were complete.
type failingRows struct {
	cols   []string
	rows   [][]driver.Value
	failAt int
	pos    int
}

func (r *failingRows) Columns() []string { return r.cols }
func (r *failingRows) Close() error      { return nil }

func (r *failingRows) Next(dest []driver.Value) error {
	if r.pos >= r.failAt {
		return errMidScan
	}
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}

// staticRows is a clean result set terminated by io.EOF.
type staticRows struct {
	cols []string
	rows [][]driver.Value
	pos  int
}

func (r *staticRows) Columns() []string { return r.cols }
func (r *staticRows) Close() error      { return nil }

func (r *staticRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}

type fakeConn struct {
	mk func(query string) (driver.Rows, error)
}

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)           { return nil, errors.New("not supported") }

func (c *fakeConn) Query(query string, _ []driver.Value) (driver.Rows, error) {
	return c.mk(query)
}

type fakeDriver struct {
	mk func(query string) (driver.Rows, error)
}

func (d *fakeDriver) Open(string) (driver.Conn, error) { return &fakeConn{mk: d.mk}, nil }

var fakeDriverSeq struct {
	sync.Mutex
	n int
}

// openFakeDB registers a one-off driver answering every query via mk.
func openFakeDB(t *testing.T, mk func(query string) (driver.Rows, error)) *sql.DB {
	t.Helper()
	fakeDriverSeq.Lock()
	fakeDriverSeq.n++
	name := fmt.Sprintf("sqlite-rowserr-fake-%d", fakeDriverSeq.n)
	fakeDriverSeq.Unlock()

	sql.Register(name, &fakeDriver{mk: mk})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// countRows answers the `SELECT COUNT(*)` each List issues before its main
// query, so the store reaches the row-iteration path under test.
func countRows(n int64) driver.Rows {
	return &staticRows{cols: []string{"count"}, rows: [][]driver.Value{{n}}}
}

// truncatedListDB returns a DB whose COUNT succeeds and whose list query dies
// after delivering one row.
func truncatedListDB(t *testing.T, cols []string, row []driver.Value) *sql.DB {
	t.Helper()
	return openFakeDB(t, func(query string) (driver.Rows, error) {
		if strings.Contains(query, "COUNT(*)") {
			return countRows(2), nil
		}
		return &failingRows{
			cols:   cols,
			rows:   [][]driver.Value{row, row},
			failAt: 1,
		}, nil
	})
}

// assertTruncationSurfaces fails unless err both is non-nil and carries the
// injected driver error.
func assertTruncationSurfaces(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s returned nil error on a truncated result set; partial data reported as complete", name)
	}
	if !errors.Is(err, errMidScan) && !strings.Contains(err.Error(), errMidScan.Error()) {
		t.Fatalf("%s error does not carry the driver failure: %v", name, err)
	}
}

const ts = "2026-05-01T00:00:00Z"

// TestDetectorStoreList_SurfacesMidIterationError pins the rowserrcheck fix
// for DetectorStore.List.
func TestDetectorStoreList_SurfacesMidIterationError(t *testing.T) {
	db := truncatedListDB(t,
		[]string{"id", "kind", "name", "pipeline_name", "pattern", "priority", "enabled", "created_at", "updated_at"},
		[]driver.Value{"det-1", "regex", "n", "p", "pat", int64(1), int64(1), ts, ts},
	)
	s := &DetectorStore{db: db}
	_, _, err := s.List(t.Context(), storage.DetectorFilter{})
	assertTruncationSurfaces(t, "DetectorStore.List", err)
}

// TestPipelineStoreList_SurfacesMidIterationError pins PipelineStore.List.
func TestPipelineStoreList_SurfacesMidIterationError(t *testing.T) {
	db := truncatedListDB(t,
		[]string{"id", "name", "description", "steps", "is_built_in", "archived", "sandbox", "created_at", "updated_at"},
		[]driver.Value{"pl-1", "n", "d", "[]", int64(0), int64(0), "{}", ts, ts},
	)
	s := &PipelineStore{db: db}
	_, _, err := s.List(t.Context(), storage.PipelineFilter{})
	assertTruncationSurfaces(t, "PipelineStore.List", err)
}

// TestRegistryStoreList_SurfacesMidIterationError pins RegistryStore.List.
func TestRegistryStoreList_SurfacesMidIterationError(t *testing.T) {
	db := truncatedListDB(t,
		[]string{"registry_url", "manifest", "last_fetched", "etag", "auto_update"},
		[]driver.Value{"https://example.test", "{}", ts, "etag", int64(0)},
	)
	s := &RegistryStore{db: db}
	_, _, err := s.List(t.Context())
	assertTruncationSurfaces(t, "RegistryStore.List", err)
}

// TestStepStoreList_SurfacesMidIterationError pins StepStore.List.
func TestStepStoreList_SurfacesMidIterationError(t *testing.T) {
	db := truncatedListDB(t,
		[]string{"name", "source", "path", "metadata", "installed_at", "updated_at"},
		[]driver.Value{"step-1", "local", "/p", "{}", ts, ts},
	)
	s := &StepStore{db: db}
	_, _, err := s.List(t.Context(), "")
	assertTruncationSurfaces(t, "StepStore.List", err)
}

// TestReminderStoreList_SurfacesMidIterationError pins ReminderStore.List.
func TestReminderStoreList_SurfacesMidIterationError(t *testing.T) {
	db := truncatedListDB(t,
		[]string{"id", "type", "title", "message", "source", "action_url", "dismissed", "created_at", "updated_at"},
		[]driver.Value{"rem-1", "t", "ti", "m", "s", "u", int64(0), ts, ts},
	)
	s := &ReminderStore{db: db}
	_, _, err := s.List(t.Context(), false)
	assertTruncationSurfaces(t, "ReminderStore.List", err)
}

// TestDetectorStoreList_CleanIterationSucceeds is the control: with no
// injected error the same path returns every row and no error, proving the
// tests above detect the driver failure rather than a query failure.
func TestDetectorStoreList_CleanIterationSucceeds(t *testing.T) {
	cols := []string{"id", "kind", "name", "pipeline_name", "pattern", "priority", "enabled", "created_at", "updated_at"}
	row := []driver.Value{"det-1", "regex", "n", "p", "pat", int64(1), int64(1), ts, ts}
	db := openFakeDB(t, func(query string) (driver.Rows, error) {
		if strings.Contains(query, "COUNT(*)") {
			return countRows(2), nil
		}
		return &staticRows{cols: cols, rows: [][]driver.Value{row, row}}, nil
	})
	s := &DetectorStore{db: db}
	got, total, err := s.List(t.Context(), storage.DetectorFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || total != 2 {
		t.Fatalf("len = %d, total = %d, want 2 and 2", len(got), total)
	}
}
