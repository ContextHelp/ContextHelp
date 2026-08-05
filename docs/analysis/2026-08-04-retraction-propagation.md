# Retraction Propagation in Federation — Current State and Protocol Design

> **Date:** 2026-08-04
> **Applies to:** dPKMS, ctxt
> **Extends:** [2026-08-04-storage-federation-addendum.md](2026-08-04-storage-federation-addendum.md)
> **References:** ADR-064, ADR-057, ADR-019, `docs/dpkms/privacy.md`

The federation addendum flagged retraction as risk #4: watermark-incremental sync handles additive replication only, with no story for a publisher who revokes access, corrects an error, or exercises deletion rights after content has replicated. This note verifies that claim against the implementation, maps exactly where the gaps are, and proposes a retraction protocol that fits the existing architecture.

## Current state: the sync stream is insert-only, and stricter than documented

**One correction to the addendum first.** It describes federation as "pull-based, watermark-incremental." The implementation (and ADR-064 itself, `docs/decisions/ADR-064-federation.md:31`) is **push-only**: workers push objects outward; pull is an explicit stub — `PullSyncer.Pull` returns `ErrPullNotImplemented` unconditionally (`internal/federation/pull.go:19-21`), deferred to Phase 3 pending a conflict-resolution ADR. This does not weaken the retraction concern; it sharpens it, because the publisher already owns the only active channel to subscribers — the right place to carry tombstones.

What actually flows through that channel:

- **The wire format has three fields and no verbs.** `FederationPushRequest` carries `Objects`, `Edges`, `Entities` (`internal/federation/remote.go:20-24`); the `Pusher` interface exposes only `Push` (`internal/federation/pusher.go:15-24`). There is no delete, no update, no revoke — the protocol cannot express anything but "here is more content."
- **Receivers only ever create.** `LocalPusher.Push` skips any object whose `ContentHash` already exists at the target, otherwise `Create`s it (`internal/federation/local.go:92-122`). The HTTP receiver `Service.FederationAccept` mirrors this exactly (`internal/service/federation.go:44-55`). Entities are `UpsertThin`; edges are `Create`-with-dup-skip. Nothing in either path deletes or overwrites.
- **The watermark is write-only.** Both pushers advance `federation_watermarks` on success (`internal/federation/local.go:147`, `internal/federation/remote.go:139-144`), but `GetWatermark` (`internal/storage/storage.go:65`) has no production caller. `Worker.tick` lists the **entire corpus every tick** — `List(ctx, ObjectFilter{Status: "all"})` with no time filter (`internal/federation/worker.go:189`) — and relies on receiver-side content-hash dedup for idempotency. Two consequences: the "incremental" property exists only as intent, and `Status: "all"` means even locally *discarded* objects federate (the filter admits `"discarded"` as a status, `internal/storage/types.go:105`). An object the user has already dispositioned as unwanted still replicates outward.
- **Corrections silently fork instead of superseding.** Because dedup keys on `ContentHash`, editing an object at the source produces a new hash, which the receiver treats as a brand-new object. The uncorrected version persists at every subscriber indefinitely. There is no supersedes link, so a subscriber's search index happily serves the retracted-in-spirit original next to the correction.
- **Registry sync is likewise upsert-only.** `Syncer.Sync` and `MultiSync` iterate the fetched index and `Upsert`/`UpsertThin` (`internal/registry/sync.go:161-201`, `307-327`); entities that have *disappeared* from a registry's index are never pruned locally. ADR-057 is the closest thing to a removal signal in the codebase — ETag-diffing detects that a *pipeline step* was removed and raises a high-priority reminder (`docs/decisions/ADR-057-registry-update-notification-model.md:116-137`) — but it is notification-only, scoped to steps not entities/objects, and mutates nothing.

## Even local deletion is incomplete

Retraction ultimately bottoms out in "subscriber purges the content," so it matters that the local delete primitive is itself partial:

- `ObjectStore.Delete` executes only `DELETE FROM objects` (`internal/storage/sqlite/objects.go:374-384`). Contrast `Update`, which explicitly maintains the FTS index (`objects.go:347-357`) and the vec/embedding rows (`objects.go:362-370`): `Delete` touches none of them. `objects_fts` is an external-content FTS5 table (`content='objects'`, `internal/storage/sqlite/migrations.go:412-416`), so deleted rows leave stale index entries that FTS5 cannot reconcile on its own; the vec0 row and embeddings blob likewise survive the object.
- `Service.DeleteObject` deletes edges then the object row (`internal/service/service.go:410-421`) — no FTS, no vector, no blob. Blob cleanup exists only as a periodic orphan sweep in housekeeping (`cmd/dpkms/cmd/housekeeping.go:334-345`).
- The privacy posture depends on this working: `docs/dpkms/privacy.md:153-160` promises users may "Delete individual bookmarks," "Purge all knowledge," and "Revoke registry access." Today the first two leave search-index and vector residue locally, and none of the three has any effect beyond the instance boundary.

