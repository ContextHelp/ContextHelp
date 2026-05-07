<!--
Thanks for contributing to ctxt / dpkms.

Per ADR-070, every change that affects stored knowledge, the index, or the
pipeline contract must declare its operator impact. The CI check at
`.github/workflows/release-notes-check.yml` enforces this.
-->

## Summary

<!-- One paragraph: what changed and why. -->

## Operator impact

> Pick exactly one bucket. If unsure, read ADR-070 §1 *Upgrade taxonomy*.

- [ ] **`none`** — config-only, doc-only, internal refactor, or test-only change. No effect on stored content, index, or pipeline behavior.
- [ ] **`reindex_auto`** — index rebuild needed (tokenizer / FTS schema / projection format). Daemon detects via signature mismatch and runs automatically. **No operator action.**
- [ ] **`reingest_selective`** — pipeline behavior changed for a subset of stored objects. Operator runs `ctxt upgrade run` (with optional `--dry-run`, `--filter`, `--rate-limit`). **Justification block required below.**
- [ ] **`reingest_all`** — full-corpus re-ingest. Operator must echo a SHA256 consent token via `ctxt upgrade run --all --i-understand-the-cost <sha>`. **Justification block required below.**

### Required for `reingest_selective` and `reingest_all`

If you ticked one of those buckets, fill these in. The CI check will refuse to merge a `reingest_*` PR without a corresponding row in `docs/release-notes/<release-date>.md`.

**Justification** — *why is this change unavoidably bucket-2/3?*

> Every `reingest_*` is a tax on every operator. Explain why a `none` or `reindex_auto` shape was not viable. If you don't have an answer, the design needs more iteration first.

**Versioned pipeline?** — yes / no

> A pipeline behavior change that bumps `<name>@vN` is preferred over an in-place change, because old objects keep their contract. If "no", explain why versioning was not feasible.

**Selector predicate** (`reingest_selective` only) — SQL WHERE clause or pipeline-name filter consumed by `ctxt upgrade plan`.

```
<predicate>
```

**Cost estimates**

| Scope | Wall-clock | LLM cost |
|---|---|---|
| Typical corpus | | |
| Worst-case / 10× scale | | |

**Verification (`hop.top/ben` suite)** — required if this PR claims a behavior improvement that must be measured (e.g. recall fix).

```
suite: suites/<name>.ben.yaml
pre-upgrade:  <metric value>
post-upgrade: <metric value>
delta:        +<value>
```

### Required for all PRs

- [ ] Release-notes row added to `docs/release-notes/<release-date>.md` (or new file created).
- [ ] Commit message includes the trailer `Operator-Impact: <bucket>` (CI parses this).
- [ ] `hop.top/eva` contracts updated if this PR changes operator-facing JSON shapes (`ctxt upgrade status`, `/healthz`, `staleness_warning`, `ctxt embeddings list`).
- [ ] `hop.top/xrr` cassettes added/updated if this PR changes external-call surfaces (LLM, embedding, HTTP).
- [ ] Tests pass: `make test`.

## How to test

<!-- Concrete steps. If reingest_selective/all, include the upgrade walkthrough. -->

## Related

<!-- Linked issues, ADRs, prior PRs. -->
