#!/usr/bin/env bash
# Write the sqlite3.h a cross toolchain lacks into an include directory.
#
#   scripts/ci/sqlite3-include.sh <dir>
#
# github.com/asg017/sqlite-vec-go-bindings/cgo compiles with -DSQLITE_CORE
# and #includes "sqlite3.h". macOS and Linux supply one from the system;
# mingw-w64 (the windows/amd64 cross build) has none. The SQLite the binary
# links is the amalgamation github.com/mattn/go-sqlite3 compiles, whose
# header that module ships as sqlite3-binding.h. This copies it, at the
# version go.mod resolves, to <dir>/sqlite3.h; pass -I<dir> in CGO_CFLAGS
# and sqlite-vec compiles against the very SQLite it links with.
#
# Used by the Makefile's windows targets and .goreleaser.yaml's windows
# builds. Runs the go on PATH.
set -euo pipefail

[ $# -eq 1 ] || {
	echo "usage: $0 <dir>" >&2
	exit 2
}
mkdir -p "$1"
out=$(cd "$1" && pwd)
mod=github.com/mattn/go-sqlite3

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
go mod download "$mod"
dir=$(go list -m -f '{{if .Replace}}{{.Replace.Dir}}{{else}}{{.Dir}}{{end}}' "$mod")
src=$dir/sqlite3-binding.h
[ -f "$src" ] || {
	echo "ERROR: $src not found; $mod no longer ships its amalgamation header under that name" >&2
	exit 1
}
# The module cache is read-only; the copy must stay replaceable.
rm -f "$out/sqlite3.h"
cp "$src" "$out/sqlite3.h"
chmod u+w "$out/sqlite3.h"