**A subscriber-side "purge object" operation that atomically removes the row, its edges, FTS entry, vec/embedding rows, and dereferenced blobs is a prerequisite for any retraction protocol — and independently worth fixing for the local privacy promise.**

## Entitlements: checked once, at sync time

The addendum's claim that "entitlements are checked at sync time, not perpetually" verifies, and the reality is slightly weaker:

- The only enforcement point is a pre-sync gate: `Syncer.Sync` calls `FetchAndStore` before fetching the index when the registry declares an entitlement URL (`internal/registry/sync.go:146-151`). A 401 aborts the sync (`internal/registry/entitlement.go:78-83`).
- `CheckNamespace` — the function that actually evaluates namespace grants and expiry (`internal/registry/entitlement.go:111-142`) — **has no production caller**. Expiry (`ExpiresAt`) is therefore evaluated nowhere in a live code path. An expired or revoked entitlement blocks the *next* sync but does nothing about content already replicated, and nothing gates retrieval/search over previously-synced namespaces.

So revocation today means: the subscriber stops receiving *new* content, keeps everything it has, and never learns the grant was withdrawn.

## Retraction protocol design

The design goal is to keep the architecture's core move intact — boring storage, complexity in the protocol layer, every object with one authoritative home — and add exactly one new verb.

### 1. Tombstones in the push stream

Add a fourth field to the wire format:

```
FederationPushRequest {
  Objects     []KnowledgeObject
  Edges       []Edge
  Entities    []Entity
  Retractions []Tombstone        // new
}

Tombstone {
  ObjectID     string    // origin's id
  ContentHash  string    // dedup identity — the field receivers actually key on
  Reason       string    // "deleted" | "revoked" | "corrected"
  SupersededBy string    // content-hash of replacement; set when Reason == "corrected"
  Scope        string    // "" (single object) | namespace glob for bulk revocation
  IssuedAt     time.Time
  Signature    string    // Ed25519 over the canonical tombstone body, origin's key
}
```

Producer side: `Service.DeleteObject` writes a tombstone row (new `tombstones` table, same DB — the transactional co-location argument applies: the tombstone commits atomically with the delete) before deleting. `Worker.tick` appends pending tombstones to each push batch. `UpdateObject` writes a `corrected` tombstone for the old content-hash whenever the hash changes, fixing the silent-fork problem.

Receiver side, per tombstone:

1. Verify signature against the origin's key (the trust store, `internal/registry/trust.go`, already anchors per-source key material). Reject unsigned tombstones from key-declaring sources — a forged tombstone is a censorship primitive.
2. Resolve by `ContentHash` (fall back to `ObjectID`); invoke the atomic purge operation from the previous section: object row, edges (`EdgeStore.DeleteByObject` exists, `internal/storage/storage.go:193`), FTS row, vec row, embeddings, and blob refs whose only referent was the purged object.
3. **Persist the tombstone locally.** This is the critical semantic: the tombstone must outlive the object so that (a) a lagging or replaying peer re-pushing the same content-hash is suppressed — tombstone beats push, always — and (b) DAG fan-out works: an intermediate aggregator forwards tombstones downstream on its own next tick, exactly as it forwards objects. Without receiver-side persistence, the full-corpus-repush behavior of `Worker.tick` would resurrect every retracted object within one interval.
4. Append an audit entry (`AuditStore` is append-only by design with no-update/no-delete triggers, `internal/storage/storage.go:115-124`, `migrations/011_audit_log.sql:15-21`) — retraction is precisely the event class where "who removed what, when, on whose instruction" must survive the removal itself.

### 2. Entitlement revocation for already-replicated content

Revocation is not a per-object event — it is a grant-level event covering everything a namespace delivered. Two mechanisms, both needed:

