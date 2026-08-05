# Blob Federation — What Crosses the Wire, and How Large Content Should

> **Date:** 2026-08-04
> **Applies to:** dPKMS, ctxt
> **Extends:** [2026-08-04-storage-federation-addendum.md](2026-08-04-storage-federation-addendum.md) (risk #3: "Blob references do not federate")
> **Prerequisite:** [2026-08-04-blob-referential-integrity.md](2026-08-04-blob-referential-integrity.md)
> **Companion:** [2026-08-04-retraction-propagation.md](2026-08-04-retraction-propagation.md)

The federation addendum called `blob://` the least-specified seam in the federation story. This note verifies exactly what crosses the wire today for an externalized object, enumerates every sync/export surface that inherits the problem, evaluates the three candidate designs against the actual code, and recommends one — ADR-shaped, since the outcome is a protocol decision, not a bug fix. The headline findings:

1. **Federation ships the ref and never the bytes.** The push wire format has no notion of blobs; an externalized object arrives at the subscriber as a row whose `raw_content` is the literal string `blob://<key>`, stored verbatim. Every remote push of a >64 KiB object manufactures a dangling ref at the receiver, deterministically.
2. **The bundle machinery cannot dangle yet because it carries no knowledge at all.** `internal/bundle/` is config-bundle-only today; signed knowledge packs (user story 54) are unimplemented. But the pack format will be built on this machinery, and a naive "zip the object JSON" pack would dangle identically.
3. **The blob key is not actually a content address.** `ContentHash(content, source)` hashes *normalized* (lowercased, whitespace-collapsed) content plus a source component — so the key is neither verifiable by hashing the stored bytes nor stable across instances that ingested the same bytes from different sources. Both properties are exactly what a federation design needs, and both are one decoupling away.

The saving grace, again inherited from the referential-integrity analysis: nothing dereferences `blob://` on reads (`blob.Resolve` has zero callers outside its package, `internal/storage/blob/resolve.go:29`), so today's cross-instance dangling refs are asymptomatic. That is a closing window, not a mitigation — the moment a resolver ships, every federated subscriber discovers its corpus is hollow.

## What actually crosses the wire

### Federation push: the row, whole and hollow

The push payload is three arrays and nothing else: `FederationPushRequest{Objects, Edges, Entities}` (`internal/federation/remote.go:20-24`), JSON-marshaled (`remote.go:96`) to `POST /api/v1/federation/push` (`remote.go:101`). The `Pusher` interface exposes the same three-array `Push` and no other verb (`internal/federation/pusher.go:15-24`). `storage.KnowledgeObject` serializes `raw_content` as a plain string field (`pkg/pluginapi/pluginapi.go:78`), and for an externalized object that string is `blob://<hash>` — the externalize step rewrote it at ingest time (`internal/pipeline/steps/externalize.go:61`) after `Put`ting the real bytes into the instance-local blob store (`externalize.go:45`).

So a >64 KiB object (threshold default 65536, `internal/config/config.go:790`) crosses the wire as:

- `raw_content: "blob://<key>"` — a ref into a store the receiver does not have;
- `metadata.blob_key`, `metadata.blob_original_size`, `metadata.blob_content_type` (`externalize.go:57-59`) — faithfully describing bytes the receiver also does not have;
- `text_content` (`pluginapi.go:79`) and `embeddings` (`pluginapi.go:88`) — which *do* travel, so search over the federated copy partially works. This is why the hole is easy to miss: the object is functional for retrieval and hollow for content.

The receiver stores all of it verbatim. `Service.FederationAccept` dedups by `ContentHash` then `Create`s the row as-is (`internal/service/federation.go:44-55`); the HTTP handler adds only Bearer-token auth (`internal/server/http/handlers_federation.go:18-24`) before delegating (`handlers_federation.go:33`). `LocalPusher.Push` is the same shape against a target DB opened by file path (`internal/federation/local.go:94-105`). No code on either side inspects `raw_content` for the `blob://` prefix — `blob.IsBlobRef` (`resolve.go:14`) is never consulted on any sync path.

One aggravation worth naming: because `Worker.tick` pushes the entire corpus every tick (`internal/federation/worker.go:189`) and receivers dedup by content hash, the dangling row is also *permanent* — the receiver's copy is created exactly once and never revisited, so even a future fix that enriches the push format will not retroactively heal rows already accepted.

### Bundles: config-only today, same trap tomorrow

`internal/bundle/` implements Ed25519-signed **config** bundles — the package doc comment spells out the flow (`ctxt config backup` → zip config files → sign → `.zip` + `.sig` sidecar, `internal/bundle/bundle.go:1-9`), and `BuildOpts` takes filenames within a `ConfigDir` (`bundle.go:146-158`). No knowledge objects, therefore no blob problem — yet. User story 54 ("export and import signed knowledge packs (bundles) so I can share or archive verifiable context snapshots", `docs/ctxt/user-stories.md:73`) is the obvious next consumer of this machinery, and the machinery is content-agnostic: `Build` signs whatever bytes go into the zip (`bundle.go:175-246`). A pack that zips object JSON without resolving `blob://` refs produces a *signed, verifiable* archive of dangling pointers — the signature would attest to the hollowness.

The nearer-term leak is `ctxt export` (`cmd/ctxt/cmd/export.go`): it fetches the object (`export.go:74-77`) and hands it to an output-generator plugin or falls back to JSON. An externalized object exports with its literal ref — an Obsidian vault entry whose body reads `blob://3fa4…` is the first place a user will actually *see* this seam.

## Affected surfaces, enumerated

| Surface | Mechanism | Blob outcome today | Evidence |
|---|---|---|---|
| Federation push, remote (HTTP) | Full rows as JSON | Dangling ref at receiver, permanent (dedup never revisits) | `remote.go:96`, `federation.go:52` |
| Federation push, local (file-path target) | Rows copied into target DB | Dangling unless both instances happen to share a blob directory; nothing checks | `local.go:94-122` |
| Inline federation (capture-time targets) | Same wire format, same pushers | Same as above | `worker.go:74-77` (inline entries skipped by async set; capture-time path shares `Pusher`) |
| Future pull sync | Stub — `ErrPullNotImplemented` | Will inherit whatever the push format decides; a pull-based fetch is actually the natural shape for blobs (see Option B) | `internal/federation/pull.go:19-21` |
| Signed knowledge packs (story 54) | Unimplemented; will build on `internal/bundle` | Would dangle if packs serialize object JSON naively | `bundle.go:146-158`, `docs/ctxt/user-stories.md:73` |
| `ctxt export` / output plugins | Object JSON → generator plugin | Literal `blob://` string in exported artifact | `cmd/ctxt/cmd/export.go:74-80` |
| Backup/restore cross-machine (story 70 device sync) | Tar of DB + optionally local blobs; S3 blobs never captured | DB-only restore or S3-backed instance restores rows with refs that resolve nowhere | `internal/service/backup.go:145-176`, `internal/service/restore.go:110-123`, `docs/ctxt/user-stories.md:97` |

Story 70 deserves the emphasis: "sync my knowledge store across my own devices (local-first replication)" is the *friendliest possible* federation case — same owner, same keys, full trust — and it hits this wall immediately for any large object, whether the user syncs via local-path federation targets, backup/restore, or a future pull. There is no adversarial or multi-tenant complexity to blame; the gap is purely that bytes >64 KiB have no transport.

## The key question first: is `blob://<key>` a content address?

Almost, and the "almost" matters more than it looks. The key is `ContentHash(rawContent, source)` (`externalize.go:43`) = SHA-256 over `normalize(content) + "\x00" + source`, where `normalize` trims, lowercases, and collapses whitespace (`internal/storageutil/content_hash.go:12-24`). Two impurities:

1. **The source component breaks cross-instance convergence.** Identical bytes ingested from `source: "slack"` on instance A and `source: "web"` on instance B get different keys. Federated dedup of blob *bytes* is therefore impossible by construction, and a fetch-by-key protocol cannot treat the key as naming the content — it names a (content, provenance) pair. The source does travel in the row (`pluginapi.go:90`), so a receiver *could* verify a key, but only via the row, which defeats self-certification.
2. **Normalization means the key is not a hash of the stored bytes.** `Put` stores the raw, un-normalized content (`externalize.go:45`) while the key hashes the normalized form. Consequences: (a) integrity verification requires re-running `normalize()` — hashing the received bytes directly never matches; (b) the map is lossy — two byte-different contents (case or whitespace variants) from the same source share a key, and a second `Put` silently overwrites the first's bytes (the local backend writes directly at the final path, `internal/storage/blob/local/store.go:44-52`), so the key cannot certify *which* variant it stores; (c) even `BlobMeta.ContentHash` records this same non-byte hash (`externalize.go:48`), so no stored artifact anywhere holds a true digest of the blob's bytes.

Note the row-level dedup identity does **not** depend on the blob key's format. The DB `content_hash` is computed over the *ref string* after externalization (`internal/jobs/worker.go:253`, per the referential-integrity analysis), so it converges as long as the ref is deterministic — which it remains under any deterministic key scheme. The blob key and the row dedup hash are separable concerns that today happen to share a function. That separability is the design opening.

## Options

### Option A — origin-fetch (presigned URLs)

The S3 backend already produces presigned GETs: `Store.URL` (`internal/storage/blob/s3/store.go:219-230`), default expiry one hour (`s3/store.go:60-62`), and `URL` is part of the `BlobStore` interface (`internal/storage/storage.go:133`). Ship the URL (or an origin fetch endpoint) with the push; subscribers resolve lazily.

Why it fails as the reference semantics:

- **Backend coverage.** Only S3 yields a network-usable URL. The local backend's `URL` returns an absolute *filesystem path* (`local/store.go:123-131`) — meaningless across machines — and the stub errors (`internal/storage/blob/stub/store.go:30-32`). The default backend is local (`internal/storage/blob/factory.go:16-18`), so the common deployment has nothing to hand out.
- **Availability coupling.** A subscriber's corpus becomes readable only while the origin (and the origin's bucket) is up, reachable, and still hosting the key. This is a direct contradiction of the local-first commitment the addendum treats as load-bearing, and of story 70's "without requiring a central server."
- **Expiry vs. lag.** A one-hour presigned URL embedded in a push payload is stale before a subscriber's first lazy read in any realistic usage pattern. Fresh-URL-on-demand requires an origin API round trip per read — availability coupling again, now on the hot path.
- **Entitlement leakage.** A presigned URL is a bearer capability; anyone it is forwarded to can fetch, outside any entitlement check (`internal/registry/entitlement.go` has no purchase on an S3 GET). The addendum's own requirement — "entitlement checks must then cover blob fetches too" — is unenforceable through capability URLs the origin no longer controls.
- **Fragile origin state.** The origin's own `Exists` swallows errors into `false` (`s3/store.go:165-175`) and its `List` caps at 1000 keys unpaginated (`s3/store.go:187-190`); building remote availability promises on that plumbing inverts the dependency the referential-integrity analysis says to fix first.

