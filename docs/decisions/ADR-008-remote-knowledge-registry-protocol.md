# ADR-008 – Registries Are Remote Knowledge Providers Following a Standard Protocol

> **Status:** Accepted
> **Date:** 2025-10-05
> **Author:** @jadb
> **Supersedes:** None
> **Superseded by:** None

---

## Context

ContextHelp aims to support a decentralized ecosystem of knowledge sources that provide taxonomy, tags, structured metadata, examples, weights, translations, and domain-specific logic. Users must be able to subscribe to any number of these external “registries” so their pipelines, tagging system, and agents can reason using consistent, shared vocabulary while remaining fully local-first.

Prior to this ADR, several questions remained unresolved:

- Should registries be local bundles, remote services, or both?
- Should ContextHelp mirror (sync) registry data locally, or query them live?
- How can we ensure that taxonomies and tag definitions remain authoritative and conflict-free?
- How can decentralized publishers expose knowledge (tags, taxonomy, weights, metadata) reliably?
- How should registries authenticate clients or enforce rate limits?
- How does the system ensure extensibility without binding to a specific registry implementation?

Furthermore:

- ContextHelp’s tag system relies on canonical definitions that must be consistently interpretable across all users.
- Registries may be operated by individuals, teams, companies, or paid providers.
- Some registries may not permit replication or local sync, requiring a query-only model.
- Knowledge evolves and must be retrievable dynamically without forcing users to maintain large local snapshots.

This ADR defines the role, expectations, and protocol requirements for registries in the ContextHelp ecosystem.

---

## Decision

**Registries will be defined as *remote knowledge providers* that expose a formal Registry Protocol. They serve as authoritative sources for taxonomy, tag definitions, metadata, semantics, and optional content, and are queried directly during multi-source retrieval. Local sync is optional and per-registry.**

---

## Rationale

### Why registries must be remote, not mandatory local bundles
A fully local model would:

- require users to download large datasets,
- create version mismatches,
- introduce conflicts between taxonomies,
- burden publishers with complex sync tooling,
- and prevent paid/private registries from enforcing access control.

A remote model keeps registries independent and self-governed.

### Why a formal Registry Protocol is required
Without a stable protocol:

- tags become ambiguous across registries,
- synonyms and translations become inconsistent,
- AI and agents cannot rely on predictable semantics,
- pipeline and query logic lose determinism.

The protocol provides structured access to:

- canonical tag definitions
- taxonomy trees
- tag aliases, weights, translations
- registry metadata (publisher, version, capabilities)
- search endpoints (optional)
- sync capabilities (optional)

### Why multi-source retrieval is the default
Live retrieval provides:

- fresh results
- minimal local footprint
- support for dynamic or paid registries
- consistent authoritative responses

Syncing is still supported when available and desired, but is not required for correctness.

### Why registries must remain authoritative
If ContextHelp allowed arbitrary local tag creation without reference to registry definitions, the ecosystem would fragment.
Registries maintain consistency and alignment between users.

### Alternatives considered and rejected
- **Full local sync only** → creates storage explosion, stale data, licensing issues.
- **Registry-less tagging** → eliminates interoperability and shared semantics.
- **Ad-hoc JSON or file-based registries** → no extensibility, no capability negotiation.
- **Hardcoding taxonomies in core** → blocking ecosystem growth and decentralization.

---

## Consequences

### Positive
- Decentralized ecosystem of registries, both open and commercial.
- Guaranteed authoritative definitions for tags and taxonomy.
- Ability to evolve tags, weights, and semantics without upgrading ContextHelp.
- Multi-source retrieval with richer knowledge and fresher results.
- Flexible per-registry sync enabling offline or mixed modes.
- Lower storage footprint for users.

### Negative
- Requires reranking to merge registry results with local results.
- Registries may become unavailable, requiring fallback behavior.
- Higher latency for remote queries unless cached effectively.
- More complexity in search pipeline (scatter-gather architecture).

### Neutral or Considerations
- Registries may require authentication, rate limits, or permissions.
- Users must manage registry subscriptions within configuration.
- Registry protocol versioning must be maintained with backward compatibility.

---

## Implementation Notes

- Introduce a `RegistryClient` interface with adapters for each registry.
- Registry protocol should define:
  - `/metadata` (name, version, capabilities)
  - `/taxonomy`
  - `/tags`
  - `/tags/{label}`
  - `/search` (optional)
  - `/sync` or `/delta` endpoints (optional)
- Implement capability negotiation so ContextHelp knows if a registry supports:
  - live search
  - syncing
  - partial sync
  - translations
  - weights
- Introduce a per-registry configuration section in `config.yaml`.
- Add caching decorators for registry lookups.
- The search engine will include:
  - local results
  - synced registry snapshots (if enabled)
  - live registry results
  merged via a reranker.
- Testing must include:
  - registry timeouts
  - stale data fallback
  - schema version mismatches
  - missing or corrupted registry responses

---

## References

- ADR-009 – Multi-Source Retrieval as Default
- ADR-011 – Dedicated Reranking Layer
- Decentralized architecture discussion
- Registry protocol prototypes
- Kernel Memory (KM) multi-node inspiration
- Various knowledge-graph registry models

---