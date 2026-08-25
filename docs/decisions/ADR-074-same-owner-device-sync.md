# ADR-074 – Same-Owner Device Sync: Op-Log Replication with Deterministic Merge

> **Status:** Proposed
> **Date:** 2026-08-25
> **Author:** $USER
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None (amends ADR-064 Phase 3 scope; see Context)
> **References:** ADR-001, ADR-019, ADR-023, ADR-049, ADR-062, ADR-063, ADR-064, ADR-070, ADR-071

---

## Context

Story 70 (`docs/ctxt/user-stories.md:97`): *sync my knowledge store across my own devices, local-first, without requiring a central server.* ADR-001 accepted local-first and deferred multi-device sync; ADR-064 built push-only federation and deferred "pull support; bidirectional sync; conflict resolution ADR required first" to Phase 3. This is that ADR, for the same-owner case.

Federation cannot serve device sync, structurally: its architecture rests on every object having one authoritative home with subscribers holding derived copies; its wire format is insert-only with no verbs (`internal/federation/remote.go:21-23`), pull is a stub (`internal/federation/pull.go`), content-hash dedup forks silently on edit, there are no tombstones, and the worker enumerates the full corpus per tick (`internal/federation/worker.go:189`). Device sync breaks the single-home invariant on purpose: 2–5 devices, one owner, **all replicas writable**, disconnected for hours to weeks, converging without data loss.

Two topologies must be served by one design:

- **Hub (priority deployment):** an owner-hosted, internet-reachable dpkms instance as the designated always-on replica — composing with the inbound-auth surface (ADR-023), consistent with the ambient-capture premise that dpkms is often remote (`docs/architecture/ambient-capture.md`) — plus local instances that mirror while connected, take writes while disconnected, and reconcile on reconnect.
- **Mesh:** peer devices over LAN, untrusted relay, or file transfer, no primary.

Full requirements, threat model (S1–S7), and acceptance criteria (AC1–AC12): [`docs/analysis/2026-08-25-device-sync-requirements.md`](../analysis/2026-08-25-device-sync-requirements.md). Mechanism survey with pre-committed kill criteria and empirical probes: [`2026-08-25-device-sync-replication-survey.md`](../analysis/2026-08-25-device-sync-replication-survey.md). Merge semantics: [`2026-08-25-device-sync-conflict-semantics.md`](../analysis/2026-08-25-device-sync-conflict-semantics.md). Identity/pairing/transport: [`2026-08-25-device-sync-identity-transport.md`](../analysis/2026-08-25-device-sync-identity-transport.md).

---

## Decision

**Same-owner device sync becomes the system's third replication mode — a separate protocol from federation, sharing its cryptographic and tombstone concepts but not its wire format — implemented as op-based replication: a semantic, HLC-stamped, device-attributed operation log co-located transactionally in the instance database, exchanged by per-device sequence vectors over four transport tiers, and applied through deterministic per-type merge rules with conflict surfacing.**

Concretely:

1. **Replication mechanism.** Every mutation at the storage/service choke point appends, in the same transaction, a semantic op (`object.create/edit/set_status/add_node/delete`, `edge.add/remove`, `entity.enrich`, `reinforce`, …) to a `sync_log` table: `(seq, device_id, hlc, op_type, entity_kind, entity_id, payload)` plus causal metadata per the fleet's causality option (point 3). Peers exchange ops they lack, tracked by per-device acked sequence vectors, and persist each device's observed high-water (own max seq + vector) — a regression at handshake (restored backup) refuses normal exchange and forces epoch resync with seq fast-forward. Application and acknowledgment commit atomically. Rows persist per-field-group merge clocks (HLC columns) so LWW outlives op GC; a Merkle/range digest over replicated truth — rows plus their merge clocks, conflict artifacts per the conflict-copy option — provides anti-entropy detection and drives epoch full-resync for divergent or long-offline replicas. GC epochs are per-device (each device advertises its GC frontier; a peer acked behind it takes resync); only protocol-parameter epochs are global, bumped by a root-signed roster record.
2. **Merge semantics** (summary; the conflict-semantics note is normative). Replication identity is the durable `objects.id`; unique-index collisions (`content_hash`, `source_key`) merge as alias-plus-reinforce — with written alias-normalization when the canonical (lower-create-HLC) row arrives after the loser has accreted state, and the same rule resolving an edit whose recomputed hash collides with a different live object — and edits supersede in place, never content-hash forks. Field classes: capture immutables (conflict-impossible); content registers (LWW by HLC, loser preserved per the fleet conflict-copy option — deterministic corpus conflict copy linked `conflicts_with`, and/or payload-bearing `sync_conflicts` parking row); curation scalars (LWW); accretive sets — graph nodes keyed by ADR-063 stable UUIDs with semantic-identity dedup, edges — (OR-set, add-wins); counters (op-sum). Status races: LWW, except concurrent edit revives a concurrent discard, and delete (tombstone) beats everything concurrent, with suppressed work parked payload-bearing in `sync_conflicts` (never `resurfacing_queue`, whose NOT-NULL cascade FK and payload-less rows cannot preserve a loser). Edges: semantic identity `(from,to,type)` dedup; referential integrity beats add-wins against endpoint tombstones. Entities: registry-sourced rows never device-sync (registry is authoritative); derived entities re-derive; owner enrichment syncs as LWW/OR-set ops. Deletions: one tombstone table serving both federation retraction and device sync; GC by causal stability over the device roster plus a retention floor; epoch resync for devices returning past the horizon.
3. **Fleet-wide protocol options.** Two merge-protocol parameters are fixed at fleet creation, uniform across the roster, validated at pairing (mismatch refuses admission), and changed only by a root-signed parameter-epoch bump: **causality encoding** — per-op origin sequence-vectors (default) or replicated apply-records with frontier reconstruction — decides every ordered-vs-concurrent branch (HLC is a total order, not happens-before); **conflict-copy policy** — replicated truth (default: losers materialize deterministically on every replica under ids derived from the losing op's `(device, seq)`, inside the AC3 digest) or local-only surfacing (losers park locally, excluded from the digest). Both alternatives of both options are fully specified, with wire/GC/dot costs and per-option digest rules, in the conflict-semantics note §0.
4. **Derived-state invariant.** FTS, vec0, and embedding rows are never part of replicated truth; every applied op runs the normal per-object index maintenance (ADR-070 bucket-1 incremental). Embedding payloads may travel as cache hints only, discarded on model mismatch (ADR-071).
5. **Identity and membership.** Owner root key delegating SSH-CA-shaped certificates to per-device keys (Ed25519 signing + X25519 agreement), reusing the federation key-lifecycle primitives and the full-SHA-256 fingerprint convention. Membership lives in a signed, hash-chained roster replicated in-band — the single authority for transport authentication, sequence vectors, and tombstone-GC quorum. Pairing is a PAKE/QR ceremony via any `admit`-capable device (the hub included); revocation is a roster record with next-contact semantics, cascading quarantine (sync + GC-quorum suspension pending root countersign) to provisional certs the revoked issuer admitted; ops accepted while a provisional cert was in-window remain valid history even if the cert lapses.
6. **Transport tiers, one envelope.** Canonically serialized envelopes carrying **origin-signed op batches** — the attribution unit, intact through forwarding; the envelope signature authenticates only the carrier and is required (with per-recipient sealing) on store-and-forward tiers, omitted on live channel-authenticated tiers — over: (1) direct-to-hub TLS with device authentication as a Provider behind the provider-agnostic `server.auth` seam (ADR-023 Layer 4 as one more inbound authenticator; principals carry roster status; the shared principal → entitlement → metering → security-event pipeline applies; bearer tokens never grant replica membership); (2) LAN via mDNS discovery-as-hint plus Noise-pattern mutual authentication feeding the same Provider path; (3) untrusted self-hostable relay seeing ciphertext and queue metadata only — the hub may double as relay; (4) signed bundle files for sneakernet/air-gap. Sequence-vector exchange makes every tier resumable and the file tier correct by construction.
7. **Topology rule.** "Primary" (the hub) is a routing, availability, and review-surface role — never a merge-semantics role. Hub and mesh run the identical protocol; T1 is the star instance of T2.
8. **Blobs.** Rows and metadata sync always; blob bytes move content-addressed (SHA-256 of stored bytes), sender-driven, blobs-before-rows, with lazy fetch as per-device policy — inheriting the blob-federation recommendation (Option C + B) rather than defining a parallel mechanism.

### Boundary with ADR-064 (amendment)

ADR-064's Phase 3 ("pull support; bidirectional sync; conflict resolution ADR required first") is **split**: the same-owner portion is decided here, as a separate protocol — not an extension of the federation push format. Cross-owner federation pull (subscribers writing back, multi-party trust) remains deferred and is explicitly **not** covered by this ADR's conflict semantics, which assume one owner's cooperative devices. Federation and device sync share: Ed25519 signing and the key-lifecycle delegation model, the tombstone table and its causal-stability GC rule, and the watermark/ack discipline (which device sync strengthens into per-device sequence vectors). They do not share wire formats, endpoints, or merge rules.

---

## Rationale

Why op-based, against the alternatives scored in the survey (kill criteria fixed before scoring; full table there):

- **Whole-row LWW + HLC** — smallest implementation, but whole-row winners erase concurrent orthogonal work (tag on A, summary fix on B); in an append-mostly corpus that is the *common* conflict, so the common case loses data. Killed (K3). HLC itself is retained as the timestamp primitive.
- **Field-wise CRDT state merge** — the right semantics, wrong carrier: per-row/per-field clock metadata across 10⁶ rows, and "changed since your ack" re-grows op tracking anyway. Subsumed: its semantics become the replay rules over the op log.
- **SQLite session extension** — empirically unreachable through the shipped driver: `mattn/go-sqlite3` v1.14.37 compiles the amalgamation without `SQLITE_ENABLE_SESSION` and binds no session API; reaching it means a driver fork or the ncruces migration P-010/P-020 already declined. Killed (K1/K2); physical row-diffs would also re-implement the merge layer inside conflict callbacks (K3).
- **cr-sqlite** — loadable in principle (extension loading is on in the canonical build), but it takes schema ownership from the migration ladder (CRR rewrites vs. external-content FTS5/vec0), imposes column-LWW without domain lattices, and ships a per-platform dynamic library beside a deliberately static binary. Killed (K7/K3).
- **Op log** wins on the substrate's own strengths: the single Go write choke point makes app-level change capture complete; transactional co-location makes sync state crash-consistent with content (the federation addendum's strongest property, kept); replay *is* the ingest path, so unique-index collisions surface where merge rules can resolve them and index maintenance stays per-object; the signed log doubles as the attributable audit trail (AC12) — no other candidate provides that at all. Its real risks — choke-point bypass, op-schema evolution, replay determinism — are named, and the anti-entropy digest exists precisely to *detect* rather than assume away the first.