Verdict: acceptable as a *transport optimization* (a subscriber that happens to share bucket access, e.g. story 70 devices pointed at the same R2 bucket, can skip transfer), never as the semantics of record.

### Option B — blob replication with the push (inline or sidecar)

Bytes move to the subscriber; the subscriber's blob store becomes self-sufficient. Two sub-shapes:

- **Inline** (base64 field per object in the push JSON): correct semantics, wrong mechanics at the margins. Every externalized blob is by definition >64 KiB; base64 adds ~33%; `Worker.tick` batches the full corpus (`worker.go:189`) against a 30-second client timeout (`remote.go:52`). A corpus with a few hundred large objects blows the request budget on re-sends of content the receiver already holds (the receiver dedups rows *after* parsing the body — bytes are shipped then discarded).
- **Sidecar** (blob endpoints beside the push): push carries a *manifest* of blob keys; the receiver reports which it is missing; the pusher transfers only those via a dedicated endpoint, then pushes the rows. Transfer is incremental by content, integrity-checkable per blob, and entitlement enforcement has a natural chokepoint (the blob endpoint, under the same auth as `federation/push` — which today is Bearer-token-only, `handlers_federation.go:18-24`; the blob endpoints must not ship weaker).

Direction matters: federation is push-only precisely because origins may be NAT'd laptops with no reachable server. Any design where the *receiver* fetches from the *origin* (receiver-pull) silently assumes origin reachability — the same assumption Option A makes. The sidecar must therefore be pusher-driven: the pusher PUTs missing blobs to the receiver, not the other way around.

