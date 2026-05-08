---
title: Lateral Capture Discovery
date: 2026-05-08
status: design
related:
  - tracks/github-repo-ingestion-20260404 (GRIP, RCL)
  - docs/stories/capture/US-0202-github-capture.md
  - docs/manual/workflows/entity-auto-enrichment.md
  - docs/architecture/ambient-capture.md
---

# Lateral Capture Discovery

## Problem

Capturing `https://github.com/samber/lo` should not only ingest that single
repo. The capture is a moment when adjacent intel is also cheaply within reach:
sibling repos under the same owner that match what the user is currently
working on, the owner's profile and sponsor page as inspirational anchors,
pinned and starred lists as curation signals.

Today this lateral signal is left on the floor. The user finds it manually or
not at all. Adding it to capture-time, gated by relevance, avoids the noise of
"here are all 200 samber repos" while still surfacing the handful that matter
for the user's active context.

This design describes a generic, source-pluggable lateral discovery layer that
runs alongside (not inside) capture pipelines, materializes probationary
entities under a TTL-bound lifecycle, and promotes them to first-class only on
explicit or implicit user signal.

## Goals

- Surface relevant adjacent entities at capture-time, ambiently, without a
  separate verb the user must remember.
- Gate by active context (live work session, capture window, explicit
  interests) so output is targeted, not exhaustive.
- Materialize discoveries as probationary entities with TTL, not as canonicals.
- Promote to canonical only on explicit re-capture, explicit promote command,
  or accumulated reference signal.
- Dedupe against existing canonicals; never shadow what already exists.
- Stay generic across capture sources; v1 ships GitHub only but the architecture
  serves future sources without rework.

## Non-goals

- Not a full embedding pipeline; reuses existing eva for scoring.
- Not LLM-based candidate ranking; LLM does triage and recipe authoring only.
- Not collaborative filtering; single-user graph.
- Not federated multi-instance queue coordination in v1.
- Not a web UI; CLI only.

## Architecture

### Boundaries

```
   ctxt capture <url>
        |
        v
  capture pipeline -- emits --> ctxt.ingest.object.persisted (signal-only)
  (code.github.repo, .pr, .issue, .profile)               or .captured (fallback)
                                              |
                                              v
                                    lateral.discover pipeline
                                              |
                                              v
                                    LateralStrategy registry
                                    +-- GitHubOwnerStrategy
                                    +-- (future) XOwnerStrategy
                                    +-- (future) LinkedInStrategy
                                              |
                                              v
                                  pre-scan triage (LLM, cached)
                                              |
                                              v
                                  candidate generation per sub-path
                                  (sub-paths fail independently)
                                              |
                                              v
                                  post-scan distillation (LLM)
                                              |
                                              v
                                  identity resolution (canonical?)
                                              |
                                              v
                                  eva blended scoring
                                              |
                                              v
                                  cap gate (top-K AND threshold T)
                                              |
                                              v
                                  +---------------------+
                                  | canonical exists?   |
                                  |  -> edge only       |
                                  | no canonical?       |
                                  |  -> probationary    |
                                  |    lateral_candidate|
                                  +---------------------+
                                              |
                                              v
                                  emits ctxt.lateral.candidate.*
                                              |
                                              v
                                  dpkms storage
                                              ^
                                              |
                                  ttl reaper (hourly cron)
                                              ^
                                              |
                          ctxt show suggestions <parent-id>
                          ctxt promote <candidate-id>     (explicit P2)
                          ctxt capture <url>              (implicit P2)
                          M references accumulated        (P3)
                          ctxt reject <candidate-id>      (hard negative)
```

### Package layout

- `internal/lateral/` — pipeline, strategy interface, scorer, resolver, cap
  gate, materializer. No source-specific code.
- `internal/lateral/strategies/github/` — `GitHubOwnerStrategy`. Knows GitHub
  owner-siblings, pinned, starred, sponsor page sub-paths.
- `internal/lateral/scoring/` — eva-blended scorer; signal redistribution;
  weight overrides; auditable score metadata.
