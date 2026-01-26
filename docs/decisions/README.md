# Architectural Decision Records (ADRs)

This directory contains the **core architectural decisions** that define how ContextHelp is designed, implemented, extended, and maintained.
Each file documents a high-impact, long-lived decision that shapes the system’s architecture.
ADRs exist to provide clarity, preserve rationale, and guide future contributors as the project evolves.

Below is an overview of all recorded ADRs and their purpose.

---

## Index of Decisions

### **ADR-001 – Local-First and Decentralized**
ContextHelp runs fully on the user's machine, functioning offline by default while optionally connecting to remote registries.
This philosophy ensures privacy, control, and resilience.

### **ADR-002 – Use Go**
Go is the primary implementation language for the engine, CLI, worker, pipelines, and plugin system.
Selected for concurrency primitives, static binaries, plugin capabilities, and cross-platform support.

### **ADR-003 – Separate Read/Write Paths**
Ingestion (write path) is completely separated from retrieval (read path).
Prevents pipelines from interfering with search and ensures predictable performance.

### **ADR-004 – Step-Based Pipeline Architecture**
Pipelines are modular sequences of retry-safe, independently testable steps.
This enables plugin-driven pipeline registration and multi-AI workflows.

### **ADR-005 – Decorator Pattern for AI Providers**
AI clients (LLMs, embeddings, OCR) are extended via decorators rather than internal logic.
Caching, logging, and tracing are layered without altering core clients.

### **ADR-006 – JSON Filesystem Storage (Superseded)**
This ADR originally proposed JSON filesystem as the default storage backend.
**Status:** Superseded - SQLite with WAL mode is now the default storage backend (see dpkms/storage.md).
The system remains pluggable: Postgres, JSONFS, KV stores, or external engines may replace SQLite.

### **ADR-007 – Transactional Outbox for Ingestion**
Ingestion uses a durable job queue (the "transactional outbox").
`ctxt analyze` enqueues a job → worker executes pipeline (via `dpkms serve`) → job state updated → crash recovery guaranteed.

### **ADR-008 – Remote Knowledge Registry Protocol**
Remote registries follow a defined protocol for:
- taxonomy
- tags
- metadata
- weights
- translations (optional)
- custom extensions

Registries act as authoritative knowledge sources.

### **ADR-009 – Multi-Source Retrieval (Optional Sync)**
Search queries fan out to:
- local database
- remote registries
- plugin-defined sources

Optional per-registry syncing enables local indexing when desired.

### **ADR-010 – Use Extended RSQL Query Language**
ContextHelp uses **RSQL as the base query language**, extended with:
- semantic operators
- pipeline filters
- language filters
- registry-scoped fields

All queries parse into an AST for correctness and safety.

### **ADR-011 – Multi-Source Reranker**
Search results from all sources are merged via a dedicated reranker
(e.g., Reciprocal Rank Fusion, weighted scoring).
Ensures consistent, meaningful ranking across heterogeneous sources.

### **ADR-012 – Plugins Extend Any Layer**
Plugins are first-class citizens and may extend:
- pipelines
- registries
- storage engines
- AI providers
- CLI commands
- query operators
- background observers

This is the foundation for the ecosystem.

### **ADR-014 – Two-Package Architecture (dPKMS + ctxt)**
ContextHelp separates into two packages with distinct concerns:

- **dPKMS** (substrate): Provides transactional job queue, pipeline runtime, storage, and persistence guarantees.
- **ctxt** (brain): Defines what work to run, how pipelines behave, and provides user-facing capture/retrieval.

See `docs/dpkms-or-ctxt.md` for detailed boundary specification.

### **ADR-015 – Focus Profiles for Context-Aware Knowledge Scoping**
`ctxt` implements Focus Profiles as declarative context lenses that scope knowledge visibility, ranking, surfacing, pipeline selection, and composition behavior based on user's current role or project.

Enables:
- Role-based profiles (Founder, Engineer, Research, Writer)
- Project-based profiles (time-bounded initiatives)
- Context-aware filtering and ranking
- Profile-scoped registry subscriptions
- Adaptive composition and surfacing

### **ADR-016 – Just-In-Time Surfacing and Proactive Knowledge Resurfacing**
`ctxt` implements a surfacing engine that proactively identifies and surfaces relevant knowledge based on current context, activity patterns, entity mentions, time windows, and focus profiles without requiring explicit queries.

Features:
- Trigger-based discovery (entity mentions, tag patterns, time-based)
- Context-aware relevance scoring
- Multiple surfacing modes (active, ambient, review)
- Explainable reasoning for each surfaced item
- User-controlled thresholds and timing

### **ADR-017 – Composition Engine and Template-Based Knowledge Assembly**
`ctxt` implements a composition engine that generates structured artifacts (briefs, plans, reports, drafts) by assembling atomic knowledge nodes following graph relationships and applying profile-specific templates with full provenance preservation.

