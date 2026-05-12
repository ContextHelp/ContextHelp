---
title: Lateral Capture Discovery — P1 Substrate Completion Summary
date: 2026-05-09
status: shipped
related:
  - 2026-05-08-lateral-capture-discovery-design.md
  - 2026-05-08-lateral-capture-discovery-design-amendment-1.md
---

# Lateral Capture Discovery — P1 Substrate Completion

## Status

**P1 substrate complete.** 28/28 planned tasks shipped (track
`lateral-substrate-20260508`). 13 lateral packages, 66 tests passing,
gofumpt + go vet clean, full repo builds.

The substrate is ready for the strategy-roster work in P2-P5 (JIT
strategy, GitHub strategy + 9 other platforms, 11 shape strategies,
research-intent layer) and the CLI surface in P6.

## What shipped

### Core abstractions (T01-T05)
- `lateral.LateralStrategy` interface, `Family` enum, `Candidate`/`CapturedEvent`/`ActiveContext`/`AppliesResult` types.
- `lateral.Registry` with specificity-aware dispatch (Platform: highest-specificity wins; Shape: all fire; JIT: only when no Tier-A claims).
- `lateral.Discover` pipeline subscribing to `ctxt.ingest.object.persisted` + `.captured` via kit `runtime/bus`, with read-with-retry against the canonical store using `kit/core/util.Retry`.
- `activecontext.Resolver` with stable fingerprints via `kit/core/util.Short`.

### Scoring + Identity + Cap gate (T06-T08)
- `scoring.Scorer` — eva-blended (3 signals: session topic 0.5, capture window 0.3, interest registry 0.2) with missing-signal redistribution.
- `identity.Resolver` — URL exact-match → identity-key → probationary fallback. Aliases `kit/runtime/domain.ErrNotFound`.
- `capgate.Gate` — top-K + threshold per type, singleton bypass.

### Schemas (T09)
- `schemas/lateral_candidate.json`, `schemas/lateral_edges.json`, `schemas/lateral_events.json` — JSON Schema contracts.
- 26 events in `lateral_events.json` using kit's extended bus topic notation: 4-segment `Source.Category.Object.Action`, optional snake_case modifier on Object, `bus.Qualifiers` payload struct for reason/mechanism/property/circumstance.

### Materialize (T10)
- `materialize.Materializer` — probationary record + `discovered_by` edge OR `refers_to_canonical` edge-only. Clock injection via `WithNowFunc`.

### Lifecycle (T11-T12)
- `lifecycle.Rules` + `NewMachine(pub, opts...)` — wired to `kit/runtime/domain.StateMachine`. States: probationary/expired/promoted. Resurrection edge: expired → promoted.
- `lifecycle.ComputeExpiresAt` + `IsColdStale` + `IsSanityViolation` — pure TTL math. `SanityFutureBound = 30d` (reaper-time guard).

### Deferred actions (T13-T18)
- Wired entirely on `kit/runtime/job` — `Service`, `Poller`, `BackoffStrategy`, `Heartbeat`, `ReleaseStaleClaims`. No hand-rolled queue/reaper.
- `jobs.EnqueueDeferred(svc, Deferred{CandidateID, Cause, ...})` — typed wrapper.
- `jobs.HandlerMap(Handlers{ColdCycleExpiry: ..., ...})` — cause-keyed dispatch.
- `jobs.ColdCycleHandler(svc, pub, scan)` — heartbeat + emits `ctxt.lateral.reaper_cycle.completed` via `domain.EventPublisher`.

### Promote + Reject (T19-T20)
- `promote.Handler.Promote(ctx, id, path)` + `policies.yaml` — CEL gates via `kit/runtime/policy/withcel`. Resurrection-window rule; deny → `domain.ErrConflict`.
- `reject.Handler.Reject(ctx, id, reason)` + `policies.yaml` — two CEL rules (no-already-promoted, reason-required). Sibling-metadata preservation via read-modify-write.

### Page-shape classifier (T21-T23)
- `pageshape.Heuristic` — 7 URL/HTML signature rules → labels (`PricingPage`, `ProductPage`, `Blog`, etc.). Internal dedup. 12 typed `Label` constants spanning all 11 shape strategies.
- `pageshape.LLMClassifier` + `RecipeCache` — recipe cache keyed by (domain, normalized URL pattern). Empty verdicts not cached. Slug-vs-version disambiguation in `idRe` (3+ trailing digits).
- `pageshape.NotifyExtractionFailure` — recipe invalidation hook for ibr.