Why deterministic-default-plus-surfacing rather than pure CRDT convergence everywhere: one owner's concurrent edits are rare and cooperative; burning implementation budget on per-character text CRDTs buys convergence the corpus does not need, while silent LWW everywhere loses the one thing that matters (invested work). The chosen posture — converge automatically where merge is lossless (sets, counters, ordered registers), preserve-and-surface where it is not (content edits, delete races) — is the CouchDB/Syncthing lesson applied with domain knowledge.

Why two fleet-uniform options instead of one hardwired choice: causality encoding and conflict-copy policy trade wire size, GC complexity, and payload-propagation privacy differently per deployment, and both alternatives are sound — so they are protocol parameters, not decisions to relitigate per release. Uniformity is non-negotiable (both options change what the convergence digest covers; a mixed fleet cannot converge byte-identically), hence pairing-time validation and root-signed parameter epochs. The defaults — per-op sequence-vectors and replicated conflict truth — fit the design center: rosters of 2–5 make per-op vectors bytes rather than structure and keep classification a pure function of the ops, and replicated losers give resolve-once-fleet-wide semantics with the hub as review surface.

Why a separate protocol instead of extending federation: the two modes have opposite core invariants (one authoritative home vs. symmetric writers). Grafting bidirectional merge onto the push format would burden every federation deployment with machinery it exists to avoid, and tie device-sync correctness to a wire format built insert-only. Shared concepts are extracted at the primitive level (keys, tombstones, acks) — the decomposition the storage-federation addendum argues for: boring storage, complexity in protocols at the edges, each protocol owning its edge.

