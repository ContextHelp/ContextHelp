# Device Sync Replication Survey — Candidates Against the dpkms Substrate

> **Date:** 2026-08-25
> **Applies to:** dPKMS, ctxt
> **Extends:** [2026-08-25-device-sync-requirements.md](2026-08-25-device-sync-requirements.md)
> **References:** ADR-063, ADR-064, ADR-070,
> [2026-08-04-cgo-bifurcation.md](2026-08-04-cgo-bifurcation.md),
> [2026-08-04-storage-federation-addendum.md](2026-08-04-storage-federation-addendum.md),
> [2026-08-04-retraction-propagation.md](2026-08-04-retraction-propagation.md)

Evaluates candidate multi-writer replication mechanisms concretely against the storage layer as it exists — not against an idealized SQLite app. Recommendation at the end; per-type merge *semantics* are the next note's job, but the mechanism chosen here must be able to carry them.

## 0. Substrate facts that constrain every candidate

1. **One SQLite file per instance; sync state co-located transactionally.** The federation addendum's strongest architectural point: cursors/watermarks that commit in the same transaction as the content they describe are never inconsistent after a crash ([2026-08-04-storage-federation-addendum.md](2026-08-04-storage-federation-addendum.md)). Any candidate that keeps replication state outside the DB forfeits this.
2. **One driver, canonically built:** `mattn/go-sqlite3` v1.14.37, CGO, `-tags fts5`, statically linked sqlite-vec (`internal/storage/sqlite/driver.go`). The cgo-bifurcation analysis established the discipline: *capabilities are declared, not discovered by crashing*, and build-tag-dependent behavior is a bug class ([2026-08-04-cgo-bifurcation.md](2026-08-04-cgo-bifurcation.md) §3). A sync design must not hang correctness on a tag or a driver swap that P-010/P-020 already evaluated and declined.
3. **Derived indexes are external-content and rebuild-sensitive.** `objects_fts` is FTS5 with `content='objects'` (`internal/storage/sqlite/migrations/001_initial.sql:27-33`); vec0 rows and the `embeddings` blob hang off objects. ADR-070's taxonomy applies: per-object incremental reindex (`reindex_auto`) is cheap; anything forcing corpus-global rebuild after sync is disqualifying.
4. **Identity today is layered:** durable `objects.id` (TEXT PK), plus a **UNIQUE** `content_hash` (dedup/reinforcement, `migrations/003_content_hash_reinforcement.sql`), plus a **UNIQUE** `source_key` (external dedup, `migrations/026_source_key.sql`). Two devices capturing the same content independently will collide on these unique indexes with *different* ids — the mechanism must give the merge layer a place to resolve that (it is a semantic question, but the mechanism must surface it rather than abort on constraint violation).
5. **The corpus is append-mostly.** Capture creates; enrichment appends typed nodes to `graph_json` with stable node UUIDs (ADR-063); curation flips `status`, adds tags/edges; true concurrent edits to one object are rare. The design center is cheap convergence for creates/appends, with a deliberate path for the rare real conflict.
6. **Writes flow through one Go choke point.** All mutations go through the storage driver / service layer (the single-writer analysis mapped this; [2026-08-04-single-writer-enforcement.md](2026-08-04-single-writer-enforcement.md)). There is no third-party writer to the DB — so *application-level* change capture is complete if placed at that choke point. This is the fact that makes an app-written oplog viable where it would be unsound in a shared-DB system.

## 1. Kill criteria (fixed before scoring)

| # | Kill if the candidate… |
|---|---|
| K1 | requires a SQLite driver migration or a maintained driver fork (P-010/P-020 territory, re-litigated for a sync feature) |
| K2 | makes sync correctness depend on a build tag or optional native capability (silent-degradation class of [2026-08-04-cgo-bifurcation.md](2026-08-04-cgo-bifurcation.md)) |
| K3 | cannot express per-type/per-field semantics — forces one generic row policy over status lattices, counters, and set-valued fields |
| K4 | forces non-incremental derived-index rebuild after sync (violates AC10) |
| K5 | has wire or scan cost O(corpus) rather than O(divergence) (violates AC9) |
| K6 | requires a live two-way session (cannot run over relay/sneakernet, violates AC8) |
| K7 | takes schema ownership away from the migration ladder (external tool rewrites/annotates tables) |

## 2. Candidates

### A — Whole-row LWW with hybrid logical clocks