### Bus event catalog (T24)
- `events.catalog.go` — 26 typed `bus.Topic` constants built via `bus.TopicOf(...).Mod(...).Action(...)`. Validated at process init.
- `events.payloads.go` — 4 typed payload types (`CyclePayload`, `FailurePayload`, `SanityViolationPayload`, `CandidateLifecyclePayload`) implementing `notify.WithSeverity`. Each embeds `bus.Qualifiers`.

### Config (T25-T26)
- `config.Config` + `Defaults()` + `Load(opts)` — thin wrapper over `kit/core/config`'s layered loader (system → user → project → extras → env → CLI).
- `config.NewReloadable(opts)` + `WatchSignal(ctx, r)` — SIGHUP-driven hot reload via `kit/core/config.Reloadable[T]` + `WatchSignal`. Mutable: scoring + cold-cycle/soft-delete days. Immutable (vetoed on reload): jobs.* + p3-reference-threshold.

### Tests (T27-T28)
- `integration_test.go` — full pipeline E2E (Registry → Probe → Score → CapGate → Resolve → Materialize) via FakeStrategy.
- `properties_test.go` — 3 `testing/quick` properties on dispatch invariants (child shadows parent, all shapes fire, JIT gated by Tier-A).

## What did NOT ship (deferred to P2-P7)

- Concrete strategies: `JITStrategy` (P2), `GitHubStrategy` + 9 platform parents + 13 platform children (P3-P4), 11 shape strategies (P5).
- Research-intent disambiguation layer (P5).
- CLI surface: `ctxt show suggestions`, `ctxt promote`, `ctxt reject`, `ctxt lateral list/stats/suggest-weights/summary` (P6).
- ADR + workflow + architecture docs + release notes (P7).

## Kit work surfaced + landed

The substrate audit found 5 reuse opportunities in landed code (T01-T10) and several
forward-looking ones for T11-T28. We filed 4 kit tracks; **3 shipped during P1**:

- **`kit-bus-extended-notation`** ✅ shipped — `bus.TopicOf` builder, `bus.ParseTopic`, `bus.Qualifiers` payload struct, `bus.QualifiersFrom` extractor, `bus.Validate`, ADR-0017. The four sigil notation we briefly designed got replaced by `Qualifiers` after recognizing topic-as-routing-key cardinality concerns.
- **`kit-runtime-sync-wallclock`** ✅ shipped — `WallClock` interface, `SystemWallClock`, `FixedClock`, `MockWallClock`, HLC↔WallClock wire. (Materializer's `WithNowFunc` can swap to this in a follow-up.)
- **`kit-config-sighup-reload`** ✅ shipped — `Reloadable[T]`, `WatchSignal`, partition via `reload:"true"` tag, atomic swap, veto on immutable change, `ReloadedPayload`/`ReloadFailedPayload` events, ADR-0016. T26 wired this directly.
- **`kit-runtime-bus-topic-naming`** abandoned — superseded by extended-notation track above.

Two follow-on ctxt-side tracks filed (not yet started): `ctxt-bus-extended-notation` (sweep ambient + ADR-066 update), `hoptop-clis-bus-notation-sweep` (sibling CLIs).

## Audit-driven cleanup commits on the branch

Five `refactor(lateral)` commits applied audit-1 through audit-5: alias `identity.ErrNotFound` to `domain.ErrNotFound`; replace bespoke retry schedule with `kit/core/util.Retry`; inject Clock from `kit/runtime/sync`; use `kit/core/util.Short` for fingerprint digest; doc `Eva` production wiring + TODO markers on test fakes.

Three event-naming sweeps: per-category catalog split → reverted to single per-category lateral catalog with kit's wire form (`bus.TopicOf` builder + `Qualifiers` payload). Final form: `ctxt.lateral.<object>[_<modifier>].<action>` with semantic axes in payload.

## Stats

- **60 files changed, 3,949 insertions, 47 deletions** vs main.
- **43 commits** on branch.
- **66 tests passing** across 13 lateral packages.
- **gofumpt + go vet + go build clean**.
