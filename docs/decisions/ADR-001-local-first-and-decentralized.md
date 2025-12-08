# ADR-001 – Local-First and Decentralized Architecture

> **Status:** Proposed
> **Date:** 2025-10-05
> **Author:** @jadb
> **Supersedes:** —
> **Superseded by:** —

---

## Context

ContextHelp is intended to operate as a personal knowledge engine that captures, processes, indexes, and retrieves a user’s contextual information across multiple media types. From the earliest discussions, a core requirement emerged: **users must retain ownership and control of their data**, and the system must not depend on centralized servers or third-party storage providers.

Several constraints shape this decision:

- **Privacy & Data Sovereignty:** Users frequently capture sensitive content (screenshots, work documents, URLs, prompts, internal tools). Storing such data externally—even temporarily—can violate confidentiality or compliance requirements.
- **Offline Reliability:** Many intended workflows occur during deep work, on planes, in secure environments, or where connectivity is intermittent.
- **Decentralized Knowledge Ecosystem:** Registries are designed to be independent knowledge providers. They may be public, private, commercial, or specialized. ContextHelp should *pull* from these registries opportunistically, not require constant connectivity.
- **Performance:** Local storage is significantly faster for retrieval and indexing than networked systems.
- **Extensibility:** The system must support plugins that interact with local content without requiring cloud syncing or multi-user coordination.
- **User Trust:** Users should be confident that no data leaves their machine unless they explicitly configure a registry or plugin that performs remote operations.

Without a clear architectural stance, contributors could mistakenly introduce assumptions about shared cloud indexes, required network services, or remotely stored bookmarks—undermining the core product identity.

This ADR impacts:
- storage subsystem
- ingestion pipeline
- registry protocol design
- query execution engine
- plugins API
- privacy model
- future sync or collaboration features

The guiding design objective is to ensure the system remains **private-by-default**, **offline-capable**, and **federated**, with registries acting as independently operated knowledge providers rather than authoritative storage for user data.

---

## Decision

**ContextHelp will adopt a Local-First and Decentralized architecture, where all user data, pipelines, bookmarks, metadata, and indexes are stored and executed locally, and registries are optional external knowledge sources queried on demand rather than centralized storage authorities.**

---

## Rationale

### Why Local-First?

- **Privacy:** Sensitive data never leaves the user’s machine unless explicitly configured.
- **Trust:** Users are more comfortable ingesting screenshots, prompts, work documentation, and proprietary content when they know it is not transmitted externally.
- **Performance:** Local databases (e.g., SQLite) provide low-latency, deterministic retrieval for pipelines, agents, and CLI operations.
- **Reliability:** The system continues to function offline, in low-bandwidth environments, or behind restrictive firewalls.

### Why Decentralized?

- **Registry Ecosystem:** By treating registries as independent providers, the system allows:
  - paid registries
  - private enterprise registries
  - niche domain registries
  - local-only registries
- **Federation:** Multiple registries can coexist without global coordination or schema unification.
- **User Autonomy:** Users decide which registries they subscribe to.
- **Extensibility:** Plugins and community-driven registries do not need central approval or infrastructure.

### Alternatives Considered

#### 1. **Cloud-Backed Central Index**
Rejected because:
- Requires accounts, authentication, and user identity management.
- Violates privacy expectations.
- Breaks offline workflows.
- Conflicts with decentralized registry ethos.

#### 2. **Hybrid Sync/Cloud Model (e.g., local + optional cloud mirror)**
Rejected (for now) because:
- Adds architectural complexity prematurely.
- Risks feature divergence between local and cloud modes.
- Introduces ambiguity around the source of truth.

#### 3. **Registry-As-Primary-Store**
Rejected because:
- Registries may not permit full sync.
- Many registries contain only taxonomies, not bookmarks.
- Forces network dependence.
- Eliminates user privacy guarantees.

This decision aligns strongly with modern trends in:
- local LLM agents
- privacy-preserving knowledge tooling
- federated knowledge ecosystems
- local-first software architecture

---

## Consequences

### Positive
- User data remains private and secure by default.
- Reliable offline operation.
- Fast local queries and bookmark access.
- Registries can be diverse, independent, and dynamic.
- No mandatory cloud or account system.
- Easier plugin development (local interfaces).

### Negative
- Multi-device sync must be designed later (optional, user-controlled).
- No shared global search index; results depend on installed registries.
- Some features (e.g., semantic sync, collaborative knowledge graphs) require future design.
- Multi-source retrieval adds complexity to ranking, deduplication, and caching.

### Neutral / Considerations
- Advanced registry models (auth, rate limits, paid tiers) must be supported gracefully.
- Local storage must be robust to user interruptions, file corruption, and migrations.
- Clear UX is needed to distinguish between local and remote search results.

---

## Implementation Notes

- **Storage:** Use SQLite as the default local store; allow pluggable backends.
- **Registries:** Implement a standard registry protocol for metadata, taxonomy, and optional content retrieval.
- **Retrieval:** Default to multi-source scatter/gather; reranking merges local and remote results.
- **Plugins:** Must respect privacy boundaries; external communication requires explicit permission.
- **Config:** Provide user controls for:
  - which registries are active
  - what data can be sent out
  - offline mode
- **Testing:** Write deterministic tests without external network calls; use registry fixtures.

Future considerations:
- optional device sync via encrypted local replication
- optional remote caching layers
- enterprise-specific registry integrations

---

## References

- ContextHelp Architecture Overview
- Registry Protocol Specification (draft)
- ADR-009 – Multi-Source Retrieval Default
- Local-First Software Principles: https://www.inkandswitch.com/local-first/
- Decentralized Knowledge Graph concepts (various papers)

---