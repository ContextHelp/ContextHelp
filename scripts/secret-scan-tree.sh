#!/usr/bin/env bash
# Scan the checkout's file tree for secrets with gitleaks.
#
# Scans what git would commit: tracked files as they are on disk plus
# untracked files that are not ignored. `gitleaks dir .` alone walks
# ignored paths too (a local .env, build output, node_modules), and
# `gitleaks detect` scans history, which this repo cannot rewrite.
# Extra arguments go to `gitleaks dir` (e.g. -r report.json -f json).
set -euo pipefail

ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
cd "$ROOT"

if ! command -v gitleaks >/dev/null 2>&1; then
	echo "gitleaks not installed. Run: make install-gitleaks" >&2
	exit 1
fi
"$ROOT/scripts/gitleaks-version-check.sh" || true

TREE="$(mktemp -d)"
trap 'rm -rf "$TREE"' EXIT

# Copy the file set so findings keep repo-relative paths, which the
# path-scoped allowlists in .gitleaks.toml match against.
git ls-files -z --cached --others --exclude-standard |
	while IFS= read -r -d '' f; do
		# Deleted-but-tracked files are listed too; skip them.
		if [ -e "$f" ] || [ -L "$f" ]; then printf '%s\0' "$f"; fi
	done |
	tar --null -T - -cf - | tar -xf - -C "$TREE"

cd "$TREE"
gitleaks dir . --config "$ROOT/.gitleaks.toml" --redact --verbose "$@"
