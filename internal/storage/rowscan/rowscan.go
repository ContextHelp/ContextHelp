// Package rowscan collects *sql.Rows into slices for the storage drivers.
package rowscan

import "database/sql"

// Scanner is the Scan method shared by *sql.Row and *sql.Rows. It is an
// alias so a scan function declared over the bare interface literal fits
// AllScanner without a wrapper.
type Scanner = interface{ Scan(dest ...any) error }

// All scans every remaining row with scan and returns the values in row
// order. It stops at the first scan error. It returns rows.Err() alongside
// whatever it collected, so a mid-iteration driver failure is never mistaken
// for the end of the result set. A query that yields no rows returns a nil
// slice. The caller still owns rows and closes it.
func All[T any](rows *sql.Rows, scan func(*sql.Rows) (T, error)) ([]T, error) {
	// The row count is unknown until iteration ends, so there is nothing to
	// size the slice by, and zero rows must stay nil for callers that check.
	var out []T //nolint:prealloc // row count unknown before iteration; nil on zero rows is the contract
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// AllScanner is All for a scan function written against Scanner, the form
// shared with single-row QueryRow lookups.
func AllScanner[T any](rows *sql.Rows, scan func(Scanner) (T, error)) ([]T, error) {
	return All(rows, func(r *sql.Rows) (T, error) { return scan(r) })
}
