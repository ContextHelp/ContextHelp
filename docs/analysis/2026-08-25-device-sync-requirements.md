# Same-Owner Device Sync — Requirements and Threat Model

> **Date:** 2026-08-25
> **Applies to:** dPKMS, ctxt
> **References:** ADR-001, ADR-019, ADR-023, ADR-064, `docs/ctxt/user-stories.md` (story 70),
> [2026-08-04-storage-federation-addendum.md](2026-08-04-storage-federation-addendum.md),
> [2026-08-04-retraction-propagation.md](2026-08-04-retraction-propagation.md),
> [2026-08-04-blob-federation.md](2026-08-04-blob-federation.md),
> `docs/architecture/ambient-capture.md`

Story 70: *"As a User I want to sync my knowledge store across my own devices (local-first replication) without requiring a central server"* (`docs/ctxt/user-stories.md:97`). ADR-001 deferred this explicitly ("Multi-device sync must be designed later — optional, user-controlled"); ADR-064 deferred the hard half again ("Pull support (Phase 3) will need conflict resolution strategy; ADR to follow"). This note defines what the deferred design actually has to deliver, and what it has to survive. It is the requirements input to the replication survey, the conflict-semantics note, and the eventual ADR.

## 1. A third replication mode

The system now has three distinct replication problems, and conflating them is how this design would fail:

| Mode | Writers | Authority | Conflicts | Status |
|---|---|---|---|---|
| **Federation** (ADR-064) | one — the origin | every object has one authoritative home; subscribers hold derived copies ([2026-08-04-storage-federation-addendum.md](2026-08-04-storage-federation-addendum.md), "The bet, restated") | avoided by construction (push-only) | Phase 1 shipped |
| **Backup/restore** | one — the instance | the live instance; backups are dead state | none — restore replaces | shipped, incomplete (story 73) |
| **Device sync** (this design) | **all replicas** — 2–5 devices, one owner | **none / negotiated** — no device is structurally primary | **the central problem** | undesigned |

Device sync is not federation with more trust. Federation's whole architecture rests on single-ownership-per-object; device sync breaks exactly that invariant: the same owner edits the same object on two devices while both are offline, and both edits are legitimate. The federation addendum was explicit that push-only "sidesteps CRDTs and multi-master conflict resolution entirely" — device sync is the mode where that bill comes due.

### Two topologies, one protocol

The design must serve two deployment shapes:

**T1 — Hub: remote always-on primary + local fallback (the priority deployment).** An owner-hosted, internet-reachable dpkms instance is the designated primary — the always-on replica every other device syncs against; "primary" is availability and routing, never merge authority (binding requirement below). This is not a hypothetical: `docs/architecture/ambient-capture.md` already treats "dpkms is often remote" as the substrate's central constraint, with `ctxd` on the local machine buffering enqueues toward a remote dpkms. The hub composes with the inbound-auth surface (ADR-023 Layer 2 token auth and Layer 4 device authentication — one auth seam; transport encryption is ADR-019 Layer 3) because it is by definition network-exposed. A local instance mirrors the hub while connected, takes writes while disconnected (capture *and* curation — status changes, tags, edits), and reconciles on reconnect.

**T2 — Mesh: 2–5 peer devices, no primary.** Laptop + desktop + phone, syncing over LAN, an untrusted relay, or file transfer, possibly never all online simultaneously.

The binding requirement: **T1 and T2 run the same wire protocol and the same merge semantics.** "Primary" in T1 is a routing and availability role — the always-reachable peer, the default conflict-review surface, the backup anchor — never a semantic role in merge. Two reasons:

1. A hub that wins conflicts *because it is the hub* silently discards offline work on the fallback device — precisely the data loss story 70 exists to prevent. The hub is stale with respect to a disconnected laptop by construction.
2. If hub-sync and peer-sync are different protocols, every convergence property must be proven twice, and the T2 mesh (which strictly contains T1 as the star graph) never gets built. T1 = T2 with one well-provisioned peer.

### "Without requiring a central server," precisely

Story 70's constraint is about *required third-party infrastructure*, not about the owner running a server:

