package rowscan_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "rowscan.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(context.Background(),
		`CREATE TABLE t (n INTEGER); INSERT INTO t VALUES (1), (2), (3)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return db
}

func scanInt(rows *sql.Rows) (int, error) {
	var n int
	err := rows.Scan(&n)
	return n, err
}

func TestAll(t *testing.T) {
	db := openDB(t)
	errBoom := errors.New("boom")

	tests := []struct {
		name    string
		query   string
		scan    func(*sql.Rows) (int, error)
		want    []int
		wantNil bool
		wantErr error
	}{
		{name: "rows in order", query: `SELECT n FROM t ORDER BY n`, scan: scanInt, want: []int{1, 2, 3}},
		{name: "no rows is nil", query: `SELECT n FROM t WHERE n > 10`, scan: scanInt, wantNil: true},
		{
			name:  "scan error stops",
			query: `SELECT n FROM t ORDER BY n`,
			scan: func(rows *sql.Rows) (int, error) {
				n, err := scanInt(rows)
				if err == nil && n == 2 {
					return 0, errBoom
				}
				return n, err
			},
			wantNil: true,
			wantErr: errBoom,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(context.Background(), tc.query)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			defer rows.Close()
			got, err := rowscan.All(rows, tc.scan)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("got %v, want nil slice", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