- `internal/lateral/lifecycle/` — state machine; TTL math; sanity bounds;
  promotion/rejection handlers.
- `internal/lateral/identity/` — resolver; ambiguous-match handling;
  edge-only vs probationary decision.
- `internal/lateral/queue/` — `DeferredQueueAdapter` interface.
- `internal/lateral/queue/adapters/{sqlite,redis,memory}/` — adapters.
- `internal/lateral/triage/` — LLM pre/post-scan calls; recipe cache; recipe
  staleness handling.
- `internal/lateral/ttl/` — reaper goroutine, size-adaptive checkpointing,
  mutex-protected cycles.
- `internal/lateral/context/` — `ActiveContext` resolver pulling session,
  capture window, interest registry.

### Strategy dispatch

Each `LateralStrategy` exposes `Applies(event) bool`. The registry iterates and
calls every strategy whose predicate matches. Multiple strategies may match for
one event; their results merge before the cap gate. No global route table.

Within a strategy, sub-path selection is internal: driven by parent-object
shape, adapter cost, and active-context shape. The strategy can pre-filter
expensive sub-paths whose candidates obviously will not survive the threshold
T given current active context.

### Bus events emitted

- `ctxt.lateral.scan.started`
- `ctxt.lateral.scan.completed`
- `ctxt.lateral.scan.deferred` (with reason: `rate_limit`, `resolver`,
  `cold_start`, `scoring`)
- `ctxt.lateral.scan.skipped` (with reason: `cold_start`,
  `rate_limit_persistent`, `triage_negative`, `no_strategy`)
- `ctxt.lateral.subpath.failed` (with sub-path id, reason)
- `ctxt.lateral.candidate.created`
- `ctxt.lateral.candidate.promoted` (with promotion path: `p2_explicit`,
  `p2_implicit`, `p3_references`)
- `ctxt.lateral.candidate.expired` (with reason: `parent_expired_no_links`,
  `cold_cycle`, `superseded_by_canonical`)
- `ctxt.lateral.candidate.rejected`
- `ctxt.lateral.candidate.resurrected`
- `ctxt.lateral.reaper.cycle.started`
- `ctxt.lateral.reaper.cycle.completed`
- `ctxt.lateral.reaper.cycle.failed`
- `ctxt.lateral.reaper.cycle.skipped_overlap`
- `ctxt.lateral.reaper.sanity_violation`
- `ctxt.lateral.scoring.invalid_score`
- `ctxt.lateral.scoring.signal_degraded`
- `ctxt.lateral.resolver.ambiguous`
- `ctxt.lateral.recipe.refreshed`

## Data model

### `lateral_candidate` namespace

```json
{
  "id": "o-lc-7g8h9i",
  "namespace": "lateral_candidate",
  "title": "samber/mo",
  "source": "https://github.com/samber/mo",
  "metadata": {
    "kind": "lateral_candidate",
    "candidate_type": "sibling_repo",
    "discovered_by": "o-gh-repo-4d5e6f",
    "strategy": "github.owner",
    "lifecycle": {
      "state": "probationary",
      "discovered_at": "2026-05-08T14:22:00Z",
      "parent_lifespan_inherited": "180d",
      "cold_since": null,
      "expires_at": "2026-11-04T14:22:00Z",
      "promotion_signals": []
    },
    "scoring": {
      "score": 0.78,
      "signals_used": ["session_topic", "capture_window"],
      "weights_applied": {
        "session_topic": 0.625,
        "capture_window": 0.375
      },
      "degraded_reason": null,
      "active_context_snapshot": "session:s-2026-05-08-go-cli"
    },
    "preview": {
      "description": "Monads and popular FP abstractions for Go",
      "stars": 2400,
      "language": "Go",
      "topics": ["go", "monad", "functional-programming"]
    }
  }
}
```

`candidate_type` values: `sibling_repo`, `owner_profile`, `sponsor_page`,
`pinned_repo`, `starred_repo`.

`preview` is lightweight scan-time metadata; never used as source of truth
post-promotion (full canonical capture supersedes it).

### Edges