- **Permitted:** an owner-hosted hub (T1). It is a full dpkms instance — it holds plaintext, because it runs pipelines, FTS, and vector search like any instance. Its protections are the instance protections: ADR-019 encryption at rest, ADR-023 inbound auth, the daemon-as-policy-boundary rule ([2026-08-04-storage-federation-addendum.md](2026-08-04-storage-federation-addendum.md), risk 1).
- **Permitted:** an untrusted relay for store-and-forward between devices that are never concurrently online. The relay sees **ciphertext and queue metadata only** — never plaintext, never keys. It must be self-hostable and optional.
- **Forbidden:** any design where sync *requires* infrastructure the owner does not control, or where any non-owned component can read content. Sync must also work with zero servers at all: two devices on one LAN, or a file carried on a stick, are complete transports.

## 2. Why the existing federation machinery cannot satisfy this

Stated explicitly, with the implementation as evidence, so the survey does not relitigate it:

1. **Push-only, structurally.** The `Pusher` interface exposes `Push` and nothing else (`internal/federation/pusher.go`); pull is an unconditional stub — `PullSyncer.Pull` returns `ErrPullNotImplemented` (`internal/federation/pull.go:11`). A fallback device can push its offline writes to the hub but can never receive the hub's state. Half of "mirror while connected" is unimplemented by design.
2. **The wire format is insert-only and has no verbs.** `FederationPushRequest` carries `Objects`, `Edges`, `Entities` (`internal/federation/remote.go:21-23`) — no update, no delete, no tombstone. A curation action (discard, retag, edit) either does not propagate or propagates as a brand-new object.
3. **Content-hash dedup makes edits fork silently.** Receivers skip any object whose `ContentHash` already exists, else create (`internal/federation/local.go:94-100`). Editing an object changes its hash, so the receiver keeps the stale version *and* gains the edited one as an unrelated row ([2026-08-04-retraction-propagation.md](2026-08-04-retraction-propagation.md), "Corrections silently fork"). For device sync this is fatal: every reconciliation after offline editing would duplicate the corpus's edited slice.
4. **No deletion story.** No tombstones anywhere in the stream; even the local delete primitive leaves FTS/vector residue (same note, "Even local deletion is incomplete"). Device sync without tombstones means a discard on one device resurrects from every other device forever.
5. **Full-corpus scans per tick.** `Worker.tick` lists the entire corpus with `ObjectFilter{Status: "all"}` (`internal/federation/worker.go:189`) and leans on receiver-side dedup for idempotency. Acceptable for a LAN push target; disqualifying for a phone on a metered connection syncing a 10⁵-object corpus.
6. **Watermarks are written, never read** (`internal/federation/local.go:147`; no production caller of `GetWatermark`). The "incremental" in watermark-incremental sync exists as intent only — and device sync needs real incrementality plus *per-peer acknowledged state*, which is a stronger structure (see acceptance criteria).

