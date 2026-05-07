# Release Notes — `<release-date>`

> Per **ADR-070**, every release that affects stored knowledge, the index, or the pipeline contract must declare its operator impact in the table below.

## Operator impact

| Change (PR / commit) | Bucket | Reindex? | Re-ingest? | Selectivity | Action required |
|---|---|---|---|---|---|
| `<PR title>` (`<sha>`) | `reindex_auto` \| `reingest_selective` \| `reingest_all` \| `none` | yes / no | yes / no / selective | `<SQL predicate>` or `<pipeline name>` or `all` or `n/a` | `<one-line operator step>` or `automatic` |

**Buckets**:
- `reindex_auto` — index rebuild needed; daemon detects via signature mismatch and runs automatically. **No operator action.**
- `reingest_selective` — pipeline behavior changed for a subset of objects. **Operator runs `ctxt upgrade run`** (with optional `--dry-run`, `--filter`, `--rate-limit`).
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
- **Selector predicate** — machine-readable WHERE clause or pipeline-name filter (consumed by `ctxt upgrade plan`).
- **Versioned pipeline?** — yes/no. Did this gate behind `<pipeline>@vN`, or replace the pipeline contract in place?
- **Estimated wall-clock** — typical / worst-case for affected subset.
- **Estimated LLM cost** — typical / worst-case (or "none" for projection-only changes).

> *Example:*
> *`text.short` pipeline now runs `markdown_parser` for documents with bullet-list bodies. Selector: `pipeline = 'text.short@v1' AND raw_content LIKE '%- %'`. Versioned: yes (`@v1` → `@v2`). Wall-clock: ~3s/object × ~50 objects = ~2.5min. LLM cost: $0 (projection-only).*

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
4. **Run** — `ctxt upgrade run [--dry-run]`.
5. **Verify** — `ctxt upgrade status --watch` until done.
6. **Roll back if needed** — `ctxt backup restore --from <path>` (operator decides; rollback is not automatic).

---

## Filing a change against this template

Each PR that lands a change with operator impact:

1. Adds a row to the table at the top.
2. Fills in the per-bucket section that applies.
3. Adds a `Operator-Impact:` trailer to the commit message:
   ```
   Operator-Impact: reingest_selective
   ```
4. CI (`.github/workflows/release-notes-check.yml`) verifies the trailer matches a row in the corresponding release-notes file.

The PR template (`.github/PULL_REQUEST_TEMPLATE.md`) prompts for the bucket up front.
