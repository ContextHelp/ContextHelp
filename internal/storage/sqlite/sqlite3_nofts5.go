//go:build !fts5

package sqlite

// This package requires the fts5 build tag: migrations create FTS5 virtual
// tables, which fail at runtime ("no such module: fts5") when SQLITE_ENABLE_FTS5
// is not compiled into the embedded SQLite amalgamation.
//
// Rebuild with -tags fts5 (or set GOFLAGS=-tags=fts5) to fix this error.
//
// The undefined identifier below is deliberate: it turns a missing build tag
// into a compile failure instead of a mid-migration SQL error.
const fts5Enabled = fts5BuildTagRequired_RebuildWith_tags_fts5
