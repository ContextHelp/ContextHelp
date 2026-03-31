package search

import "github.com/ideacrafterslabs/ctxt/internal/storage"

// Dialect identifies the SQL dialect used by a storage backend.
type Dialect int

const (
	// DialectSQLite targets SQLite: ? placeholders, json_each, objects_fts MATCH.
	DialectSQLite Dialect = iota
	// DialectPostgres targets PostgreSQL: $N placeholders, jsonb_array_elements.
	// The similar== field is unsupported (no FTS5) and returns an explicit error.
	DialectPostgres
)

// dialectDetector may be implemented by a StorageDriver to self-report its SQL
// dialect. The return value is a driver name string; "postgres" maps to
// DialectPostgres. Any other value (or absence of the interface) defaults to
// DialectSQLite.
//
// Using a string avoids a circular import: the postgres driver package can
// implement this without importing the search package.
type dialectDetector interface {
	SQLDialect() string
}

// DialectFor returns the SQL dialect for the given storage driver.
// It uses the dialectDetector interface when available; falls back to DialectSQLite.
func DialectFor(store storage.StorageDriver) Dialect {
	if dd, ok := store.(dialectDetector); ok && dd.SQLDialect() == "postgres" {
		return DialectPostgres
	}
	return DialectSQLite
}
