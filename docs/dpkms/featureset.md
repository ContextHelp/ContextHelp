# Feature Set (dPKMS Only)

## Transactional Job Queue (Durable Local Outbox Pattern)
All ingestion, refresh, sync, and maintenance operations run through a transactional
jobs table, guaranteeing crash-safe recovery, resumable execution, background
processing, and deterministic replays for auditability and debugging.

## Pipeline Runtime (Deterministic + Capability-Scoped)
A generic pipeline execution runtime with step isolation, typed inputs/outputs,
caching hooks, idempotency guarantees, structured logs, and strict capability
enforcement — ensuring work runs safely and consistently regardless of what the
pipeline does.

## Structured Knowledge Objects (Storage-First, Agent-Ready)
A structured object model for storing enriched knowledge with metadata, sections,
tags, mentions, decisions, tasks, embeddings, and provenance — supporting
progressive enrichment without requiring it.

## Semantic Identity + Knowledge Graph
Stable canonical entities referenced via `@mentions`, with a graph index of
object ↔ entity and entity ↔ entity edges used for querying, navigation, and
graph-aware retrieval.

## Traceability + Version History (Receipts + Reversible Change)
Full provenance preservation (source + time + pipeline + registry influence) with
revision history for knowledge objects, entities, and graph edges, including diff
views, rollback, and “why this changed” explanation metadata.

## Advanced Query Engine (AST-Based, Explainable)
An AST-based query language supports boolean logic, nested expressions, ranges,
time windows, provenance filters, mention-aware constraints, and graph operators,
compiling cleanly into SQL, FTS, vector search, and graph traversal — with
explainable match reasoning.

## Language-Native Storage + Indexing (Multilingual by Design)
Unicode-safe and RTL-safe storage and indexing support mixed-language knowledge
without hacks, including localized entity labels, aliases, and language-aware
querying across Arabic/English/French content.

## Security + Encryption (Sovereign by Design)
Encryption at rest and in transit with pluggable key management, controlled sharing
handshakes, and an optional privacy-preserving search mode that supports filtering
and retrieval without exposing plaintext content.

## Authentication + Authorization (Capability-Scoped)
Built-in support for local-only mode, token-based access, scoped permissions for
workspaces and registries, and policy-driven authorization for publishing,
subscribing, and collaboration.

## Decentralized Registries (Optional Subscriptions)
Subscribe to external or local registries for taxonomies, entity definitions,
localized labels and aliases, weights, shared knowledge packs, and workflows —
with optional authentication, paid access, trust policies, and controlled import
rules.

## Federated Query + Scatter–Gather Retrieval (Across Sources)
Query across local storage plus multiple registries, merge results with hybrid
retrieval (symbolic + vector), and rerank outputs using provenance-aware scoring,
mention-aware filters, graph-informed expansion, and source trust weighting.

## Self-Authenticating Knowledge (Optional Signatures + Integrity)
Optional cryptographic signing and verification for entities, bundles, registries,
and revisions, enabling tamper-evident history and portable trust without requiring
a centralized authority.

## Multiple Storage Backends (Sovereign + Portable)
Default SQLite backend with optional Postgres, remote stores, vector databases,
and community backends — all behind a portable storage contract that guarantees
stable IDs and reversible migration paths.

## Export + Portability Contract (No Lock-In)
Full export/import support for Markdown + JSON + SQLite bundles, preserving
entities, edges, provenance, IDs, and attachments, enabling complete migration
without data loss or broken links.

## Plugin Architecture (Formal Contract + Permissions)
A formal plugin contract for extending pipelines, registries, storage backends,
query operators, encryption providers, authentication methods, export formats,
and job behaviors — enforced through strict permission scopes.

## Performance Guarantees (Fast at Scale)
Indexing strategy for FTS + vectors + graph adjacency, background compaction,
incremental embedding updates, and caching layers to maintain predictable latency
as storage and graph size grows.