Ordering matters too, and the referential-integrity analysis already derived the rule: **blobs before rows**. A crash between blob transfer and row push leaves an orphan blob at the receiver — recoverable, and exactly what the mark-and-sweep reconciliation job collects. Rows-first would manufacture the dangling refs this whole design exists to prevent.

Cost: storage is duplicated per subscriber. That is not a defect; it is local-first working as specified. Per-subscriber quotas and an opt-out ("accept refs without bytes" for archival aggregators) are policy knobs, not architecture.

### Option C — content-addressed keys as the foundation

Fix the two impurities: blob key = SHA-256 of the exact stored bytes, full stop. Keep the row-level `ContentHash(content, source)` untouched for dedup/reinforcement semantics — as shown above, the row hash is computed over the ref string and survives any key scheme. Decoupling the two yields:

- **Self-certifying transfer.** A receiver verifies any received blob by hashing it — no source needed, no `normalize()` reproduction, no trust in the sender. This is the property that makes sidecar replication (Option B) safe against both malicious peers and the origin's own corrupt-blob failure mode (truncated local `Put`, referential-integrity matrix row 1): a corrupt blob fails verification at the receiver instead of propagating.
- **Cross-instance dedup.** Same bytes → same key everywhere, regardless of source or normalization variants. The manifest/missing-keys handshake in Option B becomes maximally effective, and story 70's device sync transfers each blob once.
- **Bundle integrity for free.** A signed pack whose zip contains `blobs/<key>` entries is verifiable blob-by-blob against the keys the object rows reference, under the existing signature over the zip bytes (`bundle.go:233-234`).

