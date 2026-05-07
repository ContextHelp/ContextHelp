# ADR-070 – Pipeline & Index Versioning, Upgrade Taxonomy, and Operator Consent

> **Status:** Accepted
> **Date:** 2026-05-07
> **Author:** jadb
> **Applies to:** dpkms, ctxt CLI, storage layer, pipeline registry
> **Supersedes:** None
> **References:** ADR-004 (step-based pipeline), ADR-007 (transactional outbox), ADR-013 (knowledge graph), ADR-061 (search quality tiers), ADR-064 (federation), T-0565 (search-recall fix that prompted this ADR), T-0576 (config regression that compounded the upgrade pain), T-0577 (this ADR's source task)

---

## Context

In a 72-hour window in May 2026, two consecutive ctxt releases — both small, both individually defensible — required every operator to take manual action to recover from silent regressions:

1. **T-0565** (commit `232cd8f`) fixed a search-recall bug. The fix changed how `ProjectIndex` derives `FTSBody` for objects whose graph carries no `Summary`/`Section` nodes. **Existing objects ingested under the old code remained mis-projected** until they were re-ingested. From the operator's POV, search continued to return 0 results for documents already in the corpus until they manually re-ran the analyze step.

2. **T-0576** (commit `493e469`) fixed config auto-discovery. The fix renamed `$XDG_CONFIG_HOME/contexthelp/config.yaml` to per-binary `$XDG_CONFIG_HOME/contexthelp/{ctxt,dpkms}.yaml`. Existing operators silently fell through to defaults until they renamed the file.

Neither bug was a one-off. Each was a *category* of upgrade pain that the system has no established protocol for:

- **Index-shape changes** (T-0565: tokenizer / FTS schema / projection logic) require rebuilding the index but stored content is fine.
- **Pipeline-behavior changes** (T-0565 second-order: the same fix also implies that documents previously routed to `text.short` should be re-routed) require re-ingesting a *subset* of objects.
- **Hard contract breaks** (config rename, embedding model swap) require deliberate operator-driven migration.

These three shapes are not interchangeable, and conflating them — or worse, shipping them as "just a bugfix" — burns operator trust. The user feedback after the second incident was unambiguous: *"these kinds of updates should be kept to a strict minimum"* and *"make sure user is aware system being upgraded, something showing progress is a must."*

The system today has:
- **No notion of pipeline version** stored per object. The `pipeline` column holds a name (e.g. `text.short`) but not a version, so post-fix the daemon cannot distinguish "ingested under old `text.short` (bugged)" from "ingested under new `text.short` (fixed)."
- **No tokenizer or index signature** stored alongside `objects_fts`. A schema bump silently produces an empty or wrong-shape index until someone notices the recall is bad.
- **No upgrade-status surface.** No `ctxt upgrade status`, no `/healthz` upgrade field, no banner during in-progress migrations, no completion notification, no failure-stickiness. Silent processing.
- **No release-notes contract.** PRs land on `main` with no machine-readable declaration of operator impact.

This ADR establishes the framework. ADR-071 (embedding-index versioning, dual-write) builds on it.

### Constraints

- Local-first: dpkms is often deployed on a remote machine the operator doesn't see day-to-day. Visibility must be pull-mode (CLI commands, `/healthz`) — a notification on the operator's laptop is not enough by itself.
- Backwards-compatibility is **not** a goal in this phase. Per the user's standing directive: *"don't support multiple ways of doing the same thing for now."* Migrations are explicit, one-shot, and documented.
- The system must remain queryable during upgrades when at all possible (bucket 1 and bucket 2 below). Bucket 3 is the only mode where degradation-or-blackout is acceptable.

### Goals (priority-ordered)

1. **Operator consent**: every state transition the operator should know about is queryable, dated, and has a clear "who asked for this" answer (the daemon detected and ran it automatically vs. operator typed `ctxt upgrade run`).
2. **Strict minimum** of operator-action upgrades: most fixes route through bucket 1 (silent, automatic, visible-in-progress); bucket 2 is a quarterly event; bucket 3 is annual.
3. **Determinism for testing**: upgrade flows are testable end-to-end without burning LLM credits, without a live daemon, without time-of-day dependencies. Existing `xrr` cassette infrastructure extends to cover upgrade-time external calls.
4. **Verifiable improvement**: a `reingest_selective` PR that claims to fix recall must demonstrate the recall improvement on a fixed query corpus before merge.

---

## Decision

We adopt a four-part framework: **a three-bucket upgrade taxonomy**, **per-object pipeline versioning**, **per-table index signatures**, and **a release-notes contract** that operators and CI both consume.

### 1. Upgrade taxonomy (three buckets)

Every change that affects stored knowledge, the index, or the pipeline contract falls into exactly one bucket:

| Bucket | What it covers | Daemon behavior on detection | Operator action |
|---|---|---|---|
| **`reindex_auto`** | Tokenizer changes, FTS schema bumps, embedding-index format changes — anything where stored content is correct but the *index* needs rebuilding. | Detect signature mismatch on startup, run reindex automatically, surface visible progress (CLI banner + `/healthz` upgrade-state field + completion notification). Search continues to serve from the old index until the new one is hot, then atomic swap. | None unless reindex fails. |
| **`reingest_selective`** | Pipeline behavior changes that affect a specific subset of stored objects. T-0565 is the canonical example: only `text.short`-routed documents with bullet-list bodies are stale. | Detect via `pipeline_version` mismatch, mark affected objects `pending_upgrade`. Search returns those objects with `staleness_warning` set. `ctxt upgrade plan` shows a SQL-predicate-narrow selectivity preview; `ctxt upgrade run` executes. | Run `ctxt upgrade run` (with optional `--dry-run`, `--filter <selector>`, `--rate-limit`). |
| **`reingest_all`** | Full-corpus re-ingest. Embedding model swap, fundamental pipeline rewrite, ontology change. | Daemon refuses to run on a stale corpus; requires explicit `ctxt upgrade run --all --i-understand-the-cost <SHA256>` confirmation where the SHA256 is printed in the release notes for that release. | Deliberate, planned, rare. Often paired with operator-side LLM-cost budgeting. |

The operator's mental model collapses to **"did the daemon ask for my consent?"** Most upgrades are bucket 1 (invisible automatic). A few per quarter are bucket 2 (one CLI command). At most one per year should be bucket 3.

**Strict-minimum rule**: every PR that lands in bucket 2 or bucket 3 requires a justification block in the PR description — not just "fixes bug X." Each is a tax on every operator.

### 2. Per-object `pipeline_version`

The `objects.pipeline` column gains a version suffix encoded in the value itself: `text.short@v1`, `text.short@v2`. This is human-readable, ASCII-sortable, and requires no schema change beyond a string-shape convention.

- New objects ingested after a pipeline behavior change get the new version (`@v2`).
- Existing objects keep their original version (`@v1`) until explicitly migrated.
- The pipeline registry maps version-bearing names to step lists. Old versions remain registered (additive principle) so existing objects continue to behave correctly.
- A `reingest_selective` upgrade migrates objects from `@v1` to `@v2` by re-running the new pipeline against the original `RawContent` and atomically replacing the projection.

Versioning is bumped only when behavior would diverge for an existing object. Doc-only or test-only changes don't bump.

### 3. Per-table index signatures (the bucket-1 mechanism)

Each index-shaped artifact in the storage layer carries a signature stamp:

- **`objects_fts`** gains a `tokenizer_signature` row in a sibling `index_signatures` table. The signature is a deterministic hash of (tokenizer name + version + FTS schema DDL + projection logic version).
- **Vector index tables** (covered in ADR-071) carry an `embedding_signature` (model_id + dimension + provider).

On daemon startup, dpkms compares the on-disk signature against the build-time-known signature. Mismatch → `reindex_auto` flow fires automatically. Match → no work, daemon goes healthy.

The signature scheme is the *only* automatic-rebuild trigger. There is no heuristic detection ("looks empty?", "low recall?"). Either signatures match or they don't.

### 4. Release-notes contract

Every release (or every PR landing on `main` with operator impact) produces a release-notes section using a fixed schema:

```markdown
## <release-date> — <release-name>

### Operator impact

| Change | Bucket | Reindex? | Re-ingest? | Selectivity | Action required |
|---|---|---|---|---|---|
| <PR title> | reindex_auto | yes | no | n/a | automatic |
| <PR title> | reingest_selective | no | yes | `pipeline LIKE 'text.short@v1' AND ...` | `ctxt upgrade run` |

### reingest_selective details (per change)

- **Selector predicate**: SQL WHERE clause or pipeline-name filter
- **Estimated wall-clock**: typical / worst-case
- **Estimated LLM cost**: typical / worst-case (or "none" for pure-projection changes)
- **Versioned pipeline?**: yes/no — does this gate behind `@vN` or replace the pipeline contract?
```

The template lives at `docs/release-notes/template.md`. Per-release notes go to `docs/release-notes/YYYY-MM-DD.md`. The CI check (PR-time) parses the new release-notes file and fails the PR if any commit in the diff carries an operator-impact tag (in commit trailer, see below) without a matching table row.

**Commit trailer convention**:

```
Operator-Impact: reindex_auto | reingest_selective | reingest_all | none
```

`none` is the default and need not be stated explicitly. Anything else mandates the release-notes row.

### 5. Operator-facing CLI surface

- `ctxt upgrade status` — current state (`none`, `in-progress`, `awaiting-consent`, `failed`), per-bucket queues, progress bar, ETA, error log. Output is JSON-by-default with a human-readable renderer.
- `ctxt upgrade status --watch` — tail mode, refreshes on bus events.
- `ctxt upgrade plan` — what would `ctxt upgrade run` do? Shows selectivity predicates, object counts, cost estimates, dry-run summaries.
- `ctxt upgrade run [--dry-run] [--filter] [--rate-limit] [--all --i-understand-the-cost <sha>]` — execute pending bucket-2 work, or bucket-3 with the consent guard.
- **Banner injection**: every CLI command in every binary, when an upgrade is in flight, prepends a single-line banner to stderr: `ℹ ctxt: upgrading <bucket>; <done>/<total> objects (<pct>%); ETA <eta>; details: ctxt upgrade status`. The banner reads upgrade state from `/healthz` (or local file if dpkms isn't running) — no probe latency in the hot path.
- **`/healthz`** gains an `upgrade` envelope distinct from `health`: `{"health": "healthy", "upgrade": {"state": "in_progress", "bucket": "reingest_selective", "progress": 0.62, "eta_seconds": 47, "started_at": "..."}}`.
- **Search responses** include a `staleness_warning` field when the result set contains objects whose `pipeline_version` is stale: `{"results": [...], "staleness_warning": {"count": 12, "reason": "objects pending upgrade to text.short@v2; results may be incomplete"}}`.
- **Completion notification**: macOS `osascript` notification (or terminal bell + final summary on Linux) when an automatic upgrade finishes; sticky reminder on failure.

### 6. Test strategy

The framework is testable end-to-end without external dependencies by adopting three already-vetted hop.top tools:

- **`hop.top/xrr`** (already in use across `internal/adapter/{mic,feeds/rss,screen,files/s3}`, plus `test/integration/us0219_mcp_test.go`) — record cassettes for upgrade-time external calls. Specifically: LLM calls during `reingest_selective`, embedding-provider calls during `reingest_all` (per ADR-071), and dpkms HTTP responses to `/healthz` queries during banner-injection tests. Adopters use the existing `xrr/adapters/{http,exec}` adapters; cassettes live alongside other test fixtures under `test/integration/testdata/cassettes/`.
- **`hop.top/eva`** (new adoption) — contract-test the operator-facing JSON shapes:
  - `ctxt upgrade status` output schema (Tier-1 deterministic, JSON Schema).
  - `/healthz` upgrade envelope (Tier-1).
  - search response `staleness_warning` field (Tier-1).
  Contracts live at `contracts/upgrade-status.eva.yaml`, `contracts/healthz.eva.yaml`, `contracts/search-staleness.eva.yaml`. CI runs `eva run` against a recorded fixture set on every PR; shape regressions fail the build.
- **`hop.top/ben`** (new adoption) — recall benchmarks for `reingest_selective` PRs. The benchmark suite at `suites/recall-text-short.ben.yaml` defines a fixed corpus + query list + expected-recall-floor metric. PR diff that flips an object's `pipeline_version` triggers ben on the *post-upgrade* corpus; if recall regresses beyond a tolerance, the PR fails. This is the eval-harness precondition ADR-071 names for embedding-model swaps.

The xrr cassettes for upgrade flows are versioned with the pipeline they exercise — so a `text.short@v2` upgrade PR ships its own cassette set, and the cassette filenames embed the version. Cassette format follows `xrr/spec/cassette-format-v1.md`.

---

## Rationale

### Why three buckets, not two or four?

Two buckets (silent vs. consent) is too coarse: it bundles "tokenizer changed, daemon will rebuild in 30s" with "you're about to spend $1000 on LLM calls." Operators rightly treat those as different events.

Four buckets adds a `reingest_warn`-style middle category for "pipeline changed but cheap re-ingest, can run automatically with notification only." The team rejected this in design discussion: the cost cliff between bucket 2 (operator runs one CLI command, reads the plan) and bucket 3 (operator types a SHA256 acknowledgement) is steep enough that the middle ground muddies the mental model. If a change is cheap enough to run silently, it's probably bucket 1. If it's expensive enough to want consent, it's at least bucket 2.

### Why per-object pipeline_version, not a global "schema version"?

A global version forces everyone to the new contract on the next daemon start, which is exactly the bucket-3 behavior we're trying to avoid. Per-object versioning makes selectivity natural: only the objects whose pipeline actually changed get re-ingested. Federation peers (ADR-064) can also negotiate per-object compatibility instead of refusing to sync over a version gap.

### Why bake versioning into the pipeline name (`text.short@v2`), not a separate column?

Three reasons:

1. **Zero-migration** — existing rows with `text.short` (no `@`) parse as `@v0` by convention, so no DDL change is required to roll this out.
2. **Human-readable in `ctxt show`, ctxt-CLI debug output, log lines.** Operators see `text.short@v1` and immediately understand what's old.
3. **Pipeline registry maps name-with-version to step list directly** — no extra lookup, no coupling between the pipeline registry and a separate version table.

### Why an index *signature*, not a version number?

A signature is a hash of the actual schema + tokenizer + projection logic. Hard to fake, hard to forget to bump, and *automatically* changes when any input changes (because the hash inputs change). A version number is a hand-maintained integer that someone forgets to bump in a PR. The signature approach is the same one Postgres uses for catalog versions and the same one most database migration tools use to detect drift.

### Why xrr + eva + ben specifically, not generic test scaffolding?

Each tool solves a problem the others don't:

- **xrr** locks in *external interaction determinism* (HTTP/LLM/exec cassettes). Without it, every upgrade-flow test either burns real LLM credits or maintains a hand-rolled mock that drifts from production behavior.
- **eva** locks in *operator contract stability*. The `ctxt upgrade status` JSON shape is consumed by humans, by `osascript` for notifications, and (eventually) by web dashboards. eva contracts catch shape regressions before they reach an operator's terminal.
- **ben** locks in *outcome verification*. A `reingest_selective` PR that claims "this fixes recall for bullet-list objects" must demonstrate it on a fixed corpus before merge — otherwise we're shipping faith-based upgrades and racking up bucket-2 events for nothing.

All three are already vetted; xrr is in production use already. The marginal cost is two new YAML files per PR (eva contract + ben suite) when the change is operator-impacting.

### Alternatives considered

- **"Just bump a global migration version, force operators to opt in on each release"**: rejected — exactly the bucket-3-as-default failure mode.
- **"Ship a `migrate.sh` script per release"**: rejected — works for stateful ops tooling (Rails, Django) but doesn't fit a query-system that needs to keep serving during upgrade. Also indistinguishable from "no upgrade plan at all" in the failure modes the user is rightly pushing back on.
- **"Add a single `corpus_pending_upgrade` boolean and re-ingest everything"**: rejected — collapses bucket 2 into bucket 3, which is the failure mode T-0565 already manifested.
- **"Defer all of this until the operator complains again"**: rejected — the operator already complained twice in 72 hours. The cost of this framework is one ADR + one set of CLI commands; the cost of *not* having it is every future bug-fix landing as a bucket-2 release by default.

---

## Consequences

### Positive

- Operators can answer "what is dpkms doing?" at any moment without reading source.
- `reingest_selective` PRs are gated on demonstrated recall improvement (ben), not vibes.
- The daemon can refuse to do something dangerous (bucket 3) without a clear consent token — preventing accidental `ctxt upgrade run` from costing thousands of dollars.
- Test determinism: upgrade-flow tests run in CI without LLM credits, in <1 second, against recorded cassettes.
- Federation (ADR-064) gains a clean per-object compatibility check.

### Negative

- **Two new dependencies** (eva, ben) for the test substrate. xrr is already in. Each adds CI complexity and a learning curve for contributors.
- **Pipeline registry surface area grows**: every breaking change to a step adds a new versioned entry. This is the cost of additive-by-default; over years the registry accretes versions. We accept this; orphan-version cleanup is a future ADR if it becomes a real problem.
- **Release-notes friction**: contributors who land bug-fixes touching the pipeline must now write a release-notes row. The PR template makes this mechanical, but it's still a step.
- **Per-CLI-command banner**: every `ctxt`/`dpkms` invocation now reads upgrade state. We mitigate by reading a local-file shadow of `/healthz` (refreshed by the daemon, polled by the CLI) so there's no per-command HTTP round-trip in the hot path.

### Neutral or considerations

- ADR-064 federation peers will need to advertise their pipeline-version vector in the manifest; backwards-compatibility within federation is a separate ADR if it becomes a problem.
- The `osascript` notification is macOS-only; Linux uses terminal bell + summary. Windows is out of scope until ctxt has a real Windows operator base.
- The `/healthz` upgrade envelope is a public contract — third-party monitors will parse it. Once shipped, the field shape is locked by an eva contract.
- Eventual consistency: there's a window between "daemon detects mismatch" and "first object is migrated" where search results may be incomplete for affected objects. The `staleness_warning` field exists precisely so callers know they're in this window.

---

## Implementation Notes

This ADR is the framework. Implementation landed across the following PRs/tasks (all on 2026-05-07):

### Phase 1 — Storage & detection foundations (T-0579, **shipped**)

- `index_signatures` table (migration 029); signature compute + verify on daemon startup; mismatch emits `dpkms.upgrade.signature.mismatched` bus event.
- `objects.pipeline` value-format convention (`name@vN`); migration 030 stamps existing rows as `@v0`. `pipeline.ParseVersionedName` + registry lookup for `@v0` fallback.
- Tests: in-package unit tests for signature compute + version parsing.
- xrr cassettes were optional and skipped — the signature-verify path is local SQLite only; the rebuild worker (Phase 3) is the right cassette layer.

### Phase 2 — Upgrade CLI surface (T-0580, **shipped**)

- `ctxt upgrade {status,plan,run}` commands; `ctxt upgrade status --watch` + `--interval`.
- `/healthz` upgrade envelope (extends T-0564's healthcheck): top-level `health` flips to `"upgrading"` when an upgrade is in flight; new `upgrade` field carries `{state, bucket, progress, done, total, eta_seconds, started_at}`.
- `internal/upgrade/` package owns the state machine + JSON-shadowed disk file at `$XDG_DATA_HOME/contexthelp/run/upgrade-state.json` so the banner reads from disk (no per-command HTTP latency).
- `internal/cli/banner/` middleware injected into both `cmd/ctxt/cmd/root.go` and `cmd/dpkms/cmd/root.go` `Hooks.PrePersistentRunE`.
- Tests: 15 in-package unit tests covering the state machine, banner format, healthz envelope.
- eva contracts and xrr cassettes deferred to T-0586/T-0587 (the eva/ben adoption tasks).

### Phase 3 — Selective re-ingest engine (T-0581, **shipped**)

- `internal/upgrade/selector.go`: `Selector` parses `pipeline=<name>@vN` (most common) or `where:<sql-where-clause>` (escape hatch with strict regexp validation against DDL/DML/semicolons/comments). `CountMatching` and `IterateMatching` against the SQLite store.
- `internal/upgrade/worker.go`: `Worker.Run` acquires the upgrade-state lock, iterates the selector, calls `service.ReanalyzeObject` per ID, ticks progress, honors `--rate-limit`, tracks running LLM cost against `--budget-usd`. Emits 5 bus events (`plan.computed`, `reingest.{started,progressed,completed,failed}`).
- `internal/service/reanalyze.go`: new `Service.ReanalyzeObject(ctx, id) (newVersion, llmCostUSD, error)` method.
- `internal/service/staleness.go`: `CurrentVersionForFamily(reg, name)` walks the registry returning `max(@vN)`; used by both staleness-warning population and `ctxt upgrade plan`.
- `staleness_warning` field on `SearchDiagnostics` (extended from T-0574's diagnostics block) populated in `HybridSearch` by counting candidates whose `pipeline` parses to a version older than the registry's current.
- Tests: 23 cases across selector + worker + service.
- xrr cassettes skipped for now — `Service.Analyze`'s test infrastructure didn't admit cassette-replay cleanly at the worker layer; worker tests use a `fakeReanalyzer` interface. Cassette work is queued behind T-0586/T-0587.

### Phase 4 — Release-notes scaffolding (T-0577, **shipped**)

- `docs/release-notes/template.md` — fillable template.
- `docs/release-notes/2026-05-07.md` — retroactive + ongoing notes for the 2026-05-07 cohort.
- `.github/PULL_REQUEST_TEMPLATE.md` — checkbox + bucket prompt.
- `.github/workflows/release-notes-check.yml` — CI gate that reads commit trailers and fails on missing release-notes rows.

> **Process note (2026-05-07)**: the CI gate enforces the contract on PRs, not on direct merges to main. T-0579/T-0580/T-0581 were ff-merged from worktree branches without going through PR review. The release-notes rows for those commits were authored by hand at merge time. Future work that lands via PR will trigger the gate automatically.

### Backward compatibility

- Existing rows with `pipeline = 'text.short'` (no `@vN`) are interpreted as `@v0` by the registry. The registry holds a `@v0` entry for every pre-versioning pipeline that mirrors the pre-T-0565 step list. New ingests use whatever the current default is (likely `@v1` post-T-0565).
- No data migration runs at the schema level. The next time a `reingest_selective` upgrade fires for `text.short`, only objects with `@v0` (or any pre-`@v_current` value) are picked up.

### Out of scope (deferred to future ADRs)

- ADR-071 covers embedding-model versioning specifically (multi-model dual-write, graceful migration).
- Federation per-object compatibility negotiation: future ADR if cross-instance upgrade-version mismatch becomes a real problem.
- Multi-tenant pipeline-version selection: out of scope until a real multi-tenant deployment exists.
- Roll-back of a failed re-ingest: out of scope; failures stay in the `failed` state and the operator decides whether to retry, skip, or revert via DB-level intervention. Roll-back automation is deferred to when we have a real failed-upgrade incident to design against.

---

## References

- T-0565 — search recall regression that prompted this work
- T-0576 — config regression that compounded operator pain
- T-0577 — task this ADR closes
- T-0578 / ADR-071 — embedding-index versioning, builds on this framework
- ADR-004 — step-based pipeline; this ADR adds versioning to step compositions
- ADR-061 — search quality tiers; this ADR adds the `staleness_warning` honesty layer
- ADR-064 — federation; per-object pipeline_version is the compat token
- `hop.top/xrr` — cassette format for upgrade-flow test determinism
- `hop.top/eva` — operator-contract enforcement for `ctxt upgrade status`, `/healthz`, `staleness_warning`
- `hop.top/ben` — recall benchmarks gating `reingest_selective` PRs
- User feedback memory: `feedback_ctxt_upgrade_reingest.md` (ops side, recorded 2026-05-07)
