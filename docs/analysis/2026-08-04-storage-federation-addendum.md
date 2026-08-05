# Storage Through the Decentralization Lens — Addendum

> **Date:** 2026-08-04
> **Applies to:** dPKMS, ctxt
> **Extends:** [2026-08-04-storage-architecture-pov.md](2026-08-04-storage-architecture-pov.md)

With federation as a peer objective to knowledge storage/retrieval — any person or business running one or many instances, public or access-controlled, with granular grants for content federation, aggregation, and distribution — several judgments in the base assessment change weight.

## The bet, restated

Once dpkms is a federating node rather than a single-user PKMS, the SQLite-per-instance design stops being merely a simplicity play and becomes the **sovereignty boundary itself**. One instance = one database = one unit of ownership, trust, and access policy. That is the architecturally interesting move: instead of building a distributed storage system, the design keeps storage deliberately boring and pushes *all* distribution complexity into a protocol layer at the edges — registry sync (ADR-008/034), federation pull workers with per-remote **watermarks** persisted in the same DB (ADR-064), signed bundles (Ed25519, keys in the OS keychain), and a trust/capability/entitlement model (`internal/registry/{trust,capability,entitlement,metering}.go`, ADR-032).

This is the correct decomposition. Federated systems die two ways: shared mutable storage (consistency hell) or bespoke sync protocols bolted onto storage that was not designed to be replicated. dpkms avoids both by making replication **pull-based, watermark-incremental, and ownership-anchored** — every object has one authoritative home instance; subscribers hold derived copies. That sidesteps CRDTs and multi-master conflict resolution entirely, at the cost of accepting eventual consistency and no cross-instance writes. For knowledge federation (as opposed to collaborative editing), that is the right trade — the ActivityPub/registry model, not the Google Docs model.

The transactional co-location argument from the base assessment pays a second dividend here: sync state (watermarks, registry cache, index signatures) commits atomically with the content it describes. A federation worker that crashes mid-pull resumes from a watermark that is *guaranteed consistent* with what actually landed. Systems that keep sync cursors in a sidecar file get this wrong perpetually.

## What federation changes in the risk ranking

**1. The direct-storage fallback becomes a security hole, not just a fragility.** In single-user mode, ctxt bypassing the daemon to hit SQLite directly is a politeness violation. On a *public or protected* instance, the daemon's HTTP boundary is the policy enforcement point — entitlements, metering, capability checks all live there. Any code path that reaches storage directly bypasses access control by construction. For any instance with `access != private`, daemon-only mode must be mandatory and enforced (exclusive DB lock held by the daemon), not conventional. This moves from "recommended" to "non-negotiable."

**2. Postgres parity is promoted from tax to strategic asset.** "Any business can run one or many instances" is precisely the multi-writer, concurrent-reader workload where SQLite's single-writer model becomes the ceiling. The Postgres driver is no longer an escape hatch — it is the deployment target for every serious hosted/team instance. That makes the cross-driver conformance suite urgent rather than hygienic, and the missing vector-search parity (sqlite-vec has no wired Postgres analog) a roadmap gap: a public instance that cannot serve semantic search over federated content is offering half the product.

**3. Blob references do not federate.** `blob://<key>` is instance-local. The moment a >64 KiB object syncs to a subscriber, the ref is dangling unless resolution goes back to the origin (presigned S3 URLs help, but couple availability to the origin's uptime — which contradicts local-first) or blobs replicate alongside rows (storage cost, plus entitlement checks must then cover blob fetches too). This needs an explicit design decision — content-addressed blob keys would at least make cross-instance dedup and integrity verification natural. It is currently the least-specified seam in the federation story.

**4. Deletion and retraction become first-class hard problems.** Watermark-based incremental pull handles additive sync cleanly, but federation of *knowledge* raises retraction: a publisher revokes access, corrects an error, or exercises deletion rights. That requires tombstones flowing through the same watermark stream, subscriber-side purge semantics, and — because entitlements are checked at sync time, not perpetually — a story for content already replicated when access is revoked. The privacy-by-design commitment (`docs/dpkms/privacy.md`, ADR-019) is only as strong as retraction propagation. This should be specified before public instances exist, because it cannot be retrofitted onto subscribers one does not control.

**5. Key lifecycle outgrows the keychain.** Ed25519 signing with keys in the OS keychain is right for individuals. Businesses running many instances need rotation, revocation, and probably delegation (instance keys signed by an org key). The trust store (`internal/registry/trust.go`) is the seed of this; a revocation/rotation protocol is the missing piece. Signature verification without revocation is a promise that cannot be taken back.

## Revised verdict

The federation objective does not undermine the storage architecture — it *validates* it, because keeping storage embedded and boring is what makes thousands of independently-operated instances operable by non-experts, which is the actual precondition for decentralization succeeding. (Nobody federates what they cannot self-host.) The metering/entitlement tables living beside the content also position commercial knowledge distribution — paid registries, granular grants — on the same substrate, a quiet strategic strength.

But the risk ordering inverts: in the PKMS frame, the top concerns were driver drift and CGO builds. In the federation frame, they are (1) hard enforcement of the daemon-as-sole-policy-boundary, (2) blob federation semantics, (3) retraction propagation, (4) Postgres feature parity including vectors. All four sit exactly on the seam between "storage that is an instance" and "protocol that connects instances" — which is where this architecture has chosen to spend its complexity budget, and where its review attention should follow.
