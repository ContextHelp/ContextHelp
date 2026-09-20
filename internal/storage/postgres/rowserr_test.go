package postgres

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// errMidScan is the driver error injected partway through a result set.
var errMidScan = errors.New("connection reset mid-scan")

// failingRows yields cols/values until failAt rows have been delivered, then
// returns errMidScan. A real driver signalling a truncated result set does
// exactly this: Next returns a non-io.EOF error, which database/sql surfaces
// only through Rows.Err. Code that ends its loop on `for rows.Next()` and
// never calls rows.Err cannot tell this apart from a clean end of iteration.
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

// okRows is a clean result set: it ends with io.EOF and never errors.
type okRows struct {
	cols []string
	rows [][]driver.Value
	pos  int
}

func (r *okRows) Columns() []string { return r.cols }
func (r *okRows) Close() error      { return nil }

func (r *okRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}

// fakeConn answers every query with the rows the test registered for it.
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

// openFakeDB registers a one-off driver whose queries return mk(query) and
// hands back an *sql.DB speaking to it.
func openFakeDB(t *testing.T, mk func(query string) (driver.Rows, error)) *sql.DB {
	t.Helper()
	fakeDriverSeq.Lock()
	fakeDriverSeq.n++
	name := fmt.Sprintf("postgres-rowserr-fake-%d", fakeDriverSeq.n)
	fakeDriverSeq.Unlock()

	sql.Register(name, &fakeDriver{mk: mk})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

var edgeCols = []string{
	"id", "from_type", "from_id", "to_type", "to_id",
	"edge_type", "weight", "metadata", "created_at",
}

func edgeRow(id string) []driver.Value {
	return []driver.Value{
		id, "object", "obj-1", "entity", "ent-1",
		"mentions", float64(1), []byte("{}"), time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestEdgeStoreListFrom_SurfacesMidIterationError pins the rowserrcheck fix:
// a driver error after the first row must reach the caller as an error, not
// come back as a short-but-successful slice.
func TestEdgeStoreListFrom_SurfacesMidIterationError(t *testing.T) {
	db := openFakeDB(t, func(string) (driver.Rows, error) {
		return &failingRows{
			cols:   edgeCols,
			rows:   [][]driver.Value{edgeRow("edge-1"), edgeRow("edge-2")},
			failAt: 1, // one row delivered, then the connection dies
		}, nil
	})
	s := &EdgeStore{db: db}

	got, err := s.ListFrom(t.Context(), "object", "obj-1")
	if err == nil {
		t.Fatalf("ListFrom returned nil error with %d rows; a truncated result set was reported as complete", len(got))
	}
	if !errors.Is(err, errMidScan) && !strings.Contains(err.Error(), errMidScan.Error()) {
		t.Fatalf("error does not carry the driver failure: %v", err)
	}
}

// TestEdgeStoreListTo_SurfacesMidIterationError covers the sibling query path.
func TestEdgeStoreListTo_SurfacesMidIterationError(t *testing.T) {
	db := openFakeDB(t, func(string) (driver.Rows, error) {
		return &failingRows{
			cols:   edgeCols,
			rows:   [][]driver.Value{edgeRow("edge-1"), edgeRow("edge-2")},
			failAt: 1,
		}, nil
	})
	s := &EdgeStore{db: db}

	if _, err := s.ListTo(t.Context(), "entity", "ent-1"); err == nil {
		t.Fatal("ListTo returned nil error on a truncated result set")
	}
}

// TestEdgeStoreListFrom_CleanIterationSucceeds is the control: without an
// injected error the same path returns every row and no error, so the test
// above is detecting the driver failure rather than any query failure.
func TestEdgeStoreListFrom_CleanIterationSucceeds(t *testing.T) {
	db := openFakeDB(t, func(string) (driver.Rows, error) {
		return &okRows{
			cols: edgeCols,
			rows: [][]driver.Value{edgeRow("edge-1"), edgeRow("edge-2")},
		}, nil
	})
	s := &EdgeStore{db: db}

	got, err := s.ListFrom(t.Context(), "object", "obj-1")
	if err != nil {
		t.Fatalf("ListFrom: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}
