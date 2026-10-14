#!/bin/sh
# Warn when the gitleaks on PATH is not the version mise.toml pins.
#
# Rules and default allowlists change between gitleaks releases, so a scan
# on another version is weaker evidence about what the pin would report.
# Exits 1 on a mismatch (callers decide whether that blocks); silent and 0
# when gitleaks or the pin is missing.
root=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || exit 0
[ -f "$root/mise.toml" ] || exit 0
want=$(sed -n 's/^gitleaks = "\(.*\)"$/\1/p' "$root/mise.toml")
[ -n "$want" ] || exit 0
command -v gitleaks >/dev/null 2>&1 || exit 0
have=$(gitleaks version 2>/dev/null)
have=${have#v}
if [ "$have" != "$want" ]; then
	echo "warning: gitleaks $have on PATH, mise.toml pins $want; run: make install-gitleaks" >&2
	exit 1
fi