Each row carries `hlc` (HLC timestamp: physical 48 bits + logical counter + device id as tiebreak) and `origin_device`. Sync exchanges rows changed since the peer's acked HLC; receiver keeps the higher HLC per row. Vector clocks are strictly worse here: per-row vectors cost O(devices) per row and answer "concurrent or not," which whole-row LWW then ignores anyway. HLC gives total order with bounded clock-skew sensitivity (logical component absorbs skew; device-id tiebreak makes it deterministic) at ~10 bytes/row.

- **Metadata cost:** minimal — two columns on `objects`, `edges`, `entities`.
- **Clock skew:** HLC tolerates arbitrary skew for correctness (order is still total and causal-when-observed); *fairness* degrades — a device with a fast clock wins races it shouldn't. Acceptable for one owner.
- **Tombstone interplay:** needs a separate tombstone row per delete regardless (deleted rows can't carry their own clock).
- **Fatal weakness (K3):** whole-row wins destroy concurrent orthogonal work: device A tags an object, device B fixes its summary → one device's write erases the other's. In an append-mostly corpus most "conflicts" are exactly this — orthogonal enrichment — so the common case loses data. AC4 then demands a conflict copy for what should have merged cleanly.
- **Verdict: killed on K3** as the *primary* mechanism; HLC itself is retained as the timestamp primitive for whichever mechanism wins.

### B — Field-group CRDT merge (state-based)

Split each type into merge units with CRDT semantics: LWW-register per scalar field group (status, summary block), OR-set for set-valued fields (tags, edges, graph nodes keyed by ADR-063's stable node UUIDs), a counter for `reinforcement_count`. Sync exchanges object states; receiver merges field-wise.

- **What is actually mergeable this way:** most of the schema, and ADR-063 is the enabler — `graph_json` nodes have stable UUIDs, so node-set union is well-defined; edges have semantic identity `(from,to,type)`; tags/aliases are sets; `reinforcement_count` is a counter; `status` is a small lattice. The unmergeable residue is exactly `raw_content` edits (register with conflict surfacing).
- **Metadata cost:** the problem. Field-wise state merge needs per-field-group clocks *persisted per row* — a `sync_meta` JSON column per table or a sidecar table ~O(rows × field-groups). For OR-sets, add-ids or dot metadata per element. On 10⁶ objects this is real bloat and touches every storage read/write path.
- **State-based exchange is O(changed rows) at best** but re-ships whole objects for one field flip, and computing "changed since your ack" still needs per-row versions — i.e., it quietly re-grows half of candidate C's machinery.
- **Verdict: the *semantics* are right — this is the merge model the conflict note should specify — but as a *wire/state mechanism* it is the expensive way to carry them.** Not killed; subsumed by C.

### C — Op-based replication: semantic oplog in the same DB **(recommended)**

Append a `sync_log` table in the instance DB, written **in the same transaction** as every mutation at the storage/service choke point (§0.6): `(seq INTEGER autoincrement, device_id, hlc, op_type, entity_kind, entity_id, payload JSON)`. Ops are *semantic*, not physical: `object.create`, `object.edit` (content supersession), `object.set_status`, `object.add_node`, `edge.add`, `edge.remove`, `entity.enrich`, `*.delete` (tombstone), `reinforce`. Exchange: peers track, per remote device, the highest contiguous `seq` acked (a device-sequence vector — O(roster) integers total, not per-row); a sync ships ops the peer lacks, in order per origin device. Receiver applies each op through the per-type merge rules (candidate B's semantics), which are commutative/idempotent by construction, then records it — application and ack commit atomically (§0.1).

- **Fit with append-mostly:** near-perfect — the dominant workload (creates, node/edge adds) replays as pure inserts; op replay *is* the ingest path, so content-hash/source-key collisions surface exactly where the merge rules can resolve them instead of as constraint aborts.
- **Derived indexes:** each applied op maps to the same incremental FTS/vec maintenance the normal write path already performs (ADR-070 bucket 1, per object). No global rebuilds.
- **Storage overhead:** O(ops), not O(rows × fields). The log is prunable: an op is GC-able once every roster device has acked past it (same causal-stability rule as tombstones — [2026-08-04-retraction-propagation.md](2026-08-04-retraction-propagation.md) §3 — computable exactly over a known roster; tombstone ops respect the retention floor).
- **Auditability:** the log *is* an attributable history — each op signed/attributed to a device key, dovetailing with the append-only `audit_log` (`migrations/011_audit_log.sql`) and AC12. No other candidate gives this for free.
- **Transports:** an op batch is a self-contained, signable, encryptable envelope — works over HTTP to the hub, LAN stream, relay queue, or a file (AC8/K6 clean).
- **Failure modes, honestly:** (a) *completeness depends on the choke point* — any code path that mutates tables without logging forks state silently; mitigation: forbid raw writes outside the storage layer (already the rule), plus a state-digest anti-entropy check (below) to *detect* divergence rather than assume it away. (b) *Op-schema evolution* — payloads must be versioned from day one; an old device receiving unknown op types must stop-and-ask, not skip. (c) *Replay determinism* — merge rules must be pure functions of (op, state); no wall-clock reads inside apply.
- **Anti-entropy backstop:** periodic Merkle/range digest comparison over (entity_kind, id, per-row HLC) detects silent divergence (bug, restored backup, pre-sync history) and drives the epoch full-resync path from requirements S4 — state-based reconciliation as the *repair* tool, op-based as the *normal* path. This is the CouchDB/Syncthing lesson: every production op-shipping system grows a state-comparison repair mode; design it in.

### D — SQLite session extension (changeset/patchset)

**Probed empirically against the shipped driver stack, per the rule that library behavior is verified before it is spec'd. Results:**

- The session C API is **compiled out** of the vendored amalgamation: `mattn/go-sqlite3` v1.14.37's cgo CFLAGS define FTS3/RTREE/etc. but **not** `SQLITE_ENABLE_SESSION` (module `sqlite3.go` cgo preamble); every session symbol in `sqlite3-binding.c` sits behind `#if defined(SQLITE_ENABLE_SESSION)` (e.g. binding lines ~11671, ~230962) and is therefore absent from the built object.
- There are **no Go bindings**: zero `sqlite3session_*`/changeset symbols anywhere in the module's Go layer.
- The prerequisite preupdate hook exists only behind the `sqlite_preupdate_hook` build tag (`sqlite3_opt_preupdate_hook.go`) — the exact tag-dependent-capability class [2026-08-04-cgo-bifurcation.md](2026-08-04-cgo-bifurcation.md) §2 documents as silently-degrading.
- Reaching the API therefore means a maintained driver fork (add the define + write the cgo bindings) or migrating to `ncruces/go-sqlite3` (which does bind sessions, but is the wasm driver stack P-010/P-020 declined, and would orphan the statically-linked cgo sqlite-vec).

**Killed on K1 and K2.** Even were it reachable: changesets are *physical row diffs* with row-level conflict callbacks — no field-group semantics (K3), and tombstone/lattice behavior would be reimplemented inside conflict handlers anyway, i.e. candidate C's merge layer wearing a C API.

### E — cr-sqlite (CRDT extension for SQLite)

Runtime-loadable extension turning tables into CRRs with column-level LWW via lamport-versioned metadata. Probed: extension loading *is* available in the shipped driver (`sqlite3_load_extension.go` builds unless `sqlite_omit_load_extension`), so this is not K1/K2 dead on arrival. It dies on fit:

- **K7:** cr-sqlite rewrites table schemas (CRR conversion, `__crsql_*` metadata, site-id plumbing) — schema ownership leaves the migration ladder; interaction with the external-content FTS5 table, vec0 virtual table, and the reactive trigger set is unspecified territory we would own.
- **K3:** column-level LWW is candidate A one level finer — still no status lattice, no counter semantics beyond its built-ins, no edge-vs-endpoint-tombstone rule, no conflict surfacing (silent LWW loss is the documented behavior).
- **Deployment:** a per-platform dynamic library shipped beside a single static binary, exactly the artifact-skew class the cgo analysis spent its whole length eliminating.
- **Verdict: killed (K7, K3).** Its real lesson is positive evidence for C: cr-sqlite's own sync is op/version-vector shipping over a log — the architecture works; we need it with domain semantics and inside the migration ladder.

### F — Prior art, mined for rules rather than adopted

| System | Model | Take |
|---|---|---|
| **Syncthing** | file-level sync; concurrent edits → `.sync-conflict` copies | Conflict-copy UX is the right *surfacing* primitive for unmergeable content edits (AC4). File granularity itself is far too coarse — one DB file is one "file". |
| **CouchDB/PouchDB** | per-doc revision trees; deterministic winner; losers preserved as open branches | Deterministic-winner-plus-preserved-loser is the correct default posture; whole-doc granularity loses orthogonal merges (same flaw as A); rev-tree storage per object is heavy. Its replication checkpoint protocol ≈ device-sequence vectors, validating C's exchange shape. |
| **iCloud / Core Data mirroring** | per-entity, per-field merge policies pushed through one choke-point API | Closest production analog to B-semantics-over-C-mechanism: field-level merge with app-defined policies, feasible precisely because all writes flow through one framework — the §0.6 property. |
| **Automerge & JSON CRDTs** | full generic CRDT document | Per-character/per-key generality the corpus does not need, at 2–10× storage overhead; adopting it means marshaling KnowledgeObjects into a foreign store. Rejected; its OR-set and counter constructions are the textbook implementations for B's field rules. |

## 3. Scoring

Scale: ++ strong / + adequate / − weak / ✗ kill. Axes fixed by the task; kill criteria from §1.

| Axis | A row-LWW+HLC | B field-CRDT (state) | **C oplog (op-based)** | D session ext | E cr-sqlite |
|---|---|---|---|---|---|
| Fit: append-mostly corpus | + | ++ | **++** | + | + |
| Derived-index rebuild cost | + (per row) | + | **++ (per op = normal write path)** | − (row diffs bypass service layer) | − (unknown vs FTS/vec triggers) |
| Storage overhead | ++ (2 cols) | − (per-field meta) | **+ (O(ops), GC by roster ack)** | + | − (per-column meta) |
| Implementation surface | ++ smallest | − large (every R/W path) | **− large (log + apply + exchange), but owned and testable** | ✗ K1/K2 | ✗ K7 |
| Auditability | − (state only) | − | **++ (log is the audit trail, signable)** | − | − |
| Failure modes | silent orthogonal-write loss (K3) | metadata bloat; still needs per-row versions | choke-point completeness; op versioning; needs anti-entropy backstop | unreachable API; physical diffs | schema ownership; silent LWW |
| Kill criteria hit | K3 | none (subsumed) | **none** | K1, K2, (K3) | K7, K3 |

## 4. Recommendation

**Adopt op-based replication (C): a semantic, HLC-stamped, device-attributed `sync_log` co-located in the instance DB, exchanged by device-sequence vector, applied through per-type merge rules with CRDT semantics (B) — plus a digest-based anti-entropy/repair path for divergence detection and epoch resync.** HLC (from A) is the timestamp primitive; conflict-copy surfacing (from F/Syncthing) and deterministic-winner-loser-preserved (from F/CouchDB) are the UX posture; D and E are killed with the evidence above recorded so neither is re-proposed without new facts.

What this hands the next notes:

- **Conflict-semantics note:** define the op vocabulary and the per-type merge lattices/rules C replays — object identity vs the two unique dedup indexes, status lattice, node/edge OR-sets, tombstone classes and GC, entity enrichment merge, conflict surfacing.
- **Identity/transport note:** op batches as signed envelopes; device roster as the basis of sequence vectors, ack tracking, and GC quorum.
- **Foundation checklist (prototype note):** `sync_log` + `devices` + tombstone schema shape, additive and reversible; the atomic-purge dependency shared with retraction; go/no-go gates already resolved here (session extension: no-go; cr-sqlite: no-go; oplog-at-choke-point: go, contingent on the no-out-of-band-writes rule holding).

## Appendix — probe record (D/E)

Evidence gathered from the module actually vendored by this repo (`go.mod`: `github.com/mattn/go-sqlite3 v1.14.37`), module cache copy:

1. `grep -rln 'sqlite3session\|Changeset' <mod>/  *.go` → no matches: no Go surface for the session API.
2. `grep -n '#cgo CFLAGS' <mod>/sqlite3.go` → defines `SQLITE_ENABLE_RTREE`, `SQLITE_ENABLE_FTS3`, … — `SQLITE_ENABLE_SESSION` absent → session code `#ifdef`-excluded from the amalgamation build (guards at `sqlite3-binding.c` ~11671, ~22930, ~230962).
3. `sqlite3_opt_preupdate_hook.go` carries `//go:build sqlite_preupdate_hook` → preupdate capability is build-tag-gated; not part of the canonical build ([2026-08-04-cgo-bifurcation.md](2026-08-04-cgo-bifurcation.md) decision 1 defines the canonical tag set as `fts5` only).
4. `sqlite3_load_extension.go` carries `//go:build !sqlite_omit_load_extension` → runtime extension loading available by default in the canonical build (E was evaluated on merit, not reachability).