Features:
- Template-based composition (briefs, plans, reports, checklists)
- Graph-aware assembly following entity/mention relationships
- Profile-adapted tone, style, and structure
- Full provenance tracking to source objects
- One-command generation with iterative refinement

### **ADR-018 – Safe Agent Execution Model (Propose → Dry-Run → Apply)**
ContextHelp implements a three-phase execution model requiring explicit user approval before agent-initiated actions, with clear previews, scope enforcement, and rollback capability.

Workflow:
- **Propose:** Agent generates detailed action plan with reasoning
- **Dry-Run:** System simulates execution and shows diff preview
- **Apply:** User approves, system commits with audit trail and rollback support

Guarantees user sovereignty, transparency, safety, and accountability.

### **ADR-019 – Encryption and Privacy Model**
dPKMS implements optional, layered encryption with pluggable providers, user-controlled key management, privacy-preserving search, and support for full-database and selective object encryption.

Features:
- Storage-level encryption (SQLCipher, pgcrypto)
- Application-level selective encryption (profile/tag-based rules)
- Privacy-preserving search (blind index)
- Passphrase-based key derivation (Argon2id)
- Key rotation with re-encryption
- Encrypted export/import bundles

### **ADR-020 – Export/Import Contract and Zero Lock-In Portability**
dPKMS implements comprehensive export/import based on portable, self-contained bundles in open formats (Markdown + JSON + SQLite) with stable ID preservation, full graph structure, and optional encryption, ensuring complete data portability without vendor lock-in.

Bundle Contents:
- Human-readable Markdown + machine-readable JSON
- Complete graph structure (entities, mentions, edges)
- Binary attachments with integrity hashes
- Configuration (profiles, settings)
- Registry snapshots and provenance
- Cryptographic verification and optional signatures

Export Modes: full, incremental, selective, minimal

### **ADR-021 – Multi-Backend Storage Strategy (SQLite Default, Pluggable Architecture)**
dPKMS uses SQLite with WAL mode as the default storage backend for zero-install local-first operation, with optional PostgreSQL support for team/organization deployments, and maintains a pluggable architecture enabling custom backends.

**Storage Strategy:**
- **Default:** SQLite (zero-install, local-first, 1M+ objects)
- **Teams:** PostgreSQL (concurrent writes, HA, 100M+ objects)
- **Specialized:** LEANN (vector-optimized), custom plugins

**Migration:** Clear paths between backends via export/import
**Supersedes:** ADR-006 (JSON Default Storage)

### **ADR-022 – Vector Indexing and Hybrid Semantic Search**
dPKMS implements hybrid semantic search combining FTS5, vector similarity (embeddings), and graph traversal through unified query execution with pluggable vector backends and Reciprocal Rank Fusion (RRF) for result merging.

**Features:**
- Semantic search operators (`similar=="query"`)
- Multiple embedding providers (OpenAI, Ollama, local models)
- Pluggable vector backends (in-memory, pgvector, LEANN)
- Hybrid query execution (FTS + Vector + Graph)
- RRF merger for intelligent ranking
- Privacy-first (local models default)
- Re-embedding workflows for model evolution

### **ADR-023 – Authentication and Authorization Model**
ContextHelp implements an optional, layered authentication and authorization model with token-based authentication, OAuth/OIDC for registry connections, capability-based permissions for plugins and agents, and profile-scoped access control.

**Features:**
- Personal Access Tokens (PATs) for API access
- OAuth 2.0 + PKCE for registry authentication
- Capability-based authorization (scope system)
- Plugin permission declarations and enforcement
- Profile-scoped access control
- Device authentication for multi-device sync (future)
- Audit trail for authentication events

### **ADR-024 – Self-Authenticating Knowledge (Signatures and Trust)**
dPKMS implements an optional self-authenticating knowledge model supporting cryptographic signatures for objects, exports, registry data, and plugins, with configurable trust policies and verification workflows.

**Features:**
- Ed25519 and HMAC signature support
- Export bundle signing and verification
- Registry data signature verification
- Plugin signature verification
- Trust policies (auto-accept, prompt, reject)
- Provenance chain attestation with pipeline step signatures
- Key management (generation, import, revocation)

### **ADR-025 – Multilingual Support Strategy (I18N/L10N)**
dPKMS and ctxt implement comprehensive multilingual support with language detection, translation storage, federated translation sync, and cross-language search capabilities.

**Features:**
- Language detection during inference (primary/secondary languages)
- Translation storage in dedicated `translations` map
- Plugin-based translation providers (OpenAI, DeepL, local models)
- Multilingual embedding models for cross-language search
- Entity and tag label localization
- Registry translation synchronization
- Original content preservation

