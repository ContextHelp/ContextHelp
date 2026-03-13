#!/usr/bin/env bash
# .mise/motd.sh

set -euo pipefail

start_dir="$PWD"
dir="$PWD"

motd=""
repo_root=""

# 1) If current folder has .motd, use it (even if not a repo)
if [[ -f "$start_dir/.motd" ]]; then
  motd="$start_dir/.motd"
else
  # 2) Otherwise walk up until repo root (.git) or /
  while [[ "$dir" != "/" ]]; do
    if [[ -d "$dir/.git" ]]; then
      repo_root="$dir"
      if [[ -f "$dir/.motd" ]]; then
        motd="$dir/.motd"
      fi
      break
    fi
    dir="$(dirname "$dir")"
  done
fi

# No motd => silent exit
[[ -z "$motd" ]] && exit 0

# If we're in a repo, compute a per-branch/worktree key.
# (Detached HEAD -> falls back to short SHA.)
git_key="norepo"
if [[ -n "$repo_root" ]]; then
  if command -v git >/dev/null 2>&1; then
    if git -C "$repo_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
      branch="$(
        git -C "$repo_root" symbolic-ref --quiet --short HEAD 2>/dev/null || true
      )"
      if [[ -z "${branch:-}" ]]; then
        branch="$(
          git -C "$repo_root" rev-parse --short HEAD 2>/dev/null || echo detached
        )"
      fi
      git_key="$repo_root::$branch"
    fi
  fi
fi

# Cache key hashing (portable across macOS/Linux)
hash="$(
  printf "%s" "$git_key" | shasum 2>/dev/null | awk '{print $1}' \
  || printf "%s" "$git_key" | sha1sum 2>/dev/null | awk '{print $1}'
)"

cache_base="${XDG_CACHE_HOME:-$HOME/.cache}/repo-motd"
cache_dir="$cache_base/$hash"

stamp_file="$cache_dir/last_shown_epoch"
motd_mtime_file="$cache_dir/last_motd_mtime"
motd_path_file="$cache_dir/last_motd_path"

mkdir -p "$cache_dir"

now="$(date +%s)"

epoch_last=0
[[ -f "$stamp_file" ]] && epoch_last="$(cat "$stamp_file" 2>/dev/null || echo 0)"

last_motd_path=""
[[ -f "$motd_path_file" ]] && last_motd_path="$(cat "$motd_path_file" 2>/dev/null || echo "")"

# Portable mtime (macOS + Linux)
motd_mtime="$(
  stat -f %m "$motd" 2>/dev/null || stat -c %Y "$motd" 2>/dev/null || echo ""
)"

last_motd_mtime=""
[[ -f "$motd_mtime_file" ]] && last_motd_mtime="$(cat "$motd_mtime_file" 2>/dev/null || echo "")"

# Reset timer if motd path changed OR motd changed
if [[ "$motd" != "$last_motd_path" ]]; then
  epoch_last=0
fi
if [[ -n "$motd_mtime" && "$motd_mtime" != "$last_motd_mtime" ]]; then
  epoch_last=0
fi

# 2 hours = 7200 seconds
if (( now - epoch_last < 7200 )); then
  exit 0
fi

echo ""

if [[ -x "$motd" ]]; then
  "$motd"
else
  cat "$motd"
fi

echo ""

echo "$now" > "$stamp_file"
echo "$motd" > "$motd_path_file"
[[ -n "$motd_mtime" ]] && echo "$motd_mtime" > "$motd_mtime_file"
