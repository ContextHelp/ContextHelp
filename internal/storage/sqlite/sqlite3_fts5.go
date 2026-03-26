// Package sqlite uses mattn/go-sqlite3 with FTS5 and sqlite-vec extensions.
// The fts5 build tag must be present (go build -tags fts5) to compile
// SQLITE_ENABLE_FTS5 into the embedded SQLite amalgamation.
//
// This file is a compile-time guard: if the fts5 tag is missing the package
// still compiles but migrations will fail at runtime. Add -tags fts5 to your
// build/test commands, or set GOFLAGS=-tags=fts5 in your environment.
package sqlite
