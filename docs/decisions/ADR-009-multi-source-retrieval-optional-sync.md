# ADR-009 – Multi-Source Retrieval as the Default, with Optional Per-Registry Sync

> **Status:** Proposed
> **Date:** 2025-10-05
> **Author:** @jadb
> **Supersedes:** None
> **Superseded by:** None

---

## Context

ContextHelp is designed as a **local-first**, **decentralized**, and **registry-extensible** knowledge engine. Users may subscribe to multiple registries—public, private, or commercial—to enrich their local knowledge base with taxonomies, tags, heuristics, examples, and domain-specific insights.

Early designs explored two competing approaches for accessing registry content:

1. **Local Sync Model**
   - Registries publish full datasets.
   - ContextHelp downloads and stores them locally.
   - All queries run against SQLite or another local backend.

2. **Multi-Source Retrieval Model (Scatter/Gather)**
   - Queries run against local bookmarks *and* subscribed registries.
   - Results are merged and re-ranked.
   - Registries act as authoritative remote sources of knowledge.

The decision affects many subsystems:

- Search & retrieval
- Ranking & reranking
- Pipelines (especially metadata enrichment)
- Query execution model
- Registry protocol
- Storage load
- UX expectations for latency vs freshness
- Plugin interfaces
- Offline behavior
- Privacy and decentralization guarantees

Key constraints:

- Registries may be **large**, **dynamic**, **private**, or **paid**, making full sync impractical or disallowed.
- Users expect **fresh**, **always-current** results from registries with zero maintenance.
- Registries may expose richer data (embeddings, structured patterns, examples) that do not map cleanly to local schema.
- ContextHelp aims for minimal local footprint, portable binaries, no external dependencies, and optional syncing only when explicitly requested.
- Multi-source retrieval aligns with the future vision of a federated knowledge network.

Given these constraints and goals, we must choose how registries participate in search and how data flows between local and remote sources.

---

## Decision

**ContextHelp will use Multi-Source Retrieval as the default mode of operation, with optional per-registry local sync.**

Queries will:

- Always include the local SQLite store
- Optionally fan out to subscribed registries
- Merge all results into a unified set
- Deduplicate
- Pass through a dedicated reranking layer

Registries *may* opt-in to sync modes, but syncing is not required, not assumed, and not the primary mechanism.

---

## Rationale

### Why Multi-Source Retrieval Is Preferred

1. **Freshness Without Sync Overhead**
   Registries can update frequently. Multi-source retrieval ensures results are always fresh without requiring periodic full syncs.

2. **Supports Private, Paid, or Restricted Registries**
   Many registries may legally or contractually forbid bulk download. Multi-source retrieval respects boundaries and enforces access control per query.

3. **Reduced Local Storage Footprint**
   Users may subscribe to many registries. Local sync would cause storage bloat, unnecessary I/O, and slower local indexing.

4. **Avoids Conflict Resolution Complexity**
   Registries may define overlapping taxonomies or tag labels. Multi-source retrieval avoids merging semantics at sync time.

5. **Aligns with Decentralized / Federated Model**
   Registries remain autonomous nodes. ContextHelp queries them as "knowledge peers," avoiding centralization.

6. **Better Ecosystem Support**
   Third-party registry authors can publish structured knowledge without worrying about sync compatibility, storage constraints, or distributed merges.

7. **Simpler Security & Privacy Boundaries**
   Syncing data locally introduces long-term state retention. Multi-source retrieval keeps remote data transient unless explicitly stored.

### Why Not Default to Full Sync

- Sync introduces complexity: caching, deltas, deletes, versioning, conflict resolution.
- Not all registries can expose embeddable local-index-friendly formats.
- Many users do **not** want large data replicated on their device.
- Sync prevents “query-time contextual updates,” such as extremely dynamic registries (security feeds, release notes, changelogs).

### Why Optional Sync Remains Available

Some users value:

- Offline availability
- Local embedding search over registry data
- Full-text scanning of registry content
- Heavy-speed scenarios involving agents or automation

Therefore, sync is available per-registry but not required.

---

## Consequences

### Positive

- **Always fresh results** from remote registries.
- **Minimal local storage footprint** regardless of number of subscribed registries.
- **Supports private, paid, or enterprise registries** without legal or technical challenges.
- **Decentralized-by-design** ecosystem where registries remain autonomous.
- **Simpler UX**—no sync operations needed for basic use.
- **Registry publishers can evolve content without breaking local caches.**

### Negative

- Remote calls add **latency**, requiring:
  - parallelization
  - timeouts
  - partial-result strategies

- Requires a **dedicated reranking engine** to merge heterogeneous scores.
- Registry downtime impacts result completeness.
- Developers of registries must implement search endpoints consistently.

### Neutral / Considerations

- Local sync remains available but may require storage and migrations.
- Registries must communicate capabilities (syncable / not syncable).
- Caching layers may be introduced to reduce cold-start overhead.
- Users with many registries may want configurable query fan-out limits.

---

## Implementation Notes

- Expand Registry Protocol to define search endpoint:
  - `/search` with pagination, scoring, metadata, tag mapping.
  - Capability flags: `supportsSync`, `supportsDelta`, `supportsEmbeddings`.

- Add **Reranker Service** as a core module.
- Add caching layer for remote registry queries (TTL-based).
- Modify search execution plan:
  - Step 1: Compile AST
  - Step 2: Local search (SQL + FTS + vector)
  - Step 3: Parallel remote registry queries
  - Step 4: Merge + normalize + dedupe
  - Step 5: Rerank with RRF or weighted scoring

- Registry sync command:
  ```
  ch registry sync <registry>
  ```
- Sync metadata stored in `registry_snapshots` table.

- Provide plugin interface for registry adapters.

---

## References

- ADR-001 – Local-First Architecture
- ADR-010 – Query Language (RSQL-based)
- ADR-011 – Reranking Layer
- Microsoft Kernel Memory scatter/gather indexing patterns
- Elastic / Meilisearch federated search designs
- Decentralized system patterns (ActivityPub, IPFS, matrix search federation)

---