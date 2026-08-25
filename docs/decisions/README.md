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

### **ADR-031 – Nodes and context.help cloud Boundary (Accepted)**
Defines the boundary between OSS nodes (enforcement + protocols) and the hosted offering (org lifecycle, admin UI, billing, marketplace), including the required Node Admin API surface.

### **ADR-032 – Entitlements and Metering as Policy Predicates (Accepted)**
Defines subscriptions, credits, and paywalls as pluggable policy checks (not storage features), enabling paid registries and extensions without embedding billing logic into OSS nodes.

### **ADR-033 – Team RBAC as an Additive Layer Over Scopes (Accepted)**
Adds a team-friendly RBAC layer for humans that compiles down to the existing scope/policy enforcement, preserving least-privilege for tokens and agents.

### **ADR-034 – Registry Packaging: Index Sync and JIT Resolution (Accepted)**
Standardizes thin sync (index/schema) plus just-in-time pulls for full content, with explicit local copy constraints (`none` | `index` | `content`).

### **ADR-035 – Docling as Optional Document Parsing Backend (Accepted)**
Docling (IBM Research, MIT license) is not adopted as a core dependency due to heavyweight footprint (PyTorch, ~6GB RAM) and scope mismatch (parser vs. knowledge substrate). Instead, it may be offered as an optional plugin for high-fidelity document structure extraction (tables, layout, OCR). Default path uses lightweight Go-native parsers.

### **ADR-036 – SurfSense Evaluated and Not Adopted (Accepted)**
SurfSense (Apache-2.0, self-hosted AI research assistant) is not adopted due to fundamental architecture mismatch (multi-service Python/TypeScript web app vs. local-first Go CLI), zero overlap with ctxt's core differentiators (knowledge graph, constrained generation, plugin system, RSQL), and heavyweight dependency requirements (PostgreSQL + Redis + Celery). Specific patterns (hybrid search RRF, chunking strategy, connector OAuth flows) noted as reference material.

### **ADR-037 – Agent-Aware Adaptive Memory and Multi-Agent Coordination (Proposed)**
ContextHelp will implement an agent-aware adaptive memory coordination layer enabling multi-agent workflows to share memory through event-driven protocols, adapt memory encoding by agent role, track decision causality, and maintain consistency across distributed agent execution.

**Problem Solved:**
- Multi-agent systems 40-50% slower than necessary (redundant reasoning)
- No visibility into agent decision rationale
- Memory redundancy (3x per-agent footprint)
- No cross-agent learning or validation

**Solution:**
- Event-driven memory synchronization with subscriptions
- Task-specific retention and memory encoding strategies
- Reasoning traces capturing causality chains
- TTL-based and ack-based consistency protocols

**Impact:**
- 40-60% speedup for research→planning→execution workflows
- 50%+ memory efficiency gain
- 30-40% decision quality improvement (from AMA research)
- Foundation for next-gen multi-agent products

**Roadmap:**
- Phase 1 (Q1 2026): Design & documentation
- Phase 2 (Q2-Q3 2026, Phase 3 roadmap): Foundation (events, profiles, traces)
- Phase 3 (Q3-Q4 2026+, Phase 4 roadmap): Consistency & optimization

### **ADR-038 – SuperMemory Evaluated and Not Adopted (Accepted)**
SuperMemory (MIT, AI memory API platform) is not adopted due to fundamental problem-space mismatch (cloud-first AI agent memory API vs. local-first personal knowledge substrate), hard Cloudflare infrastructure lock-in, and zero overlap with ctxt's core differentiators. Specific concepts (memory decay/intelligent forgetting, memory versioning with updates/extends/derives relationships, MCP server pattern, MemoryBench evaluation framework) noted as reference material.

### **ADR-039 – MLPMemory Evaluated and Not Adopted (for Current Phases) (Accepted)**
MLPMemory (Oct 2025 research: parametric memory via neural weight compression for 2.5× faster inference) is not adopted due to fundamental problem mismatch (optimizes single-agent inference latency vs. our multi-agent coordination latency), architectural conflicts (frozen knowledge incompatible with real-time ingestion), and philosophical misalignment (neural weights vs. explicit knowledge graphs, centralized pretraining vs. local-first operation).