---

## Consequences

### Positive

- Story 70 satisfied: offline-capable multi-device knowledge store with no required third-party service; hub deployment gives an always-on primary that composes with inbound auth.
- Convergence is deterministic and testable: commutativity/idempotence obligations are enumerated per rule pair as a test contract; conflicts that discard information are always user-visible.
- The op log yields attribution, audit, and incremental everything (wire cost O(divergence), per-object index maintenance) in one structure.
- Deletion finally has end-to-end semantics on owned devices — tombstones, causal-stability GC over an enumerable roster, epoch resync — strengthening the retraction story rather than duplicating it.
- Identity work is shared with federation key lifecycle: one delegation shape, one fingerprint convention, one revocation mechanism.

### Negative

- Real implementation surface: `sync_log` + roster + tombstone schema, envelope formats, four transports, merge engine, conflict UX. Mitigated by phasing (below) and by the prototype gating the riskiest parts first.
- Op-log completeness depends on the no-out-of-band-writes rule; any direct DB write bypassing the storage layer forks state silently. The flock/daemon-boundary enforcement work (single-writer analysis) becomes more important, and the anti-entropy digest is mandatory, not optional.
- Storage grows by O(ops) until roster-acked GC; long-offline devices delay log and tombstone GC (bounded by revocation and the retention floor).
- A compromised-but-unrevoked device can write to the whole replica set (inherent to the feature); bounded by attribution and revocation, not prevented.
- Op payload versioning is a forever-contract: old devices must stop-and-ask on unknown ops, which means a fleet can stall until its laggard upgrades.
- The binary-version write gate means a stale binary refuses writes once `sync.enabled` — a deliberate availability cost: loud refusal on one device beats a silent oplog bypass forking the whole fleet.

### Neutral / Considerations

- Federation wire format unchanged; ADR-064 Phase 1/2 unaffected.
- The hub holds plaintext like any dpkms instance — "no central server" means no *required third-party*, and the requirements note fixes that reading; relay components see ciphertext only.
- Entities mostly exit the replication problem (registry-authoritative or re-derivable); only owner enrichment syncs.
- Multi-owner collaboration, cross-owner pull, and real-time co-editing remain out of scope and undecided.

---

## Implementation Notes

**Schema additions (additive, reversible; exact DDL at implementation time):** `sync_log` (op stream + applied-remote-op record + causal metadata per the causality option), `devices` (roster projection: keys, cert, status, last_seen, acked vector, observed high-water for regression detection), `tombstones` (shared with retraction protocol; + `hlc`, `origin_device`), `object_id_aliases` (merge identity), `sync_conflicts` (**payload-bearing conflict parking table** — derived-id PK, nullable `object_id` with no FK, losing-op payload/provenance; `resurfacing_queue` structurally cannot serve: NOT-NULL cascade FK, no payload), per-field-group HLC columns on `objects`/`edges`/`entities` (persisted merge clocks), fleet protocol parameters + parameter epoch, and persisted HLC generator state.

**Two-process discipline:** the daemon and CLI share one `device_id`, so HLC generator state (last physical/logical) is persisted and CAS'd inside the same DB transaction that stamps it — in-memory clock state across two processes would break per-device monotonicity. And a **binary-version write gate**: when `sync.enabled`, a binary lacking oplog capture refuses writes (a migration stamps the capability floor) — a stale binary silently bypassing the log is the choke-point failure mode arriving through skew, and loud refusal beats silent state fork.

**Prerequisites shared with other tracks:** atomic local purge (retraction sequencing step 1 — device tombstone application depends on it); content-addressed blob re-key (blob-federation step 1) before blob transfer ships; fingerprint unification and secrets-backend routing from the key-lifecycle note.

**Phasing:**