- `discovered_by` — candidate to parent; always present; survives promotion as
  historical attribution.
- `refers_to_canonical` — candidate to canonical when identity resolution
  matches; only on edge-only path.
- `promoted_from` — only added at promotion time; from new canonical to the
  retained probationary record.
- Reference edges to expired records are retained for the full 30d soft-delete
  window; only at hard-purge are both record and inbound reference edges
  removed.

### Lifecycle states

```
            +-------------+
            | probationary|  <-- created by lateral.discover, cap gate passed
            +------+------+
                   |
       +-----------+---------------+
       |           |               |
       v           v               v
   expired   promoted (P2)   promoted (P3)
   (TTL)     re-captured     M-references
                   ^               ^
                   |               |
                   +------+--------+
                          |
                  resurrected from
                  state: expired
                  (within 30d window)
```

### TTL math

- `expires_at = discovered_at + parent.lifespan` (parent lifespan from source
  class registry; GitHub repo default 180d).
- Refresh on parent re-capture: all probationary children get `expires_at`
  recomputed against new parent timestamp.
- Independent staleness: `cold_cycle_days` (default 30); if `cold_since` is
  older than threshold, expire regardless of parent state.
- Reaper cadence: hourly batch, transactional, idempotent.

### Identity resolution (pre-materialization)

1. Exact-URL match against canonical `source` field — edge-only.
2. Identity-key match (e.g. `@github.user.<login>` to canonical via aliases) —
   edge-only.
3. Ambiguous match (multiple canonical candidates) — pick highest-scoring above
   confidence threshold (0.7); else emit `lateral.resolver.ambiguous` and fall
   to probationary.
4. No match — materialize as probationary `lateral_candidate`.

Edge-only path applies to all candidate types, not just owner profile and
sponsor page; sibling repo whose owner is canonical edges to the canonical
owner; sponsor page whose sponsoree is canonical edges to that canonical.

## Active-context blended scoring

### Inputs

1. **Live work-session topic** (US-0216 derived topic vector + tags). Null if
   no session live.
2. **Rolling capture-window aggregate** (last 30d default; tags, languages,
   topics, namespaces). Cached 1h. Always present once captures exist.
3. **Explicit interest registry** (`@interest.*` entries). Optional. If user
   activates one (`ctxt interest activate <id>`), only that contributes; else
   all defined interests contribute with own weights.

### Blending

Per-candidate score 0.0-1.0 = weighted sum of three sub-scores. Each sub-score
is similarity (cosine on tag/topic vectors + lexical overlap on description and
name) against the signal's representation.

Default weights:

```yaml
session_topic: 0.5
capture_window: 0.3
interest_registry: 0.2
```

Missing-signal redistribution: if a signal is null or errored, its weight is
proportionally redistributed across surviving signals.

Per-candidate-type overrides allowed (e.g. `starred_repo` weights
`interest_registry` higher than `session_topic` because a star is a
long-horizon signal, not a today signal).

### Cap gate

Top-K AND threshold-T, both apply. Defaults:

```yaml
threshold:
  sibling_repo: 0.45
  pinned_repo: 0.40
  starred_repo: 0.55
  owner_profile: 0.0   # singleton; T bypassed
  sponsor_page: 0.0    # singleton; T bypassed
cap_k:
  sibling_repo: 5
  pinned_repo: 3
  starred_repo: 3
```

Owner profile and sponsor page are singletons per owner; bypass K, gated only
by T (threshold 0.0 = always-or-never depending on edge-only resolution).

### Auditability

Every probationary candidate's `metadata.scoring` records: final score,
signals used, weights applied (after redistribution), snapshot ID, degraded
reason if any. `ctxt show suggestions --explain` prints this block.

### Cold-cycle interaction

`cold_since` advances only if no scoring event touches the candidate.
Re-running scan against a refreshed active context re-scores existing
probationary children of any parent in scope and resets `cold_since` for
survivors.

## LLM role

LLM is used for judgment, not enumeration:

1. **Pre-scan triage** — coarse (dispatcher level: "does this object warrant
   any lateral scan?") + fine (strategy level: "given my sub-paths, would any
   plausibly produce useful candidates given current active context?"). Cached
   by `(source_page_type, active_context_fingerprint)`.

2. **Post-scan distillation** — filters raw strategy output before cap gate.
   Keeps the relevant subset; rejects obvious non-candidates.

3. **Recipe authoring** — when LLM identifies which DOM regions / fields carry
   the relevant data for a source, the selection is saved as an ibr recipe,
   keyed by `(source_domain, page_type)`. Reused on subsequent captures from
   the same source-page-type without re-asking the LLM.

Recipes refresh only on failure (no time-based TTL). Failure detection:
extraction returns empty result, or "structurally suspicious" output —
cardinality dropped >80% from baseline, expected fields missing, etc.

Tier A (predefined sub-paths for structured sources like GitHub) only in v1.
Tier B (JIT LLM-proposed sub-paths for long-tail sources) is out of scope for
v1.

## Failure handling

### Mode 1 — GitHub API rate limit

Dynamic floor driven by trailing-4h actual API call rate from the host
client's counter:

```
captures_per_hr = avg(API calls in last 4h) / cost_per_capture_estimate
budget_for_user = captures_per_hr * cost_per_capture * (time_until_reset / 3600)
floor_pct = clamp(budget_for_user / 5000, 0.05, 0.90)
soft_floor = 5000 * floor_pct
```

Below floor: defer scan to persistent queue, retry once after rate-limit reset
window; if still over budget on retry, drop with
`lateral.skipped.rate_limit_persistent`. No hard floor (clamp prevents
floor below 5%).

### Mode 2 — kit/ibr failures

- Sub-path failure isolation: hard. Each sub-path is an independent goroutine.
- Transient retry: once with 5s backoff, only on plausibly-transient errors
  (timeout, network, 5xx); not on 403 / anti-bot / empty-from-stable-selector.
- No circuit breaker; ibr self-heals via recipe staleness.
- Sub-path failures are sub-events on `lateral.scan.completed`, not a distinct
  partial state.
- LLM triage: coarse + fine; cached by `(source_page_type,
  active_context_fingerprint)`.
- Recipe scope: domain + page-type.
- Recipe lifecycle: failure-driven only; no time-based TTL.

### Mode 3 — identity-resolver failures

- Resolver unavailable: defer scan to persistent queue (resolver is
  load-bearing).
- Slow resolver: soft 2s timeout, retry once with 5s timeout; beyond that,
  treat as unavailable.
- Ambiguous match: high-confidence (>0.7) match wins via eva blend; truly
  ambiguous emits `lateral.resolver.ambiguous` and falls to probationary.
- Cold-start (alias registry not loaded): defer until ready.

### Mode 4 — reaper failures

- Mid-cycle crash: A+B size-adaptive. Below 10k records, idempotent sweep
  restarts from beginning. At/above 10k, checkpoint every 1k records;
  resume from checkpoint.
- Reaper-not-running: heartbeat events + ops-layer health check (alert if no
  heartbeat for >2 cycles).
- Overlapping cycles: mutex; new cycle skips with
  `lateral.reaper.cycle.skipped_overlap` if previous still holds.
- TTL math correctness: sanity bounds (refuse to expire records with
  `expires_at` >30d in future or older than `discovered_at`) + soft-delete
  quarantine (mark `state: expired`, hard-purge after 30d via separate
  process).
- Promotion vs reaper race: per-record advisory lock; promotion can flip
  `state: expired` to `promoted` within 30d soft-delete window. Both P2 and P3
  resurrect.

### Mode 5 — scoring failures

- eva down: defer scan to persistent queue (same shape as mode 3 resolver).
- Invalid score per candidate: treat as 0.0; candidate fails cap gate;
  dropped; logged as `lateral.scoring.invalid_score`.
- Slow eva: batch all candidates into single eva call where supported, with
  soft 5s timeout and retry-once at 10s; per-candidate fallback if batch
  unsupported (same retry shape).
- Active-context signal degraded: treat errored signal as null;
  redistribution handles it; logged as `lateral.scoring.signal_degraded`.
- Score auditability when degraded: record `signals_used` accurately +
  `degraded_reason` field for `--explain` output.

### Mode 6 — bus event ordering and cold-start

- 6A — parent not yet readable: prefer `ctxt.ingest.object.persisted`
  (signal-only event with ID + namespace; lateral re-reads from canonical
  store). Fall back to `ctxt.ingest.object.captured` + read-with-retry
  (50ms, 200ms, 1s, 3s; 5s total) for pipelines that don't emit `.persisted`.
- 6B — cold-start (all three signals null): skip with
  `lateral.skipped.cold_start`; ambient retry on next capture.
- 6B definition: cold-start = all three signals null; thin-but-warm produces
  low scores naturally and falls out of cap gate.
- Incomplete parent record: each strategy declares preconditions on a
  `LateralStrategy` interface method; refuses to run if missing.

## CLI surface

### `ctxt capture <url>` (modified)

No signature change. Response includes `lateral` block:

```json
{
  "job_id": "j-gh-repo-1a2b3c",
  "object_id": "o-gh-repo-4d5e6f",
  "lateral": {
    "scheduled": true,
    "strategy": "github.owner",
    "expected_completion": "~30s"
  }
}
```

If skipped: `lateral.scheduled: false` with `skip_reason`.

`--lateral-verbose` flag prints lateral progress and failures inline (default
off).

### `ctxt show suggestions <parent-id> [--explain] [--all]`

Per-parent listing of probationary candidates. `--explain` adds scoring
breakdown. `--all` shows below-threshold candidates (not materialized, visible
for tuning).

### `ctxt lateral list`

Graph-wide listing of probationary candidates, paginated by score-descending.

### `ctxt promote <candidate-id>`

Explicit P2 promotion. Re-namespaces, kicks canonical capture pipeline,
preserves `discovered_by` edge.

### `ctxt reject <candidate-id>`

Hard-negative label. Marks candidate `state: expired` immediately. Records
rejection in source-pattern statistics for `suggest-weights` fit.

### `ctxt lateral stats [--since <duration>] [--strategy <id>]`

Tier 2 tuning surface. Descriptive statistics over labeled outcomes:
discovery volume, outcomes (promoted P2/P3, expired cold/parent,
probationary), score-bucket promotion rates, signal lift, source-class skew,
partial/failure breakdown.

### `ctxt lateral suggest-weights [--since <duration>] [--strategy <id>] [--apply | --diff]`

Tier 3-B. Constrained least-squares fit over labeled outcomes (weights in
[0,1], sum to 1, optimize rank correlation between score and promotion
outcome). Loss weights hard negatives (`reject`) 3x soft negatives
(cold-expired). Refuses fits with <20 promotions; emits clear "need more
data" message instead.

### `ctxt lateral summary`

24h operational view (default). "What happened recently": scan counts,
failure reasons, promotions, things-pending-review. Distinct from `stats`
(which is monthly tuning).

### `ctxt lateral show-config <strategy-id>`

Prints merged-effective config for a strategy after deep-merge of global
defaults and per-strategy overrides.

## Configuration

```yaml
lateral:
  strategies:
    github.owner:
      enabled: true
      # any other lateral.* block can be overridden per strategy via deep-merge

  scoring:
    weights:
      session_topic: 0.5
      capture_window: 0.3
      interest_registry: 0.2
    capture_window_days: 30
    overrides:
      starred_repo:
        session_topic: 0.2
        capture_window: 0.3
        interest_registry: 0.5
    threshold:
      sibling_repo: 0.45
      pinned_repo: 0.40
      starred_repo: 0.55
      owner_profile: 0.0
      sponsor_page: 0.0
    cap_k:
      sibling_repo: 5
      pinned_repo: 3
      starred_repo: 3

  lifecycle:
    cold_cycle_days: 30
    soft_delete_days: 30
    p3_reference_threshold: 3

  rate_limit:
    avg_capture_window_hours: 4
    floor_pct_min: 0.05
    floor_pct_max: 0.90
    cost_per_capture_estimate: 4

  reaper:
    interval: 1h
    checkpoint_threshold_records: 10000
    checkpoint_interval_records: 1000
    promotion_grace_window: 60s

  resolver:
    timeout_soft: 2s
    timeout_retry: 5s

  scoring_service:
    timeout_soft: 5s
    timeout_retry: 10s

  deferred_queue:
    adapter: sqlite             # sqlite | redis | memory
    sqlite:
      path: ~/.config/ctxt/lateral_queue.db
    redis:
      url: ""
      key_prefix: ctxt:lateral:queue
    retention: 24h
    persistent: true

  triage:
    cache_by:
      - source_page_type
      - active_context_fingerprint

  recipes:
    refresh_on_failure: true
    structurally_suspicious_thresholds:
      cardinality_drop_pct: 80
      missing_required_fields: 1

  capture_integration:
    persisted_event_timeout: 5s
    read_retry_schedule: [50ms, 200ms, 1s, 3s]
```

### Per-strategy overrides

Any block can be overridden per strategy via deep-merge (strategy values win
over global; missing strategy values fall through to global defaults).

### Runtime-mutable subset

SIGHUP reloads the runtime-mutable subset:

- `enabled` flags per strategy
- `scoring.weights`
- `scoring.threshold`
- `scoring.cap_k`
- `lifecycle.cold_cycle_days`
- `triage.cache_by`

Static (restart required):

- `deferred_queue.adapter` (changing mid-flight loses in-flight events)
- `capture_integration.*` (race risks)
- `reaper.checkpoint_threshold_records` (changes mid-cycle confuse
  size-adaptive logic)

## Test strategy

### 1. Unit tests

- `internal/lateral/scoring/`: blender math, signal redistribution, weight
  overrides, missing-signal handling, NaN handling.
- `internal/lateral/lifecycle/`: state machine transitions, TTL math, sanity
  bounds.
- `internal/lateral/identity/`: resolver dispatch, ambiguous-match handling,
  edge-only vs probationary decision.
- `internal/lateral/queue/`: per-adapter contract tests; same suite runs
  against SQLite, Redis, memory.
- `internal/lateral/strategies/github/`: candidate generation given mocked
  GitHub API responses, sub-path independence, precondition declaration.

### 2. Cassette tests (xrr)

- `GitHubOwnerStrategy.Probe()` against fixture cassettes (popular owner with
  sponsor page; owner without; rate-limit-near-floor; not-found; malformed
  pinned-list).
- ibr extraction against fixture HTML (clean sponsor page; structurally
  changed; empty result).
- Recipe authoring/refresh: LLM proposal cassette; cached recipe hit;
  recipe-staleness recovery cycle.

### 3. Integration tests

Real ctxt process, real local dpkms test instance, real SQLite queue, mocked
GitHub via httptest server.

- End-to-end: capture → lateral schedules → strategy runs → eva scores → cap
  gate → probationary OR edge-only. Verify graph state.
- Lifecycle: capture → cold-expire (accelerated TTL via test config) →
  soft-delete → P2 promote within window → resurrection → hard-purge after
  window.
- Failure modes: simulate eva down → defer to queue → eva back → scan
  completes. Same shape for resolver-down, rate-limit-floor, ibr-down.
- Reaper: seed many probationary records → cycle → verify expirations and
  edge-only collapses. Both small (no checkpointing) and large (checkpointing)
  datasets.

### 4. Property-based tests

- Promotion is idempotent.
- Reaper is idempotent.
- Identity resolver never duplicates canonicals.
- Cap gate is monotonic in K.
- Score is reproducible given same candidate and active-context snapshot.

### 5. Live-traffic eva tests (pre-merge gate)

Run `ctxt lateral stats` after integration suite. Assert promotion-rate-by-
score-bucket is monotonically increasing (high scores → higher promotion
rates). Sanity check that scoring is producing useful ranking, not noise.

## Acceptance criteria

- [ ] `ctxt capture https://github.com/<owner>/<repo>` triggers lateral scan
      asynchronously. Response includes `lateral.scheduled` field.
- [ ] `lateral.discover` subscribes to `ctxt.ingest.object.persisted`
      (preferred), falls back to `ctxt.ingest.object.captured` +
      read-with-retry.
- [ ] `GitHubOwnerStrategy` enumerates owner siblings (API), pinned (API),
      starred head (API, gated by active context), sponsor page (ibr).
- [ ] Sub-paths fail independently; one failing does not affect siblings.
- [ ] LLM pre-scan triage runs (coarse + fine); decisions cached by
      `(source_page_type, active_context_fingerprint)`.
- [ ] LLM post-scan distillation filters raw candidates before cap gate.
- [ ] ibr recipes authored on first capture, persisted, refreshed on
      extraction failure (no time-based TTL).
- [ ] Identity resolver: exact-URL → identity-key → ambiguous handling →
      probationary fallback.
- [ ] eva-blended scoring with three signals, missing-signal redistribution,
      per-source-class overrides.
- [ ] Cap gate: top-K AND threshold-T; singletons (owner profile, sponsor
      page) bypass K.
- [ ] Probationary entities materialized with full Section 2 schema;
      canonical-resolved candidates create `refers_to_canonical` edges only.
- [ ] Reaper hourly, idempotent, mutex-protected, size-adaptive checkpointing
      at 10k threshold, sanity-bounded, soft-delete + 30d hard-purge.
- [ ] Promotion: explicit (`ctxt promote`), implicit P2 (`ctxt capture
      <same-url>`), and P3 (M=3 references) converge on same handler. P2 and
      P3 both resurrect within 30d soft-delete window.
- [ ] `ctxt show suggestions <parent-id> [--explain] [--all]` lists
      per-parent.
- [ ] `ctxt lateral list` lists graph-wide.
- [ ] `ctxt lateral stats [--since] [--strategy]` reports descriptive
      statistics.
- [ ] `ctxt lateral suggest-weights [--since] [--strategy] [--apply |
      --diff]` fits weights via constrained least-squares; refuses fits with
      <20 promotions.
- [ ] `ctxt lateral summary` shows 24h operational view.
- [ ] `ctxt reject <candidate-id>` produces hard-negative label.
- [ ] Deferred-event queue: persistent SQLite default, Redis adapter
      available, memory adapter for tests; 24h retention; at-least-once
      semantics.
- [ ] Rate-limit dynamic floor: capture-rate-driven, [5%, 90%] clamp, 4h
      trailing window, GitHub API call counter as input.
- [ ] Per-strategy override of all blocks via deep-merge; `ctxt lateral
      show-config <strategy-id>` prints effective merged.
- [ ] SIGHUP reloads runtime-mutable subset.
- [ ] All bus events emitted per Architecture section with structured
      reasons.

## Out of scope for v1

- Strategies beyond GitHub (X/Twitter, LinkedIn, arXiv, Wikipedia). Architecture
  supports them; each is its own US.
- JIT (LLM-proposed) strategies for unstructured sources (Tier B).
- Online weight learning. Manual `suggest-weights` only.
- Cross-user / federated lateral signals.
- `--lateral-supplement` flag on Tier-A strategies. Tier A stays sealed.
- Lateral on capture sources other than `code.github.*`.
- Multi-machine federated queue coordination (Redis adapter exists but
  inter-instance coordination is out of scope).
- Web UI / dashboard for suggestions.

## Documentation deliverables

- ADR documenting `lateral.discover` pipeline, `LateralStrategy` interface,
  JIT-deferred decision.
- `docs/manual/workflows/lateral-discovery.md` — workflow chapter (capture,
  suggestions, review, promote/reject, tuning).
- `docs/architecture/lateral-discovery.md` — engineer-facing architecture
  reference.
- Schema additions in `schemas/` for `lateral_candidate`, new edge types,
  new bus events.
- Update to `docs/manual/workflows/entity-auto-enrichment.md` cross-linking
  to lateral-discovery (lateral feeds the same probationary-then-promoted
  lifecycle the workflow already discusses).
- Operator-Impact trailer + matching row in `docs/release-notes/<date>.md`.