The trade: **1:1 row↔blob cardinality becomes 1:N.** Today's exactness (one row per hash per instance, because the key embeds source) gave the referential-integrity design its "eager delete is always safe" property. Under pure byte keys, two rows with different sources but identical bytes share a blob — locally rare, but *structural* at federation receivers aggregating multiple origins. Eager blob deletion must therefore check for other referents before deleting (one indexed query on the ref string), and the sweep invariant ("live iff some row references it") remains exact but the sweeper becomes the primary cleanup rather than a backstop. This is a real, bounded cost, and it is the correct trade: federation makes shared bytes a feature (dedup) rather than an accident.

Migration is mechanical because cardinality today is 1:1: for each `blob://` row, read the blob, hash the bytes, `Put` under the new key, rewrite `raw_content` and `metadata.blob_key`, delete the old key. The row's `content_hash` changes (it hashes the ref string) — acceptable locally; for already-federated rows the receiver's copy is frozen anyway (dedup never revisits), which is one more reason to land this before instances federate in earnest.

## Recommendation

**Hybrid: Option C as the foundation, Option B (sidecar, pusher-driven, inline-under-threshold) as the transport, Option A demoted to an opportunistic optimization.** Concretely, in landing order:

### 1. Re-key blobs to pure byte hashes (Option C)

`key = hex(SHA-256(bytes))`; store the true byte digest in `BlobMeta.ContentHash`; leave `storageutil.ContentHash` and all row-level dedup untouched. Ship the migration with the atomic-local-`Put` fix from the referential-integrity note — re-keying rewrites every blob anyway, and a verify pass (re-hash against key) comes free with the new key semantics.

### 2. Extend the push protocol with a blob manifest + sidecar transfer (Option B)

Wire changes, versioned alongside the retraction protocol's fourth field so receivers negotiate once:

```
FederationPushRequest {
  Objects, Edges, Entities            // unchanged
  BlobManifest []BlobManifestEntry    // new: one per blob:// ref in Objects
}
BlobManifestEntry { Key, Size, ContentType string }

POST /api/v1/federation/blobs/check   {keys: [...]}  → {missing: [...]}
PUT  /api/v1/federation/blobs/{key}                  → 201 (or 409 on hash mismatch)
```

