# Release Notes — `<release-date>`

> Per **ADR-070**, every release that affects stored knowledge, the index, or the pipeline contract must declare its operator impact in the table below.

## Operator impact

| Change (PR / commit) | Bucket | Reindex? | Re-ingest? | Selectivity | Action required |
|---|---|---|---|---|---|
| `<PR title>` (`<sha>`) | `reindex_auto` \| `reingest_selective` \| `reingest_all` \| `none` | yes / no | yes / no / selective | `<SQL predicate>` or `<pipeline name>` or `all` or `n/a` | `<one-line operator step>` or `automatic` |

**Buckets**:
- `reindex_auto` — index rebuild needed; daemon detects via signature mismatch and runs automatically. **No operator action.**
- `reingest_selective` — pipeline behavior changed for a subset of objects. **The daemon re-derives it by itself at start** for pipelines that call no model, or only local ones (ADR-086). Hosted, unknown or unavailable providers defer the group, and its objects keep their text. `ctxt upgrade run` (with optional `--dry-run`, `--filter`, `--rate-limit`) re-derives from stored content only, cannot apply a change that needs a fetch, and leaves those objects stale.
- `reingest_all` — full-corpus re-ingest. **Daemon refuses** without explicit `ctxt upgrade run --all --i-understand-the-cost <sha>` confirmation.
- `none` — no operator-visible impact (config-only, doc-only, internal refactor).

---

## Changes by bucket

### `reindex_auto` changes

For each `reindex_auto` change, list:

- **What changed** — tokenizer? FTS schema? projection logic? embedding-index format?
- **Signature bump** — old → new signature (informational; surfaced in startup logs).
- **Estimated rebuild wall-clock** — typical / worst-case at current corpus size.

> *Example:*
> *FTS tokenizer updated to add Arabic case-folding. Signature: `tok-v3` → `tok-v4`. Wall-clock at typical 10K-object corpus: ~30s.*

### `reingest_selective` changes

For each `reingest_selective` change, list:

- **What changed** — which pipeline / step / projection / decoration logic?
- **What changed in stored output** — and which objects are affected (pipeline names, or a selector predicate consumed by `ctxt upgrade plan`).
- **Step version** — which step version was bumped (`step@vN` → `step@vN+1`). Bump only when the step's output for the same input changed.
- **What the daemon does by itself** — stored content or a guarded re-fetch; pause between objects (`upgrade.rederive.pause`, default `2s`).
- **What it does not do** — pipelines with a hosted, unknown or unavailable provider are deferred (`ctxt upgrade status`); pages that are gone, changed or redirect elsewhere stay as captured and are listed as skips.
- **Cost** — the formula, not a guess: per object, the pause, plus the fetch or fetches if the change is fetch-side (a repository takes two API calls, more on a retry), plus the time of the local models the pipeline's LLM steps call; wall-clock about `N × (pause + fetch + model time)` for `N` stale objects.
- **Hold back** — `upgrade.rederive.enabled: false`, set before the first start of the version.
- **Operator action** — usually none. Say what a deferred group needs, if anything.

> *Example:*
> *`text.short` pipeline now runs `markdown_parser` for documents with bullet-list bodies. Affected: `text.short`. Step version: `markdown_parser@1` → `@2`. The daemon re-derives on stored content at the next start (no model, or a local one). Cost: ~3s/object + 2s pause, × ~50 objects = ~4min; model calls of the local model only.*

### `reingest_all` changes

For each `reingest_all` change, list:

- **Justification** — why a full re-ingest, not a selective one? (This is required prose; if you can't write a justification, you have a `reingest_selective` change in disguise.)
- **Old pipeline retained?** — yes/no. Is the prior pipeline registered alongside the new one for graceful migration, or hard-replaced?
- **Cost estimate (current corpus)** — typical wall-clock + dollars at present KB size.
- **Cost estimate (next-year corpus)** — at 10×–100× projected scale.
- **Consent SHA256** — the token operators must echo via `--i-understand-the-cost <sha>`. Generated from this release's content hash.

> *Example:*
> *Embedding model swap: `openai-text-embedding-3-small@2025-01-15` → `openai-text-embedding-3-large@2026-05-01`. Justification: provider deprecated the old model on 2026-08-01. Old retained: yes (per ADR-071, dual-write during migration window, default-flip after coverage + recall verification). Cost (current): ~$8 + ~10min. Cost (10×): ~$80 + ~100min. Consent SHA256: `b3a1f7e2…`.*

### `none` changes

Optional. Brief one-liners for changes with no operator impact (config refactors, doc updates, test-only commits). Skip the section entirely if there are none.

---

## Verification

For `reingest_selective` and `reingest_all` changes that claim a behavior improvement, attach the `hop.top/ben` benchmark output:

- **Suite**: `suites/<name>.ben.yaml`
- **Pre-upgrade recall**: `<value>`
- **Post-upgrade recall**: `<value>`
- **Delta**: `+<value>` (must be non-negative for the PR to merge — see ADR-070 §6 *Test strategy*)

---

## Upgrade walkthrough

If this release contains any `reingest_selective` or `reingest_all` change, include a walkthrough:

1. **Before upgrading** — back up: `ctxt backup create --to <path>`.
2. **Pull / install** — `<command>`.
3. **Plan** — `ctxt upgrade plan` shows what will be re-ingested.
4. **Run** — the daemon starts the re-derive by itself for local pipelines. `ctxt upgrade status` shows what it defers; those objects keep their text.
5. **Verify** — `ctxt upgrade status --watch` until done.
6. **Roll back if needed** — `ctxt backup restore --from <path>` (operator decides; rollback is not automatic).

---

## Filing a change against this template

Each PR that lands a change with operator impact:

1. Adds a row to the table at the top. A step-version bump (`StepContract.Version`) is a `reingest_selective` change: the row says which pipelines are affected and what the daemon does.
2. Fills in the per-bucket section that applies.
3. Adds a `Operator-Impact:` trailer to the commit message:
   ```
   Operator-Impact: reingest_selective
   ```
4. CI (`.github/workflows/release-notes-check.yml`) verifies the trailer matches a row in the corresponding release-notes file.

The PR template (`.github/PULL_REQUEST_TEMPLATE.md`) prompts for the bucket up front.