- **Phase 0 — prototype** (separate scoped spec): two devices, LAN-only, objects + edges only, scripted conflict scenarios from the determinism test contract as acceptance tests.
- **Phase 1 — foundation:** `sync_log` capture at the choke point (dark: logging only, no exchange); schema migrations; HLC plumbing; determinism test suite against the merge engine in-process.
- **Phase 2 — hub tier:** device identity/pairing/roster, tier-1 transport, hub topology end-to-end including conflict surfacing.
- **Phase 3 — mesh tiers:** LAN (tier 2), file (tier 4), relay (tier 3); tombstone GC + epoch resync.
- **Phase 4 — blobs:** manifest/check/lazy-fetch riding the blob-federation mechanism.

**Testing:** the pairwise op-commutativity matrix as property tests; multi-device convergence sims (random schedules over 3–5 replicas comparing final digests); xrr cassettes for transport-level exchanges; ben-style timing for reconciliation-after-N-days scenarios.

---

## Honest limits

1. **Convergence is guaranteed only for synced state under the stated rules;** a device that never reconnects diverges forever, and a bypassing writer forks state until the digest check catches it. Detection is designed; prevention of bypass is a separate enforcement track.
2. **Resurrection windows exist:** tombstone GC before a silent (unrevoked, past-floor) device returns is resolved by epoch resync — current state wins, the device's unsynced ops replay on top; content it alone held *after* the epoch boundary survives, deleted content does not return, but the reconciliation is coarse by design.
3. **Deterministic defaults are sometimes wrong** — LWW on curation scalars can override the decision the owner actually preferred; the design bounds this to cheap, reversible fields and surfaces everything else, but does not eliminate it.
4. **Relay/hub availability trade-offs:** the mesh works with zero infrastructure, at the cost of revocation latency and rendezvous friction; the hub removes both at the cost of running a server. The design makes the trade owner-visible rather than pretending neither cost exists.
5. **Bytes on a stolen device are beyond protocol reach** — revocation cuts participation; ADR-019 at-rest encryption is the only protection for what was already replicated.

---

## References

- [ADR-001 – Local-First and Decentralized](ADR-001-local-first-and-decentralized.md)
- [ADR-019 – Encryption & Privacy](ADR-019-encryption-and-privacy.md)
- [ADR-023 – Authentication & Authorization Model](ADR-023-authentication-authorization-model.md) (Layer 4: device authentication)
- [ADR-049 – Edges Table for Mentions](ADR-049-edges-table-for-mentions.md)
- [ADR-063 – Graph-Canonical KnowledgeObject](ADR-063-graph-canonical-knowledge-object.md)
- [ADR-064 – Federation: Multi-Instance Object Sync](ADR-064-federation.md) (amended: Phase 3 same-owner scope decided here)
- [ADR-070 – Pipeline & Index Versioning](ADR-070-pipeline-and-index-versioning.md)
- Analyses: [requirements](../analysis/2026-08-25-device-sync-requirements.md) · [survey](../analysis/2026-08-25-device-sync-replication-survey.md) · [conflict semantics](../analysis/2026-08-25-device-sync-conflict-semantics.md) · [identity & transport](../analysis/2026-08-25-device-sync-identity-transport.md) · [storage-federation addendum](../analysis/2026-08-04-storage-federation-addendum.md) · [retraction propagation](../analysis/2026-08-04-retraction-propagation.md) · [blob federation](../analysis/2026-08-04-blob-federation.md) · [key lifecycle](../analysis/2026-08-04-key-lifecycle.md) · [cgo bifurcation](../analysis/2026-08-04-cgo-bifurcation.md) · [single-writer enforcement](../analysis/2026-08-04-single-writer-enforcement.md)
  (the 2026-08-04 analyses are maintained on `main`; they arrive alongside this set at integration)
- Prior art: CouchDB replication & revision trees; Syncthing conflict copies; iCloud/Core Data mirroring merge policies; Automerge/cr-sqlite (evaluated, not adopted); HLC (Kulkarni et al.); OR-set/counter CRDT constructions; local-first software principles (Ink & Switch)

---
