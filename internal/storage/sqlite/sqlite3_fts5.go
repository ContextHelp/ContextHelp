//go:build fts5

// Package sqlite uses mattn/go-sqlite3 with FTS5 and sqlite-vec extensions.
// The fts5 build tag must be present (go build -tags fts5) to compile
// SQLITE_ENABLE_FTS5 into the embedded SQLite amalgamation.
//
// This file carries the package documentation for the canonical build. Its
// counterpart in sqlite3_nofts5.go fails the build when the tag is absent, so
// a missing tag surfaces at compile time rather than at first migration.
package sqlite
