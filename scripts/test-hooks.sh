#!/usr/bin/env bash
# Exercise the installed pre-commit hook against known secret shapes.
#
# Resolves the hook the way git does (core.hooksPath, worktree-safe), so it
# tests what `make install-hooks` wired. Each scenario is staged in a
# throwaway repo; the checkout's index, worktree and .env are never touched.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel)"

if [ "$(git -C "$ROOT" rev-parse --git-path hooks)" != ".githooks" ]; then
    echo "hooks not installed (core.hooksPath is not .githooks); run: make install-hooks" >&2
    exit 1
fi
HOOK="$ROOT/.githooks/pre-commit"
if [ ! -x "$HOOK" ]; then
    echo "$HOOK missing or not executable; run: make install-hooks" >&2
    exit 1
fi
if ! command -v gitleaks >/dev/null 2>&1; then
    echo "gitleaks not installed; the hook would skip the scan. Run: make install-gitleaks" >&2
    exit 1
fi

SANDBOX="$(mktemp -d)"
trap 'rm -rf "$SANDBOX"' EXIT
# Scenarios carry no lint config; let the chained pre-commit stage no-op.
export PRE_COMMIT_ALLOW_NO_CONFIG=1

passed=0
failed=0

# scenario <name> <file> <content> <expect: block|allow>
scenario() {
    local name=$1 file=$2 content=$3 expect=$4 repo got
    repo="$SANDBOX/$((passed + failed))"
    git init -q "$repo"
    cp "$ROOT/.gitleaks.toml" "$repo/.gitleaks.toml"
    printf '%s\n' "$content" > "$repo/$file"
    git -C "$repo" add -- "$file"
    if (cd "$repo" && "$HOOK") > "$repo.out" 2>&1; then got=allow; else got=block; fi
    if [ "$got" = "$expect" ]; then
        echo "ok    $name ($got)"
        passed=$((passed + 1))
    else
        echo "FAIL  $name: want $expect, got $got"
        sed 's/^/      /' "$repo.out"
        failed=$((failed + 1))
    fi
}

# ${VAR} in scenario content is literal on purpose.
# shellcheck disable=SC2016
scenario "config with env var reference" safe-config.yaml 'api_key: ${OPENAI_API_KEY}' allow
scenario "OpenAI key" unsafe-config.yaml 'api_key: sk-proj-abcdefghijklmnopqrstuvwxyz1234567890' block
scenario "Anthropic key" anthropic.yaml 'key: sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuv' block
# shellcheck disable=SC2016
scenario "markdown docs" docs.md 'Use ${OPENAI_API_KEY} for configuration.' allow
scenario "AWS access key" aws.yaml 'aws_access_key_id: AKIAIOSFODNN7EXAMPLE' block
scenario "GitHub token" github.yaml 'token: ghp_abcdefghijklmnopqrstuvwxyz1234567890' block
scenario "example config placeholder" config.example.yaml 'api_key: sk-proj-your-key-here' allow

echo ""
echo "passed: $passed  failed: $failed"
[ "$failed" -eq 0 ]
