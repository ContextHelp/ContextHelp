# Device Sync Prototype — Scope and Foundation Checklist

> **Date:** 2026-08-25
> **Applies to:** dPKMS, ctxt
> **Implements:** [ADR-074](../decisions/ADR-074-same-owner-device-sync.md) Phase 0/1
> **References:** [conflict semantics](../analysis/2026-08-25-device-sync-conflict-semantics.md) (§7 test contract),
> [replication survey](../analysis/2026-08-25-device-sync-replication-survey.md),
> [requirements](../analysis/2026-08-25-device-sync-requirements.md) (AC list),
> [2026-08-04-retraction-propagation.md](../analysis/2026-08-04-retraction-propagation.md)

The smallest end-to-end build that can falsify ADR-074's riskiest assumptions, plus the foundation work that can start alongside it. Nothing beyond what this spec explicitly de-risks gets implemented under the design track.

## 1. Assumptions the prototype must test (riskiest first)

| # | Assumption (source) | Falsified if… |
|---|---|---|
| R1 | The storage/service choke point captures **every** mutation into `sync_log` in the same transaction (survey §0.6, §2.C) | any scripted workload produces a row change with no matching op, or an op that doesn't match the row change (checked by digest after replay) |
| R2 | The per-type merge rules are commutative/idempotent **as implemented**, not just as specified (conflict semantics §7) | any op-pair schedule yields different final digests on two replicas |
| R3 | Replaying remote ops through the normal write path keeps FTS5 + vec0 incrementally correct (ADR-070 bucket 1; AC10) | post-sync search results differ from a from-scratch reindex of the same state |
| R4 | Unique-index collisions (`content_hash`, `source_key`) convert cleanly to alias+reinforce during replay instead of aborting (conflict semantics §1.1) | replay of independent same-content captures errors, duplicates, or loses reinforcement counts |
| R5 | Tombstone application + retained tombstone actually prevents resurrection from a lagging replica (AC5) | a delayed peer's old ops re-create a deleted object |
| R6 | Sequence-vector exchange is resumable and O(divergence) (AC9) | an interrupted sync corrupts/duplicates state, or wire volume scales with corpus size rather than delta |
| R7 | HLC ordering is sane under deliberate clock skew (survey §2.A) | ±5 min skew between devices produces non-deterministic or causally-wrong winners |

Identity/pairing ceremony, relay, and blob transfer are deliberately **not** on this list — they are risky but separable, and each has a designed fallback; R1–R7 are the assumptions the whole architecture stands on.

## 2. Prototype pin

- **Two devices** = two dpkms instances (two DB files; same host or two hosts — both configurations in CI where possible, same host minimum).
- **Transport: LAN tier only, statically configured** — direct TCP between the two instances with fixed pre-shared device keys in config. No mDNS, no PAKE ceremony, no relay, no hub role, no roster document (a 2-entry static roster in config). The transport is a carrier for envelopes, not the thing under test.
- **Data: objects + edges only.** No entities, no blobs (externalization disabled in prototype config), no profiles beyond default.
- **Op vocabulary:** `object.create`, `object.edit`, `object.set_status`, `object.add_node`, `object.delete`, `edge.add`, `edge.remove`, `reinforce` — enough to express every §7 scenario.
- **Conflict surfacing:** conflict copies + `resurfacing_queue` entries land as specified; a bare `ctxt sync conflicts` listing (read-only) is in scope; resolution commands are not.
- **Scripted conflict scenarios as acceptance tests** — the conflict-semantics §7 pair matrix, executed as: partition both instances, apply scripted op sets on each side, sync in both orders (A→B then B→A, and the reverse pairing), assert identical final digests and the specified outcome per pair: create×create (same content), edit×edit, status×status, discard×edit, delete×edit, delete×edge.add, edge.add×edge.add (same semantic edge), edge.remove×edge.add, reinforce×reinforce.

### Exit criteria (measurable; all must pass)

1. **Convergence:** after N=100 randomized concurrent ops per side across ≥10 random partition/sync schedules, both replicas' state digests are byte-identical, every run. (R2, R6)
2. **Completeness:** replaying one replica's full `sync_log` against an empty DB reproduces its state digest exactly. (R1)
3. **No resurrection:** delete on A concurrent with edits on B → after full exchange, object absent on both, B's edit preserved in review queue; a third exchange from a deliberately-lagged log replay does not re-create it. (R5)
4. **Index correctness:** post-sync FTS and vector query results on each replica equal a from-scratch rebuild of the same rows (query-set diff empty). (R3)
5. **Dedup collision:** same content captured independently on both sides → one canonical object, alias recorded, `reinforcement_count` = sum, no constraint errors in logs. (R4)
6. **Resumability + cost:** kill the transport mid-sync at three injected points → next sync completes with criteria 1–5 intact; wire bytes for a 10-op delta on a 10⁵-object corpus are within 10× of the 10-op payload size (no corpus-proportional term). (R6)
7. **Skew:** all of the above with one instance's HLC physical clock offset ±5 min. (R7)

## 3. Foundation checklist (can start alongside; confirmed against ADR-074)

- [ ] **Schema migration, additive and reversible:** `sync_log`, `devices` (static-roster projection now, ceremony-populated later), `tombstones` (+`hlc`, `origin_device` — the retraction note's shape extended, one table for both consumers), `object_id_aliases`. Down-migration drops tables/columns without touching existing data. Migration numbering per the existing ladder (`internal/storage/sqlite/migrations/`).
- [ ] **HLC utility** (generate, parse, merge-on-receive, device-id tiebreak) with skew property tests — shared by oplog capture and merge engine.
- [ ] **Atomic local purge primitive** — object row + edges + FTS entry + vec row + embeddings in one transaction. Prerequisite here for tombstone application; independently a prerequisite of the retraction protocol and the local privacy promise — one implementation serves both consumers.
- [ ] **Determinism test harness:** the §7 pair matrix as property tests against the merge engine in-process (no transport) — this lands *before* the transport exists and gates everything after.
- [ ] **Choke-point audit:** enumerate every code path that writes `objects`/`edges` and assert each flows through the storage layer (the survey's §0.6 premise); any bypass found is a blocker bug for R1, filed and fixed before the prototype claims completeness.

**Go/no-go gates already resolved by the survey's empirical probes (do not re-litigate without new facts):** SQLite session extension — **no-go** (not compiled into mattn v1.14.37, no Go bindings; K1/K2). cr-sqlite — **no-go** (schema ownership, no domain semantics; K7/K3). App-level oplog at the choke point — **go**, contingent on the choke-point audit above holding.

## 4. Follow-up track proposal

One new **feature track: `device-sync-prototype`** (distinct from the design track, which closes with this spec). Phase structure mirrors ADR-074 Phase 0→1; every task lands behind a config flag (`sync.enabled`, default off), nothing ships user-facing:

1. Schema migration + HLC utility (+ down-migration test).
2. Oplog capture at the choke point, dark (log-only, no exchange) + choke-point audit.
3. Merge engine + determinism harness (in-process, both sides simulated).
4. Atomic purge primitive + tombstone application path.
5. Envelope format + static-key TCP transport + sequence-vector exchange.
6. Scenario runner + exit-criteria suite (CI job, two-instance).
7. Skew/interruption/cost measurements; findings written back to a dated analysis note; ADR-074 status Proposed→Accepted (or amended) on the evidence.

Estimated shape: 7 tasks, S–L, strictly ordered except 4 (parallel with 3) and the harness (3) intentionally ahead of transport (5). Later phases of ADR-074 (pairing ceremony, hub tier, relay, blobs, GC/epoch) get their own tracks only after this one's findings are in.
