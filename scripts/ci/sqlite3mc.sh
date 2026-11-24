#!/usr/bin/env bash
# Build SQLite3MultipleCiphers (sqlite3mc) static library and headers for
# encrypted SQLite builds (-tags "libsqlite3 fts5").
#
# Usage:
#   scripts/ci/sqlite3mc.sh <out-dir>
#
# Downloads the pinned SQLite3MultipleCiphers amalgamation release, verifies
# its SHA-256 digest, compiles libsqlite3.a with FTS5 + codec + RTree + JSON1,
# and places sqlite3.h/sqlite3ext.h under <out-dir>/include.
set -euo pipefail

[ $# -eq 1 ] || {
	echo "usage: $0 <out-dir>" >&2
	exit 2
}

VERSION="2.6.1"
SQLITE_VERSION="3.54.0"
ZIP="sqlite3mc-${VERSION}-sqlite-${SQLITE_VERSION}-amalgamation.zip"
URL="https://github.com/utelle/SQLite3MultipleCiphers/releases/download/v${VERSION}/${ZIP}"
SHA256="3f25af603566a23c821d8e581778b5ece2a541d2435776de57b8d845c176815a"

mkdir -p "$1"
OUT="$(cd "$1" && pwd)"

if [ -f "${OUT}/lib/libsqlite3.a" ] && [ -f "${OUT}/include/sqlite3.h" ] && [ -f "${OUT}/.version" ] && [ "$(cat "${OUT}/.version")" = "${VERSION}-${SQLITE_VERSION}" ]; then
	exit 0
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

if command -v gh >/dev/null 2>&1; then
	gh release download "v${VERSION}" -R utelle/SQLite3MultipleCiphers -p "${ZIP}" -p "sqlite3mc-${VERSION}-SHA256SUMS" -D "$WORK"
	EXPECTED="$(grep "${ZIP}" "$WORK/sqlite3mc-${VERSION}-SHA256SUMS" | awk '{print $1}')"
else
	curl -fsSL -o "${WORK}/${ZIP}" "${URL}"
	EXPECTED="${SHA256}"
fi

ACTUAL="$(shasum -a 256 "${WORK}/${ZIP}" | awk '{print $1}')"
if [ -n "${EXPECTED}" ] && [ "${ACTUAL}" != "${EXPECTED}" ]; then
	echo "ERROR: SHA-256 mismatch for ${ZIP}: got ${ACTUAL}, want ${EXPECTED}" >&2
	exit 1
fi

unzip -q "${WORK}/${ZIP}" -d "${WORK}/src"
mkdir -p "${OUT}/include" "${OUT}/lib"
cp "${WORK}/src/sqlite3.h" "${WORK}/src/sqlite3ext.h" "${WORK}/src/sqlite3mc_amalgamation.h" "${OUT}/include/"
chmod u+w "${OUT}/include/"*.h

CC="${CC:-cc}"
"${CC}" -O2 -c "${WORK}/src/sqlite3mc_amalgamation.c" -o "${WORK}/sqlite3mc.o" \
	-DSQLITE_ENABLE_FTS5 \
	-DSQLITE_ENABLE_RTREE \
	-DSQLITE_ENABLE_JSON1 \
	-DSQLITE_TEMP_STORE=2 \
	-DSQLITE_THREADSAFE=1 \
	-DSQLITE_ENABLE_UNLOCK_NOTIFY \
	-DSQLITE_USE_URI=1
ar rcs "${OUT}/lib/libsqlite3.a" "${WORK}/sqlite3mc.o"
printf '%s\n' "${VERSION}-${SQLITE_VERSION}" > "${OUT}/.version"
echo "Built sqlite3mc ${VERSION} (SQLite ${SQLITE_VERSION}) in ${OUT} (sha256=${ACTUAL})"