### **ADR-026 – Multimodal Content Processing**
dPKMS and ctxt implement comprehensive multimodal content processing with content-addressed attachment storage, OCR, transcription, and cross-modal semantic search.

**Features:**
- Content-addressed attachment store (hash-based deduplication)
- Multimodal pipelines (image, audio, video, document)
- OCR integration (Tesseract local, cloud providers)
- Transcription integration (Whisper local, cloud providers)
- Multimodal embeddings for cross-modal search
- Attachment preservation in export/import
- Offline-first processing with graceful degradation

### **ADR-027 – Plugin Isolation and Sandboxing**
ContextHelp implements mandatory capability-based plugin isolation with explicit permission declarations, runtime enforcement, user approval workflows, resource quotas, and comprehensive audit logging.

**Features:**
- Capability-based permission model (filesystem, network, storage, exec)
- Runtime sandbox enforcement at API boundaries
- Resource quotas (CPU, memory, disk, connections)
- User approval at installation with clear permission prompts
- Plugin-namespaced storage isolation
- Comprehensive audit trail for all plugin operations
- Plugin signature verification with trusted publishers

### **ADR-028 – Atomic Notes and Decomposition Strategy**
`ctxt` implements automatic content decomposition into atomic sections during pipeline enrichment, enabling precise search, linking, composition, and graph traversal at the idea level while preserving original content intact.

**Features:**
- Semantic boundary detection for coherent sections
- Section storage as first-class entities linked to parent objects
- Section-level search (FTS + vector embeddings per section)
- Fine-grained linking and backlinks at section granularity
- Composition from atomic sections with full provenance
- Multiple decomposition strategies (heading-aware, speaker-turn, structural)
- Validation for semantic coherence and self-containment

### **ADR-029 – Entity Resolution and Conflict Handling**
dPKMS and ctxt implement deterministic entity resolution with layered precedence (local → registries → placeholder), alias normalization, conflict detection, local overrides, and graceful handling of unresolved entities.

**Features:**
- Layered resolution: Local → Remote Registries → Unresolved Placeholder
- Alias normalization and chain resolution (cycle detection)
- Local override precedence (user sovereignty)
- Registry priority configuration
- Conflict detection and logging (metadata, alias, namespace)
- Unresolved entity placeholders (usable, resolvable later)
- Provenance tracking (source, resolution path, conflicts)

### **ADR-030 – Living Skeleton Development Method**
The project follows the Living Skeleton methodology: build working end-to-end path first, strengthen the same spine incrementally, replace placeholders with real implementations without breaking contracts, prioritize correctness over features.

**Principles:**
- Working end-to-end path from day one (Skeleton 0)
- Each skeleton phase adds capability without breaking previous work
- Replacement over rewrites (swap implementations, keep contracts)
- Contract stability (interfaces stabilize early, internals evolve)
- Incremental value delivery (users adopt progressively)
- Clear phase progression (exit criteria, no skipping)
- Placeholder tracking and systematic replacement

---

## Purpose of This Directory

This directory contains:

- **Persistent historical decisions**
- **Context and justification** for each major choice
- Guidance for contributors evaluating new proposals
- A stable reference for architecture, design, and extensibility boundaries

Only decisions that materially affect the project’s trajectory are captured as ADRs.

---

## How to Use ADRs

### When adding new features or making architectural changes:
- Check whether an existing ADR already dictates the correct approach.
- If not, consider writing a new ADR following the `TEMPLATE.md` file.
- ADRs should be:
  - concise
  - rationale-rich
  - forward-looking
  - easy to read

### When reviewing contributions:
ADRs provide the criteria used to evaluate whether a PR adheres to the project’s architectural intent.

---

## Adding a New ADR

Use the template:

```
TEMPLATE.md
```

Follow the structure:
1. Title
2. Status
3. Context
4. Decision
5. Consequences
6. Alternatives considered

Propose a new ADR only when the decision is:
- foundational
- difficult to reverse
- cross-cutting
- high-impact

Small implementation details typically belong in design docs, not ADRs.

---

## Summary

This directory contains the **canonical architectural backbone** of ContextHelp.

Together, these ADRs encode the philosophy, constraints, and technical direction of the system, ensuring stability and consistent decision-making as the ecosystem grows.

### Two-Package Architecture

ContextHelp is organized into two primary packages:

- **dPKMS**: The execution substrate that provides the local-first storage, job queue, pipeline runtime, and persistence guarantees. See `docs/dpkms-or-ctxt.md` for detailed distinction.
- **ctxt**: The user-facing brain that decides what work to run, defines pipelines, and provides capture/retrieval behavior.

Each ADR includes an "Applies to" field indicating whether it governs dPKMS, ctxt, or both packages.

If you are contributing to ContextHelp, this is one of the most important directories to understand.