Pusher flow per tick: collect `blob://` keys in the batch → `check` → PUT each missing blob → push rows. **Blobs strictly before rows**; a mid-sequence crash leaves receiver-side orphans for the reconciliation sweep, never dangling refs. Blobs at or below an inline threshold (suggest 256 KiB) may instead ride base64-inline in the push request to spare round trips; the receiver treats inline and sidecar identically past decode.

Receiver obligations, in order:
1. **Verify before Put:** hash received bytes; mismatch with the claimed key → reject (409), log, count. Never store unverifiable bytes under a content key.
2. **Enforce entitlements at the blob endpoints** with at least the push token check (`handlers_federation.go:18-24`), and — once granular grants exist — authorize a blob operation iff the caller is entitled to at least one object referencing that key. The blob endpoint is the chokepoint Option A structurally lacks.
3. **Reject hollow pushes going forward:** once a peer advertises manifest support, an object whose `raw_content` is a `blob://` ref with no corresponding manifest entry (and no local copy) is a protocol error, not a row to store. This is the guard that stops the dangling-ref class permanently instead of case-by-case.

### 3. Knowledge packs carry their blobs (story 54)

Pack format on the existing bundle machinery: zip of `manifest.json` (extended with object/edge/entity counts and the blob key list), `objects.json`, `edges.json`, `entities.json`, `blobs/<key>` entries; one signature over the zip as today (`bundle.go:233-234`). Import verifies signature, then each blob against its key, then applies the same accept path as federation (dedup, verify-before-Put). A pack missing referenced blobs fails import unless explicitly overridden — the override exists for text-only archival, and the imported rows must then mark degraded content status rather than store a silently dangling ref.

### 4. Retraction interplay

The retraction protocol's receiver-side purge should delete a tombstoned object's blob **iff no other live row references the key** — the 1:N consequence of Option C. Tombstones retract *rows*, not *bytes*: if the same bytes later arrive referenced by a new, entitled, non-tombstoned object, storing them again is legitimate (the tombstone suppresses the retracted content-hash row, which is the identity that matters). Purge ordering mirrors ingestion inverted: row first, blob second — a crash between the two leaves an orphan for the sweep, never a dangling ref. The audit entry appended on purge should record the blob key and whether bytes were deleted or retained for other referents.

### 5. Origin-fetch as opportunistic optimization only (Option A)

Where pusher and receiver share bucket access (story 70 devices on one R2 bucket), the `check` handshake already returns "nothing missing" and no bytes move — shared-backend optimization falls out of the design with zero presigned-URL machinery. Presigned URLs remain what they are today: a local convenience API, kept off the federation path.

## What this deliberately does not solve

- **Raw-mode oversized objects** bypass externalization entirely and travel inline in the row (`internal/service/service.go:154-188`, per the referential-integrity note) — they federate *correctly* today, just expensively. No change.
- **Backup/restore completeness** (S3 blobs never captured, `backup.go:145-176`) is adjacent but separate; it becomes *more* urgent under Option C since restore-then-federate of a hollow instance would now be rejected by peers rather than silently accepted.
- **Bandwidth scheduling** (throttling sidecar transfers, resumable PUTs for very large blobs) is deferred until real corpus sizes demand it; the manifest handshake bounds transfer to missing content, which is the dominant saving.

## Verdict

The addendum's instinct was right and understated: `blob://` is not merely under-specified for federation — the current wire format *guarantees* every large federated object arrives broken, permanently, on every surface that syncs rows (push today; packs, pull, and device sync tomorrow). The fix decomposes cleanly because the codebase already separated the two identities that matter: the row's dedup hash (which must keep its source component and normalization) and the blob's storage key (which must lose both). Make the key a true content address, move bytes pusher-driven behind a manifest handshake with verify-before-Put, and both federation and signed packs inherit integrity from the key itself — while entitlements gain the enforcement chokepoint that origin-fetch designs can never provide. Land the re-key before instances federate in earnest: frozen receiver-side copies mean every hollow row pushed today is a row no protocol upgrade can heal.