**Key insight:** MLPMemory solves a different problem in a different layer. Our bottleneck is agent coordination (40-60% speedup via ADR-037 AMA), not inference (2.5× speedup via neural compression). Reconsider only in Phase 4+ if building LLM-based agents and inference becomes top-5 issue.

**Related:** ADR-037 (our actual focus: multi-agent coordination), ADR-001 (local-first philosophy), ADR-013 (knowledge graphs)

### **ADR-040 – HEMA: Long-Context Conversation Coherence Architecture (Proposed)**
HEMA (Hippocampus-Inspired Extended Memory Architecture, Apr 2025) proposes dual-memory system for maintaining coherence in 300+ turn conversations via compact narrative memory + episodic vector memory. **Highly applicable** for enterprise chatbot scenarios (Slack, Teams, Discord) and messaging bots (WhatsApp, Telegram).

**Problem addressed:** Over long conversations, current approach loses narrative coherence even with full context available. Users expect bots to remember conversation thread without re-specifying context.

**Use cases requiring HEMA:**
- Enterprise Slack/Teams/Discord bots (50-200 turn channels)
- Personal WhatsApp/Telegram bots (long-lived sessions)
- Iterative search refinement (20-100 turn searches)

**Implementation timing:**
- Phases 1-2: Document and design (this ADR)
- Phase 3 (Q3-Q4 2026+): Implement when prioritizing chatbot/messaging integrations
- Expected improvements: 46-point factual recall gain, 1.6-point coherence gain (from paper)

**Related:** ADR-037 (agent coordination), ADR-039 (inference optimization), ADR-018 (safe execution)

### **ADR-041 – MeMo (Associative Memory LMs) Evaluated and Not Adopted (Accepted)**
MeMo (Zanzotto et al., ACL Findings 2025) proposes explicit associative memory for language models using Correlation Matrix Memories (CMMs) with algebraic forgetting. Not adopted: wrong level of abstraction (token-level vs. knowledge-object-level), synthetic-only evaluation with no real-language evidence, incompatible stack (Python/PyTorch), and non-commercial license (CC BY-NC-SA 4.0). ctxt already provides transparent, editable, forgettable memory through SQLite + structured schemas + graph. Research area (explicit memory architectures) noted for monitoring.

### **ADR-042 – Cognitive Memory in LLMs: Taxonomy Evaluation and Applicability (Proposed)**
Comprehensive taxonomy of memory mechanisms in LLMs (text-based, KV-cache, parameters, hidden-state) that provides framework for understanding design space. **Partially adopted** as validation and roadmap guidance.

