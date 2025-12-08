# ADR-013 – Introduce Mentions and the Knowledge Graph as First-Class Semantic Infrastructure

> **Status:** Proposed
> **Date:** 2025-12-08
> **Author:** @jadb
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

ContextHelp previously operated with two semantic layers:

- **Hints (`#ui #inspiration`)** – user-provided nudges that help shape interpretation.
- **Tags** – AI-generated classifications and weighted semantic signals.

While powerful, these layers lacked the ability to **reference canonical concepts** or express **explicit identity-based relationships** across bookmarks. This created several limitations:

- Users could not explicitly indicate that content refers to a known concept or entity.
- Tags remained emergent labels, not stable identifiers.
- Queries could not reliably filter based on canonical ideas or domain concepts.
- Registries could not expose rich semantic objects like APIs, standards, product modules, UI patterns, etc.
- No graph structure existed to link bookmarks ↔ entities ↔ related entities.

Constraints included:
- Preserving the separation of hints, tags, and mentions.
- Avoiding any UX regression in pipelines or search.
- Ensuring compatibility with decentralized registries and local-first principles.
- Maintaining stable entity identity across updates.
- Keeping performance predictable even at scale.

Subsystems affected:
- Pipelines (mention extraction + resolution)
- Bookmark schema
- Registry protocol + syncing
- Storage (entity index, backlinks)
- Query language (mention operators)
- APIs and CLI
- Semantic retrieval and ranking

Goals:
- Introduce an explicit semantic identity layer (entities).
- Enable precise cross-bookmark linking via mentions.
- Support a structured knowledge graph for inference, navigation, and semantic contextualization.
- Preserve backward compatibility and remain opt-in for users.

---

## Decision

**ContextHelp will introduce Mentions (@entity) and a canonical Knowledge Graph connecting bookmarks and entities, making entities first-class semantic references throughout the system.**

This includes:
- A new `mentions` field in bookmark schema.
- A new entity schema (`schema-entity.md`).
- Mention extraction + resolution steps in all pipelines.
- A graph index with backlinks for entity/bookmark relationships.
- Registry support for publishing entities and aliases.
- Query language extensions (`mention:` operator).
- A new `knowledge-graph.md` document describing structure and semantics.

---

## Rationale

Alternate approaches were considered:

### 1. **Extending tags to include canonical IDs** (rejected)
Tags are emergent, weighted, and model-driven. They:
- don’t represent identity
- may change across versions
- are influenced by hints, content distribution, and inference heuristics

Using them as canonical references would mix classification with identity, creating confusion and instability.

### 2. **Treating mentions as hints with special syntax** (rejected)
Hints:
- shape interpretation
- influence tagging
- do not represent durable concepts
- are not backed by registries

This would blur semantics, introduce pipeline ambiguity, and hurt query precision.

### 3. **Standalone concept registry without mentions** (rejected)
Entities would exist, but no user-facing reference mechanism would bind bookmarks to them, making the system incomplete.

---

### Benefits of chosen approach

- **Stable semantic identity**
  Entities are durable, user-editable, and registry-backed.

- **Explicit linking**
  Mentions (`@ui.best-practice`) create unambiguous edges.

- **Graph reasoning**
  Backlinks support semantic navigation and intelligent retrieval.

- **Separation of concerns**
  - hints → influence
  - tags → classification
  - mentions → identity

- **Extensibility**
  Registries can publish concept hierarchies, API surfaces, domain vocab, etc.

- **Decentralization-compatible**
  Entity conflict resolution is namespace-aware and versionable.

### Drawbacks / Risks

- Increased complexity in pipelines due to mention extraction.
- Storage overhead for entity index and backlinks.
- Requires registry ecosystem maturity to fully realize potential.
- Users may confuse hints/tags/mentions; UX education required.

---

## Consequences

### Positive
- Enables precise concept referencing.
- Supports graph-aware search and reasoning.
- Introduces a foundation for future semantic automation (embedding per entity, clustering, reasoning agents).
- Improves developer UX by making conceptual domains machine-navigable.

### Negative
- Adds new schemas and fields developers must understand.
- Requires new pipeline and registry logic.
- Graph-building increases ingestion complexity.

### Neutral / Considerations
- Mentions do not influence tags; this must remain consistent.
- Plugins must adopt mention-aware processing where relevant.
- Registries must implement entity endpoints for full functionality.

---

## Implementation Notes

- Add `mentions` array to bookmark schema.
- Create `schema-entity.md` defining canonical entities.
- Add mention extraction + resolution pipeline step before tagging.
- Add entity index + backlink table to storage.
- Extend query language (`mention:slug`, wildcards).
- Update REST/CLI/gRPC APIs to support:
  - listing entities
  - fetching entity metadata
  - resolving aliases
  - retrieving backlinks
- Implement `knowledge-graph.md` describing graph semantics.
- Update all affected docs: architecture, schema-registry, schema-taxonomy, schema-tag, pipelines.*

Migration:
- Existing bookmarks receive empty `mentions` arrays.
- Graph index initializes with no edges, builds incrementally.

Testing:
- mention parsing
- entity resolution (local + registry)
- graph integrity
- query correctness
- backward compatibility

---

## References

- **mentions.md** – foundational description of mention semantics
- **knowledge-graph.md** – structure and usage of the new graph layer
- **schema-entity.md** – canonical entity model
- **schema-bookmark.md** – updated bookmark schema
- **query-language-spec.md** – mention operators
- ADR-010 – use extended RSQL
- ADR-004 – step-based pipeline architecture enabling mention extraction