- **Namespace-scoped tombstones** (`Scope` field above): the registry pushes one signed tombstone meaning "purge everything you hold in `acme.legal.*`." Cheap, explicit, works through DAG intermediaries.
- **Lease semantics on the subscriber**: entitlements become leases, not one-time gates. `RegistryEntitlement.ExpiresAt` already exists in storage; wire `CheckNamespace` into a periodic re-verification loop (the shape of ADR-057's `ScheduledCheckLoop` is the right template) that re-fetches `/entitlements` and — on expiry, revocation, or sustained unreachability past a grace window — quarantines then purges the affected namespaces. This is the OCSP/DNS-TTL model: access that must be periodically re-affirmed rather than granted forever. It also covers the failure mode tombstones cannot: a registry that disappears entirely can never send the revocation push.

Retrieval-time enforcement (search/serve checks the lease before returning restricted-namespace results) is the cheap interim: content lingers at rest but stops being *served* the moment the lease lapses, ahead of the purge.

### 3. Tombstone GC

Tombstones accumulate; the CRDT literature is blunt that they can only be collected once no peer can still resurrect the data (causal stability). Mapped onto this architecture:

- A tombstone must outlive the *slowest downstream consumer's* lag. Since the watermark table exists precisely to measure that, the clean rule is: GC a tombstone once every configured target's watermark exceeds its `IssuedAt` — which requires finally *reading* watermarks, not just writing them.
- For peers outside the config (a restored-from-backup subscriber, a target removed and re-added), add a fixed retention floor (e.g., 90 days) plus an **epoch marker**: a source that GCs tombstones bumps its epoch; a receiver seeing a newer epoch than its last full sync must treat its copy as potentially stale and do a reconciling full resync rather than an incremental one. This bounds the resurrection window instead of pretending to eliminate it.

### 4. Prior art this leans on

- **ActivityPub `Delete` + `Tombstone`**: the closest production analog — a `Delete` activity federates to every server that received the object; servers SHOULD replace it with a `Tombstone` so late-arriving references resolve to "gone" rather than 404. Its decade of operation also demonstrates the honest limit: delivery is best-effort and compliance is voluntary.
- **Atom deleted-entry (RFC 6721)**: `at:deleted-entry` in the feed itself — deletion as a first-class stream element carrying `ref` + `when`, the exact shape proposed here for the push batch.
- **CRDT tombstone GC**: the causal-stability requirement above; keep deletion markers until provably delivered everywhere, then collect via epoch/checkpoint agreement.
- **Certificate revocation (CRL/OCSP)**: grant-level revocation lists plus liveness-checked leases — the model for entitlement re-verification rather than per-object messaging.

## Honest limits

State them in the spec, because the privacy documentation currently implies more than any protocol can deliver:

1. **Subscribers you don't control cannot be forced to purge.** A tombstone is a signed, auditable *instruction*; a compliant implementation honors it, a hostile fork ignores it. The protocol converts "we cannot retract" into "we can prove notice was served and cut off everything renewable" — leases lapse, future syncs gate, keys rotate — but bytes already copied to an uncooperative peer are gone from the publisher's control. Same epistemic position as ActivityPub, email, and the web at large.
2. **Derived artifacts leak.** ADR-064 declares entities and search indexes "derived, rebuilt at target" (`ADR-064-federation.md:65-66`); embeddings, extracted entities, and downstream summaries computed from a retracted object are not addressed by object-hash tombstones. The purge operation should cascade to per-object derivations (edges, FTS, vectors); cross-object derivations (an entity enriched by ten sources, one now retracted) are best-effort re-derivation, and the spec should say so.
3. **Crypto-shredding is the only strong retraction** — encrypt per-namespace at rest, revoke by destroying keys (ADR-019's infrastructure points this direction). That inverts the local-first plaintext-SQLite design and is out of scope here, but it is the honest answer for content whose retraction must be *guaranteed* rather than requested, and worth naming as the future escalation path for regulated deployments.

## Sequencing

1. **Atomic local purge** (object + edges + FTS + vec + embeddings + blobs) — fixes the local privacy promise today; prerequisite for everything else.
2. **Stop federating discarded objects** — one-line filter change in `Worker.tick`; also the moment to implement real watermark-incremental listing, which the tombstone GC rule depends on.
3. **Tombstone type + wire field + receiver purge/persist/forward** — the protocol core; ship behind the existing push endpoint (additive JSON field, old receivers ignore it — acceptable only during pre-1.0; version the endpoint at spec time).
4. **Entitlement leases** — wire `CheckNamespace` into scheduled re-verification with quarantine-then-purge; retrieval-time gating as the interim.
5. **Tombstone GC + epoch resync** — after watermark reads exist and real deployments show actual peer-lag distributions.

The addendum's judgment stands: this must be specified before public instances exist, because every element above is a *receiver-side* behavior — and receivers, once deployed beyond one's control, are exactly the thing that cannot be retrofitted.