**Applicability:**
- ✅ **Text-based memory:** Directly applicable (our primary approach)
- ❌ **KV-cache:** Not applicable (we don't run LLM inference)
- ❌ **Parameters:** Not applicable (we don't fine-tune)
- ⚠️ **Hidden-state:** Partially (handled by HEMA ADR-040)

**Key insight:** Taxonomy confirms we're using correct approach (text-based + multi-strategy retrieval). Identifies three gaps:
1. **Conflict resolution** (detect contradictions in knowledge)
2. **Multi-pass refinement** (enable iterative query refinement)
3. **Cognitive efficiency** (heat-based archival for unbounded growth)

**Implementation:** Gap 1-2 in Phase 3; Gap 3 in Phase 4.

**Expected benefits:** Better knowledge integrity, improved search UX, operational scalability.

**Related:** ADR-037 (Agent Coordination), ADR-040 (HEMA), ADR-013 (Knowledge Graph), ADR-021 (Storage)

### **ADR-044 – ContextualRetriever: Context-Aware Retrieval for Conversational Search (Proposed)**
ContextualRetriever (EMNLP 2025) implements context-aware embeddings for multi-turn conversational search, addressing ADR-042 Gap #2 (multi-pass refinement) through learned understanding of implicit queries within conversation context.

**Problem solved:** Iterative search refinement loses context; users must re-specify full constraints each turn. Implicit references ("that one", "from before") become ambiguous.

**Innovation:** Context-aware embeddings + Conversational Contrastive Learning (CCL) + Intent-Guided Learning (IGL) enable retrieval to understand user intent from conversation without explicit query rewriting.

**Performance:**
- Failed retrievals reduced: 49% (retrieval only), 67% (with reranking)
- Latency: Same as baseline (no extra inference overhead)
- Benchmarks: TREC-CAsT, TopiOCQA, QReCC

**Use cases:**
- ✅ Iterative search refinement (knowledge workers)
- ✅ Multi-turn composition (humans building briefs)
- ✅ Agent reasoning (follow-up questions with implicit context)

**Implementation:** Phase 3 (depends on HEMA ADR-040 + ADR-042 foundations)

**Expected benefits:** Better UX for multi-turn search, handles topic drift, no latency cost, integrates with existing multi-strategy ranking.

**Related:** ADR-042 (fills Gap #2), ADR-040 (HEMA conversation infrastructure), ADR-037 (agent reasoning)

### **ADR-047 – Contextual Normalization for RAG Not Adopted (Accepted)**
C-Norm (Chen et al., Oct 2025; withdrawn from ICLR 2026) proposes training-free delimiter optimization for RAG context formatting using attention-guided scoring. **Not adopted:** the problem it solves (arbitrary formatting of concatenated raw passages) doesn't exist in dPKMS, which uses structured decomposition (atomic sections, entities, mentions) and template-based composition. Tested only on small models (7B, 1.5B) with marginal gains (1-3%). Useful takeaway: delimiter choice can collapse accuracy 81%→10%, validating our architectural choice to control formatting end-to-end.

**Related:** ADR-017 (Composition), ADR-028 (Atomic Notes), ADR-045 (ICR²), ADR-046 (Chunking) — same "wrong architecture" pattern

### **ADR-045 – ICR²: In-Context Retrieval and Reasoning Not Adopted (Accepted)**
ICR² (Qiu et al., ACL 2025 Findings, Apple ML Research) proposes techniques for improving long-context LLM retrieval by fine-tuning models and probing attention heads. **Not adopted:** dPKMS does explicit multi-strategy retrieval (FTS5, vector, graph, RSQL), not in-context LLM retrieval. The paper's finding that confounding passages cause up to 51% performance drop directly validates our architecture's choice of precision retrieval over context-stuffing.

**Related:** ADR-022 (Hybrid Search), ADR-046 (Chunking), ADR-039 (MLPMemory) — same "wrong layer" pattern

### **ADR-046 – Advanced Chunking Strategies for RAG Not Adopted (Accepted)**
Merola & Singh (ECIR 2025 Workshop) compare late chunking vs. contextual retrieval for preserving document context in RAG. **Not adopted:** dPKMS uses structured decomposition (sections, entities, mentions) — not fixed-size chunking — so the context-loss problem doesn't exist. Useful takeaway: rank fusion (4:1 dense:BM25) consistently helps across embedding models, validating our existing RRF approach. Embedding model choice matters more than chunking strategy.

**Related:** ADR-022 (Hybrid Search), ADR-028 (Atomic Notes), ADR-044 (ContextualRetriever), ADR-047 (C-Norm) — same "inapplicable architecture" pattern

### **ADR-048 – Visual SSL Scaling: Informative for Image Similarity Search (Accepted)**
Fan et al. (Meta FAIR/NYU, Apr 2025) demonstrate vision-only SSL matches CLIP at 7B parameters with better scaling. **Not directly adopted** (we consume VLMs via API, not raw encoders), but **establishes a design principle for first-class image support:**

**Key insight:** Image knowledge objects should carry **dual embeddings** — text (from VLM description) and visual (from vision encoder) — enabling both "search by description" and "find visually similar images." The existing embeddings schema (`model TEXT NOT NULL`) already supports this without changes.

**Enables future feature:**
- `ctxt search --similar-to image.png` — visual similarity search across knowledge base
- Image-to-image retrieval for diagrams, screenshots, whiteboards

**Model selection guidance:** Prefer models trained on document/chart-heavy data (+13.6% OCR/Chart improvement from text-filtered training). Implementation waits for `image.*` pipelines (US-0003) and visual embedding API availability.

**Related:** ADR-026 (Multimodal), ADR-022 (Hybrid Search), ADR-021 (Multi-Backend), US-0003 (Image OCR), US-0051 (Semantic Search)

### **ADR-062 – Information/Knowledge Lifecycle Model (Proposed)**
Formal lifecycle model distinguishing perishable information (short shelf life, relevance decays
over time) from durable knowledge (persistent, earned through repeated interaction and
corroboration). Introduces three object tiers (`information | consolidating | knowledge`),
a configurable exponential decay model with TTL defaults per pipeline type, an
interaction-driven confidence growth model, and tier-aware behaviour across resurfacing,
export, thin sync, and graph edge confidence. Adds `ctxt inbox --stale` as a user-facing
triage surface for expired/decaying objects.

**Schema additions:** `object_tier`, `expires_at`, `decay_score`, `interaction_count`,
`source_count` on `objects`. Edge `weight` semantics formalised.

**Amends:** ADR-016 (resurfacing), ADR-020 (export), ADR-034 (thin sync), ADR-042 (Gap 3),
ADR-049 (edge weight semantics).

### **ADR-043 – BEAM/LIGHT: Long-Term Memory Techniques Partially Adopted (Accepted)**
BEAM/LIGHT (Tavakoli et al., ICLR 2026) benchmarks 10 cognitive memory abilities across 100K–10M token conversations and provides LIGHT, a three-component memory framework (episodic + working + scratchpad). Full system not adopted (requires 32B model per turn, Python/GPU stack). **Three techniques adopted:**

1. **Per-query noise filtering** as reranking step — empirically validated -8.3% degradation without it at 10M tokens. Implementable via constrained generation (`LMQL in("yes","no")` per candidate). Phase 3.
2. **LIGHT scratchpad design** supersedes HEMA compact memory (ADR-040) — four memory categories, threshold compression, relevance filtering. Phase 3.
3. **Retrieval budget k=15** as evidence-based default — k=20 degrades from noise, k=5 loses 6-8%. Immediate.

**Key validation:** Contradiction resolution confirmed as unsolved (0-5% across all methods), validates ADR-042 Gap 1. BEAM's 10-ability taxonomy noted as evaluation framework for ctxt search quality.

**Related:** ADR-040 (HEMA, scratchpad upgrade), ADR-042 (contradiction resolution validation), ADR-022 (hybrid search), ADR-011 (reranking)

### **ADR-063 – Graph-Canonical Knowledge Object (Accepted)**
Establishes the KnowledgeObject as the canonical graph node and pipeline-draft target. Mentions, edges, and graph queries all anchor on this canonical type. Foundation for ADR-053's pipeline-draft model.

### **ADR-064 – Federation (Accepted)**
Push-only DAG federation between dPKMS instances. Each instance pushes objects + edges (not jobs/feeds/pipelines/indexes) to zero or more federation targets. Targets can be local SQLite paths or remote HTTP endpoints. Sync modes: async (background) or inline (synchronous during pipeline). Content-hash idempotent at target. Multi-platform deployments compose via federation, not via multi-backend single-protocol adapters. *Amended by ADR-074:* Phase 3's same-owner scope is decided there as a separate protocol; cross-owner pull remains deferred.

### **ADR-065 – Pluggable Adapters: Protocol Server + Read/Write Substrate (Proposed)**
Typed adapter substrate at `internal/adapter/` with five concepts: protocol slots (email, contacts, calendar, …), adapters (with capabilities: fetch / serve / submit / emit-events / subscribe-events), backends (concrete platform configs: stalwart, mxhook+gmail, cardamum, himalaya, …), one-platform-per-protocol invariant enforced at registration, lifecycle on kit/runtime/bus. Migrates legacy `internal/ingest/` adapters (cardamum, himalaya) to the typed substrate via a backwards-compat shim. Federation composition (ADR-064) handles multi-platform deployments.

### **ADR-066 – Ambient Capture Substrate (Local-Side Daemon) (Accepted)**
Long-running local daemon (`ctxd`) hosts pluggable ambient sources (clipboard, file-watch, browser-history, foreground-window, screenshot, meeting) feeding existing pipelines via the existing `/api/v1/analyze` HTTP path. Runs **client-side** because dpkms is often deployed remote. Pluggable buffer (local-FS XDG / S3-compatible / in-memory). Client-side fingerprint dedup at enqueue. kit/runtime/policy CEL guards enforce privacy before network egress. Three launch paths share one binary: `ctxt capture --ambient`, `brew services start ctxd`, or service-manager. Comprehensive bus event taxonomy at every transformation. Borrows substrate patterns from OpenChronicle without its screen-memory scope.

**Diagrams:** [`docs/diagrams/ambient/066-architecture.mmd`](../diagrams/ambient/066-architecture.mmd), [`066-event-flow.mmd`](../diagrams/ambient/066-event-flow.mmd), [`066-buffer-backends.mmd`](../diagrams/ambient/066-buffer-backends.mmd).

### **ADR-067 – Session / WorkUnit as a First-Class Type (Accepted)**
Adds `Session` to `pkg/pluginapi/` and `SessionID` to `KnowledgeObject`. Three-rule cutter (idle 5min / soft-cut 3min + frequent-switching exception / 2h timeout) ported verbatim from OpenChronicle's `session/manager.py`. Cutter runs **client-side in `ctxd`** because the foreground-window signal is local. dpkms persists sessions as opaque metadata via PUT `/api/v1/sessions/{id}` (idempotent). Soft-FK on `objects.session_id` absorbs network-loss replay ordering. v1 is grouping-only; per-session reducer deferred to Phase 6+. Schema reserves `flush_end` and `classified_end` bookmarks for the future reducer.

**Diagrams:** [`docs/diagrams/ambient/067-lifecycle.mmd`](../diagrams/ambient/067-lifecycle.mmd), [`067-cutter-flowchart.mmd`](../diagrams/ambient/067-cutter-flowchart.mmd), [`067-replay-sequence.mmd`](../diagrams/ambient/067-replay-sequence.mmd).

### **ADR-068 – MCP Read-Surface (Dual: dpkms-Authoritative + ctxd-Local) (Accepted)**
Two MCP servers expose the knowledge graph and live local state to AI agents (Claude Code, Claude Desktop, Cursor, Codex, opencode). dpkms-side (`/api/v1/mcp/`) is authoritative with 10 tools (search, list, get, entity, recent, sessions, session, compose, mentions, schema). ctxd-side (`:8744/mcp`) is local-only with 5 tools (current_session, recent_local, pending_enqueue, sources, health) surfacing state dpkms cannot see when remote. Read-only by design; writes go through existing `ctxt analyze`. Streamable-HTTP per MCP spec 2025-03-26. `ctxt mcp install <client>` ports OpenChronicle's mature install matrix. Closes ADR-038's deferred MCP commitment (US-0037–US-0041).

**Diagrams:** [`docs/diagrams/ambient/068-topology.mmd`](../diagrams/ambient/068-topology.mmd), [`068-tool-dispatch.mmd`](../diagrams/ambient/068-tool-dispatch.mmd), [`068-tool-surface.mmd`](../diagrams/ambient/068-tool-surface.mmd).

### **ADR-069 – Meeting Capture Source (Audio + Video, Multi-Platform) (Accepted)**
Specialized ambient source for video calls (Zoom, Meet, Teams, FaceTime, Discord). Captures system audio + window framebuffer via OS-blessed public APIs (ScreenCaptureKit on macOS 13+, WASAPI loopback + Graphics Capture on Windows 10+, xdg-desktop-portal + PipeWire on Linux). Routes to existing `audio.transcribe` (diarization, alignment, sectioning) or `video.full` (transcript + frame OCR + scene-aligned timeline) pipelines. Explicit-trigger only in v1 (no always-on). Mandatory recording indicator. First-class redact-as-supersede. Mobile companion apps (iOS / Android) deferred to Phase 6/7 (separate repos, separate tracks). 22-topic bus event taxonomy covers every state transition. Storage gets a separate retention tier (default 48h local + optional S3 archive).

**Diagrams:** [`docs/diagrams/ambient/069-recording-state.mmd`](../diagrams/ambient/069-recording-state.mmd), [`069-multi-platform.mmd`](../diagrams/ambient/069-multi-platform.mmd), [`069-recording-sequence.mmd`](../diagrams/ambient/069-recording-sequence.mmd).

### **ADR-070 – Pipeline & Index Versioning, Upgrade Taxonomy, and Operator Consent (Accepted)**
Three-bucket upgrade taxonomy (`reindex_auto`, `reingest_selective`, `reingest_all`) classifies every change that affects stored knowledge, the index, or pipeline contract. Per-object `pipeline_version` (`name@vN`) lets old objects keep their contract while new ingests adopt the corrected behavior. Per-table index signatures (tokenizer-, projection-, embedding-format-hashes in `index_signatures`) drive automatic bucket-1 rebuilds on startup signature mismatch. Release-notes contract at `docs/release-notes/<date>.md` + `Operator-Impact:` commit trailer + CI gate (`.github/workflows/release-notes-check.yml`) enforce the taxonomy at PR-time. Operator-facing CLI: `ctxt upgrade {plan,run,status,status --watch}` with banner injection in every CLI command during in-flight upgrades; `/healthz` upgrade envelope; `staleness_warning` field on search responses. Test substrate: `hop.top/xrr` cassettes (already in use) for upgrade-time external calls; `hop.top/eva` contracts for operator-facing JSON shapes (new adoption); `hop.top/ben` recall benchmarks gating `reingest_selective` PRs (new adoption). Strict-minimum design discipline: bucket 2 quarterly, bucket 3 annually.

### **ADR-071 – Embedding Index Versioning and Dual-Write Migration (Accepted)**
Single `embeddings` table with `(object_id, model_id, chunk_idx)` composite key plus `embedding_models` registry plus dual-write at ingest during migration windows plus operator-driven default-flip with coverage + recall guards. Solves graceful embedding-model migration without query blackout. Designs-for (does not build) heterogeneous routing, sovereignty tiers, score fusion, on-device fallback. Three preconditions before implementation: ADR-070 shipping, `hop.top/ben` recall suite at `suites/recall-vector.ben.yaml`, second concrete use case beyond migration. `ctxt embeddings {list,register,migrate,set-default,deprecate,purge}` operator surface. Test substrate: ben suite (mandatory before migrate ships), xrr cassettes for embedding API calls, eva contract for `embeddings list` shape.

### **ADR-074 – Same-Owner Device Sync: Op-Log Replication with Deterministic Merge (Proposed)**
Third replication mode (vs. push federation and backup): 2–5 owned devices, all writable, offline-tolerant, converging without data loss — the conflict-resolution ADR that ADR-064 Phase 3 deferred, decided for the same-owner case as a separate protocol. Mechanism: semantic HLC-stamped op log (`sync_log`) co-located transactionally in the instance DB, exchanged by per-device sequence vectors, replayed through deterministic per-type merge rules (durable-id identity with alias-on-dedup-collision, LWW registers with preserved losers for content edits, OR-set graph nodes/edges with semantic-identity dedup, op-sum counters, status lattice with edit-revives-discard and delete-wins exceptions); causality encoding and conflict-copy policy are fleet-creation-time protocol options (per-op sequence-vector and replicated-truth defaults); digest anti-entropy + epoch resync backstop. Identity: owner-root → device delegation certs on the federation key-lifecycle primitives; signed hash-chained roster drives transport auth, sequence vectors, and tombstone-GC quorum (causal stability over the roster + retention floor). One envelope of origin-signed op batches over four transports: hub TLS + device-auth Provider on the `server.auth` seam (ADR-023 Layer 4), LAN mDNS+Noise, ciphertext-only relay, signed file bundle. Hub topology (remote always-on primary + local fallback) and mesh run the identical protocol — "primary" is a routing role, never a merge role. Derived indexes never replicate; blobs move content-addressed and lazy per blob-federation design. SQLite session extension and cr-sqlite evaluated and rejected on empirical probes.

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