What device sync **should reuse** from federation and its analyses: the transactional co-location argument (sync state commits atomically with content, in the same SQLite file — the addendum's strongest point), Ed25519 signing and the org-root→instance-key delegation shape ([2026-08-04-key-lifecycle.md](2026-08-04-key-lifecycle.md)), the tombstone design and causal-stability GC rule from the retraction note, and content-addressed blob keys ([2026-08-04-blob-federation.md](2026-08-04-blob-federation.md), Option C). Shared concepts, separate protocol.

## 3. Offline model

- **Disconnection is a normal state, not a failure.** Devices diverge for hours (commute), days (travel), weeks (a desktop left off). The protocol must not degrade with divergence duration — only reconciliation *volume* may grow.
- **All replicas take writes while disconnected**: capture (new objects — the dominant case, per the append-mostly corpus), curation (status transitions, tags, edge additions), edits (content mutation — rare but the hard case), deletions.
- **Convergence requirement:** after any finite set of writes on any devices and enough pairwise sync to connect the set, all devices reach *identical* state — same objects, same field values, same edges, same tombstones. This requires merge to be commutative, associative, and idempotent over the operation set (order- and pairing-insensitive), because T2 gives no ordering guarantees: A↔B, then B↔C, then C↔A must equal any other schedule.
- **No data loss by default:** when a deterministic merge rule must pick a winner between two concurrent legitimate writes, the loser is *preserved and surfaced* — payload included, in a structure the winner's deletion cannot cascade away (conflict copy / parking table; semantics in the conflict note) — never silently discarded. Deterministic-and-lossy is acceptable only for values that are cheap to regenerate or semantically idempotent.
- **Partial sync is safe:** a sync interrupted at any byte leaves both sides consistent (possibly behind, never corrupt) and resumable. This falls out of the transactional co-location discipline if sync state and applied changes commit together.
- **Interplay with ctxd buffering:** ambient capture already tolerates dpkms-unreachable by spooling in `ctxd`'s buffer (`docs/architecture/ambient-capture.md`, "Replay safety"). That solves *capture* offline against a single remote instance. Device sync solves *the store itself* being multi-sited: with a local instance present, `ctxd` enqueues locally and sync moves knowledge between instances. The two compose; neither replaces the other.

## 4. Transport requirements

Detailed design in the identity/pairing/transport note; requirements here:

| Tier | Path | Requirement |
|---|---|---|
| 1 | Direct to hub (internet) | TLS + mutual device authentication (composes with ADR-023 inbound auth); survives NAT — device dials hub, never the reverse |
| 2 | LAN peer-to-peer | mDNS discovery + mutually authenticated encrypted channel; no dependency on internet reachability |
| 3 | Untrusted relay | store-and-forward of *encrypted* sync envelopes; relay learns only (device-pseudonym, size, timing); self-hostable; queue survives relay restarts |
| 4 | File / sneakernet | a signed, encrypted sync bundle importable out-of-band; the degenerate transport that proves the protocol is state-based at the edges (no live session required) |

All tiers carry the same sync payload. Session resumption across network changes (laptop sleeps, IP changes) is required on tiers 1–2; tiers 3–4 are resumable by construction.

## 5. Threat model

Assets: knowledge corpus (objects, edges, entities), blobs, device keys, the device roster, metadata (who syncs when, corpus size). One owner; devices are mutually *trusting* once paired but must be mutually *authenticated* and individually revocable.

| # | Adversary / event | Attack surface | Design obligation |
|---|---|---|---|
| S1 | **Stolen or lost device** | full replica + resident keys | Per-device keys (never a shared owner secret on every device) so one device is revocable without rekeying the world; revocation propagates through sync and takes effect at next contact; at-rest encryption (ADR-019) is the only protection for bytes already on the device — state this honestly. A revoked device must be excluded from tombstone-GC quorum (else its absence blocks GC forever, or its return resurrects). |
| S2 | **Relay operator** (tier 3) | stored envelopes, traffic metadata | End-to-end encryption between devices; relay holds ciphertext only; authenticated encryption prevents tampering/reordering within a stream; owner accepts metadata leakage (sizes, timing, pseudonyms) — document it. Relay can drop/delay traffic: liveness failure only, never integrity. |
| S3 | **Network attacker on LAN** (tier 2) | discovery + channel | mDNS is unauthenticated by nature — discovery is a *hint*, never trust; channel established only between keys already paired; MITM on first pairing defeated by the pairing ceremony (short authenticated string / QR), not by the channel. |
| S4 | **Stale replica resurrection** | tombstone GC window | A device offline longer than the tombstone retention floor must not re-introduce deleted content when it returns. Requires: tombstones outlive causal stability across the *device roster* (small and enumerable — the advantage over open federation), plus an epoch rule: a device returning past the GC horizon reconciles via full resync in which tombstone-surviving state wins ([2026-08-04-retraction-propagation.md](2026-08-04-retraction-propagation.md) §3 has the epoch mechanism). Same rule covers restore-from-old-backup, which is a stale replica wearing a valid key. |
| S5 | **Malicious peer impersonating an owned device** | pairing + transport auth | Membership in the replica set is cryptographic: a device is in the roster iff it holds a key delegated by the owner root (key-lifecycle delegation shape). No "same LAN," "knows the hub URL," or "presents a bearer token" shortcut admits a writer. Every sync payload attributable to a device key; unsigned or unknown-key payloads rejected. |
| S6 | **Internet attacker vs. the hub** (T1) | exposed API | The hub is a public-bind dpkms instance: daemon-only policy boundary mandatory (federation addendum risk 1), ADR-023 auth on every endpoint; sync endpoints additionally require *device* authentication — a device-auth Provider on the same inbound-auth seam, yielding a principal whose roster status gates replica membership (identity/transport note §3.2) — so a leaked API token must not grant replica-set membership. |
| S7 | **Compromised device (active)** | everything — it is a legitimate writer | Out of scope to *prevent* (a paired device is trusted by definition); in scope to *bound*: per-device signatures make every write attributable, the audit log (append-only, `internal/storage/sqlite/migrations/011_audit_log.sql`) records sync application, and revocation cuts future writes. State plainly: device sync widens the blast radius of one compromised endpoint from one replica to all — this is inherent to the feature. |

Non-threats (explicitly): multi-owner collaboration (separate problem, separate trust model); Byzantine peers inside the replica set beyond attribution/revocation; hiding corpus *existence* from a device the owner paired.

## 6. Non-functional requirements

- **Scale envelope:** design for 10⁴–10⁶ objects, 3–10× that in edges, 10³–10⁵ entities; blobs to tens of GB with individual blobs ≥64 KiB by definition (`internal/config/config.go` externalize threshold). Steady-state divergence between two active devices: 10¹–10³ ops/day.
- **Sync cost proportional to divergence, never to corpus size.** No full-corpus enumeration on any periodic path (the anti-pattern is `worker.go:189`). Reconciliation after a week offline: minutes, not hours; steady-state connected sync: seconds.
- **Metadata-first, blobs lazy.** A phone must be able to hold the full graph (rows) while fetching blob bytes on demand / by policy — the requirement blob-federation Option C's content-addressed keys make cheap (dedup by byte hash, verify by re-hash). Full blob mirroring is a per-device policy choice, on by default for desktop peers and the hub.
- **Battery/bandwidth:** delta transfer only; batched and schedulable (piggyback on power/Wi-Fi conditions on mobile); compression on the wire; no persistent chatter when idle (event-driven or coarse polling, not tight loops).
- **Derived state never crosses the wire as truth.** FTS, vec0 rows, embeddings are rebuilt locally per ADR-064's derived-artifact rule; anything derived that does travel is a cache hint, and correctness never depends on it. Consequence: post-sync index rebuild cost must be incremental (per applied object), not corpus-global.
- **Observability:** per-peer sync status (last contact, acked watermark, pending ops, conflicts surfaced) via CLI/health surface; sync failures are loud, attributable, and non-corrupting.

## 7. Acceptance criteria for the design

The eventual design (survey → conflict semantics → ADR) must satisfy every line:

- [ ] **AC1 — Symmetric multi-writer:** any of 2–5 paired devices accepts writes at any time, connected or not; no structurally primary replica in merge semantics.
- [ ] **AC2 — T1 served:** an internet-reachable owner-hosted hub + local fallback works with the *same* protocol: local mirrors hub while connected, takes writes disconnected, reconciles on reconnect with no silent loss on either side.
- [ ] **AC3 — Convergence:** after quiescence and pairwise-connected syncs in any order/pairing, all replicas are byte-identical on synced state (objects, edges, entities, tombstones). Deterministic; no coordinator.
- [ ] **AC4 — Loss surfacing:** any merge that discards a concurrent write's value preserves the loser in a user-visible place; the rule for every field/type is written down in the conflict-semantics note.
- [ ] **AC5 — Deletion sticks:** a delete/discard on one device propagates to all; no resurrection by lagging replicas within the retention horizon; returning past the horizon triggers epoch reconciliation (S4).
- [ ] **AC6 — Edits supersede:** an edit propagates as a supersession of the same logical object — never as a content-hash fork (kills failure mode §2.3).
- [ ] **AC7 — Membership is cryptographic:** pairing ceremony + per-device keys + owner-root delegation; unpair/revoke removes a device's write authority and its standing in GC quorum (S1, S5).
- [ ] **AC8 — Transport-complete:** works over direct-to-hub, LAN, untrusted relay (ciphertext-only), and file transfer; no plaintext at any non-owned component; no required third-party service.
- [ ] **AC9 — Incremental:** wire cost O(divergence); no full-corpus scans; interrupted syncs resume; sync state commits atomically with applied content in the same DB.
- [ ] **AC10 — Derived-state invariant:** FTS/vector/embeddings excluded from replicated truth; local rebuild is incremental per applied change.
- [ ] **AC11 — Blob laziness:** row sync completes without blob bytes; blob fetch is by content-addressed key, verifiable at the receiver, dedupable across devices.
- [ ] **AC12 — Auditability:** every applied remote op is attributable to a device key and recorded through the append-only audit path.

## Open questions carried forward

1. Merge rules per type/field — the whole next note (survey scores the mechanisms; conflict note fixes semantics).
2. Whether the hub in T1 doubles as the tier-3 relay for device pairs that cannot reach each other (attractive: one deployment; risk: hub outage degrades both roles).
3. Tombstone retention floor default (retraction note suggests 90 days for federation; same-owner roster may justify shorter, since causal stability is computable exactly over a known roster).
4. Whether entity rows replicate or re-derive (they are cheap to re-derive but carry curated enrichment — split decision expected; conflict note owns it).
