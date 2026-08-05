# Data Storage Architecture — Systems Architect's Assessment

> **Date:** 2026-08-04
> **Applies to:** dPKMS, ctxt
> **Companion:** [2026-08-04-storage-federation-addendum.md](2026-08-04-storage-federation-addendum.md)

## The core bet

The architecture makes one big, coherent bet: **a single embedded SQLite database is the entire substrate**. System of record, full-text index (FTS5), vector index (sqlite-vec), job queue, cache tables, audit log — one file, one transaction domain. Everything else in the storage story is either an overflow valve (blobs), a staging area (ambient buffer), or an escape hatch (Postgres).

This is the right bet for a local-first PKMS, and the strongest property it buys is rarely stated explicitly: **transactional co-location**. Ingesting an object, updating its FTS row, writing its embedding, and enqueuing follow-up jobs can all commit atomically. The transactional-outbox ingestion pattern (ADR-007) only works this cleanly because the queue lives in the same database as the data. The moment you split queue (Redis) or vectors (Qdrant) into separate services, you inherit dual-write consistency problems, crash-recovery reconciliation, and an ops footprint no single-user tool can justify. The compose file gestures at Redis and Qdrant, but the Go code never adopted them — correctly. The compose entries are now misleading documentation and should be pruned or annotated.

## Layering

The layering discipline is genuinely good:

1. **Interface layer** — `internal/storage/storage.go` defines ~24 narrow store interfaces (ObjectStore, JobStore, EdgeStore, BlobStore…). Consumers depend on interfaces, never drivers.
2. **Drivers** — SQLite (default) and Postgres implement the same contract; config validation admits only those two. ADR-021 keeps BoltDB/Badger/Neo4j as hypothetical plugin backends without paying for them today.
3. **Overflow** — blob store externalizes RawContent above 64 KiB behind `blob://` refs, with local-FS default and S3-compatible backends (R2, B2, MinIO, Garage). This keeps the DB hot path small — the classic "don't put video in your B-tree" rule, done properly.
4. **Edge staging** — the ambient buffer is a filesystem/S3 event spool *outside* the transaction domain, decoupling capture (ctxd, must never block) from ingestion (dpkms, may be down).

Equally important is what the layer boundary *isn't*: there is no code-level storage split between ctxt and dpkms — both binaries link `internal/storage`. The separation is **behavioral**: dpkms is the sole writer in server mode; ctxt talks HTTP via `idxbridge` and falls back to direct storage when no daemon runs. That is a pragmatic single-user design, but it is the most fragile contract in the system — enforced by convention and a reachability probe, not by types or process boundaries. Two writers on one SQLite file with CGO/FTS5 in play is the failure mode to guard hardest against; the fallback path should be gated by an exclusive lock rather than politeness.

## Search: one index, two modalities

Keyword (FTS5) and vector (vec0) search blend inside the same SQLite process, and ADR-046 explicitly rejected RAG-style chunking — objects are embedded whole over a projected body. That decision trades recall ceiling for architectural simplicity and correct-by-construction citations (no chunk-to-source reassembly). For a personal corpus measured in tens of thousands of objects, that is the right trade; sqlite-vec's brute-force/ANN behavior will hold up fine. The index-signature table (tracking tokenizer/embedding-model provenance) shows maturity — index invalidation is where these systems usually rot silently.

There is no RAG pipeline, deliberately: the system implements the retrieval half only (embeddings + FTS blend, ranking, proximity, citations) and serves raw results to external consumers — CLI, HTTP API, MCP server. Retrieval-augmented *consumers*, not generation inside the product (ADR-036/039/045/046; progressive-retrieval direction in P-080).

## Risks and where to push

- **Postgres parity drift.** Two full driver implementations of ~24 interfaces is a standing tax. Without a shared conformance test suite run against both drivers in CI, the Postgres path *will* drift — it is the less-exercised one. sqlite-vec has no Postgres analog wired, so vector-search parity is already an open question.
- **CGO bifurcation.** Shipping both `mattn/go-sqlite3` (CGO, FTS5) and `modernc.org/sqlite` (pure Go) means build-tag-dependent behavior; the known "no such module: fts5" test failure is this risk manifesting. Pick one canonical build for release artifacts and treat the other as a fallback with an explicit degraded-capability flag.
- **Blob/DB referential integrity.** `blob://` refs cross the transaction boundary — a crash between blob write and row commit orphans blobs; deletion orders risk dangling refs. A periodic reconciliation job (mark-and-sweep against the blob store) is cheap insurance.
- **File-state sprawl.** Config, cursors, policy, pidfiles, upgrade state, REPL history each have bespoke XDG persistence. Individually fine; collectively, backup/restore correctness ("what is the complete state of this install?") now spans one DB, a blob dir, a spool dir, and ~8 file families. The backup subsystem should own that enumeration explicitly.

## Verdict

This is a disciplined local-first design that resisted every fashionable temptation — no Redis, no vector DB service, no chunked RAG — and got substantial payback in atomicity and operational simplicity. The bet only degrades at multi-user scale, and the Postgres escape hatch exists for exactly that. The work to prioritize is not new capability: cross-driver conformance tests, hard single-writer enforcement, and blob reconciliation. All three defend properties the architecture already claims, rather than adding ones it does not need.

See the [federation addendum](2026-08-04-storage-federation-addendum.md) for how the decentralization objective reorders these risks.
