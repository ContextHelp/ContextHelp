#!/usr/bin/env bash
# Verify the Operator-Impact trailer of every commit in BASE..HEAD (ADR-070).
#
#   scripts/ci/check-operator-impact.sh <base> <head>
#
# Each commit's trailer must be one of the buckets below (absent = none). A
# non-none bucket needs a row in a docs/release-notes/*.md file the range adds
# or modifies, naming the commit's short SHA and the bucket.
#
# A published commit's trailer cannot be edited without rewriting history.
# docs/release-notes/operator-impact-corrections.tsv records the corrected
# bucket for such a commit (full SHA, bucket, reason); the check uses it in
# place of the trailer. The release-notes check workflow runs this script, and
# so can a local run: make check-operator-impact.
set -euo pipefail

base=${1:?usage: check-operator-impact.sh <base> <head>}
head=${2:?usage: check-operator-impact.sh <base> <head>}
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
corrections=$root/docs/release-notes/operator-impact-corrections.tsv
buckets="none reindex_auto reingest_selective reingest_all"

valid_bucket() { case " $buckets " in *" $1 "*) return 0 ;; esac; return 1; }

if [ -f "$corrections" ]; then
	while IFS=$'\t' read -r sha bucket reason; do
		case $sha in '' | '#'*) continue ;; esac
		if ! [[ "$sha" =~ ^[0-9a-f]{40}$ ]]; then
			echo "::error::$corrections: '$sha' is not a full 40-character SHA" >&2
			exit 1
		fi
		if ! valid_bucket "$bucket" || [ -z "${reason:-}" ]; then
			echo "::error::$corrections: $sha needs a valid bucket ($buckets) and a reason" >&2
			exit 1
		fi
	done <"$corrections"
fi

# corrected_bucket <sha>: the bucket the corrections file records, if any.
corrected_bucket() {
	[ -f "$corrections" ] || return 0
	awk -F'\t' -v s="$1" '$1 == s { print $2; exit }' "$corrections"
}

commits=$(git -C "$root" rev-list "${base}..${head}")
if [ -z "$commits" ]; then
	echo "No commits in range; nothing to check."
	exit 0
fi
notes_files=$(git -C "$root" diff --name-only --diff-filter=AM "${base}..${head}" -- 'docs/release-notes/*.md' || true)

fail=0
for sha in $commits; do
	trailer=$(corrected_bucket "$sha")
	if [ -z "$trailer" ]; then
		trailer=$(git -C "$root" log -1 --format='%(trailers:key=Operator-Impact,valueonly,separator=%x20)' "$sha" || true)
		trailer="${trailer## }"
		trailer="${trailer%% }"
		[ -n "$trailer" ] || trailer=none # ADR-070 §4: absent means none
	fi

	if ! valid_bucket "$trailer"; then
		echo "::error::Commit $sha has invalid Operator-Impact trailer: '$trailer'"
		echo "::error::Valid values: ${buckets// / | }. See ADR-070 §1."
		echo "::error::A published commit is corrected in docs/release-notes/operator-impact-corrections.tsv."
		fail=1
		continue
	fi
	[ "$trailer" != none ] || continue

	if [ -z "$notes_files" ]; then
		echo "::error::Commit $sha declares Operator-Impact: $trailer but no docs/release-notes/*.md file is added or modified in this range (template: docs/release-notes/template.md)."
		fail=1
		continue
	fi
	short_sha=${sha:0:7}
	found=0
	for f in $notes_files; do
		if grep -qF "$short_sha" "$root/$f" && grep -qF "$trailer" "$root/$f"; then
			found=1
			break
		fi
	done
	if [ $found -eq 0 ]; then
		echo "::error::Commit $sha (short SHA $short_sha) declares Operator-Impact: $trailer but no release-notes file in this range names both the SHA and the bucket. Files searched: $notes_files"
		fail=1
	fi
done

if [ $fail -ne 0 ]; then
	echo "::error::Release-notes check failed. Each non-none Operator-Impact commit needs a matching row in docs/release-notes/<release-date>.md."
	exit 1
fi
echo "All Operator-Impact declarations are accounted for."
