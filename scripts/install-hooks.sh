#!/usr/bin/env bash
# Install the repo's git hooks.
#
# Thin wrapper around the canonical installer, `make install-hooks`, which
# points core.hooksPath at the tracked .githooks directory. Works from any
# cwd and in linked worktrees, where .git is a file rather than a directory.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel)"
exec make -C "$ROOT" install-hooks
