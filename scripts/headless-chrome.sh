#!/usr/bin/env bash
# Run a page through headless Chrome for a manual check, with the launch
# rules the e2e suite uses: mock keychain, throwaway profile, no sync, no
# extensions. The switches live in internal/browser/launch; this only
# builds and runs cmd/headless-chrome.
#
# Usage:
#   scripts/headless-chrome.sh dump [--timeout 20s] [--attempts N] [--until RE] <url-or-file> [-- chrome-switch...]
#   scripts/headless-chrome.sh path
#
# CTXT_CHROME picks the Chrome or Chromium binary.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

go -C "$root" build -buildvcs=false -o "$tmp/headless-chrome" ./cmd/headless-chrome
"$tmp/headless-chrome" "$@"
