# ADR-036 – SurfSense Evaluated and Not Adopted

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None

---

## Context

[SurfSense](https://github.com/MODSetter/SurfSense) (Apache-2.0, ~12,900 stars) is an open-source AI research assistant and knowledge management platform. It positions itself as a self-hostable alternative to NotebookLM, Perplexity, and Glean. It ingests content from 28+ connectors (Slack, Notion, GitHub, Gmail, Google Drive, Jira, Confluence, Discord, etc.), builds a searchable knowledge base with vector embeddings, and provides an LLM-powered conversational interface with cited answers.

SurfSense was evaluated because it overlaps with ContextHelp's problem space — fragmented knowledge with AI-powered search — and because its connector ecosystem, hybrid search implementation, and browser extension represent capabilities relevant to ctxt's roadmap.

### What SurfSense Does

- **28+ connectors** via OAuth or API (Slack, Notion, GitHub, Gmail, Google Drive, Jira, Linear, Confluence, Discord, Airtable, etc.)
- **50+ file formats** via ETL services (Docling, Unstructured, LlamaCloud)
- **Hybrid search:** pgvector cosine similarity + PostgreSQL tsvector/tsquery full-text search, fused via Reciprocal Rank Fusion (k=60)
- **Two-tiered RAG:** Document-level summary embeddings (coarse) + chunk-level embeddings (fine-grained, citation-capable)
- **Cited chat:** Perplexity-style answers with `[[citation:chunk_id]]` markers linked to source chunks
- **100+ LLM models** via LiteLLM gateway (OpenAI, Anthropic, Google, Ollama, vLLM, etc.)
- **6000+ embedding models** via sentence-transformers compatible APIs
- **Browser extension:** Plasmo Framework (Manifest V3) saves authenticated page content
- **Team collaboration:** RBAC (Owner/Admin/Editor/Viewer), shared SearchSpaces, Electric-SQL real-time sync
- **Report/podcast generation:** Export to PDF/DOCX, TTS-powered podcast from conversations
- **Agent architecture:** LangGraph + LangChain stateful agents with tool registry

### Tech Stack

| Layer | Technology |
|---|---|
| Frontend | Next.js 16, React 19, TypeScript, Tailwind v4, Radix UI |
| Backend | FastAPI (Python 3.12+), async SQLAlchemy |
| AI orchestration | LangGraph + LangChain, LiteLLM |
| Task queue | Celery + Redis |
| Database | PostgreSQL 14+ with pgvector |
| Full-text search | PostgreSQL tsvector/tsquery |
| Real-time sync | Electric-SQL |
| Auth | FastAPI Users, JWT + Google OAuth, Argon2 |

Minimum deployment requires: PostgreSQL + pgvector + Redis + Python 3.12 + Node.js 20 + Celery (worker + beat) + at least one LLM API key or local model.

---

## Decision

**SurfSense will not be adopted as a dependency, component, or architectural basis for ContextHelp. Specific patterns from its implementation are noted as reference material for future ctxt development.**

---

## Rationale

### Fundamental Architecture Mismatch

SurfSense is a **multi-service web application** (FastAPI + Next.js + PostgreSQL + Redis + Celery). ContextHelp is a **local-first Go CLI** targeting single-binary distribution with SQLite default (ADR-001, ADR-002). These are incompatible foundations:

- SurfSense requires PostgreSQL — ctxt requires SQLite-first with pluggable backends (ADR-021)
- SurfSense requires Redis + Celery — ctxt uses a transactional outbox job queue (ADR-007)
- SurfSense is Python/TypeScript — ctxt is Go (ADR-002)
- SurfSense's components do not decompose into embeddable libraries

### No Overlap in Core Differentiators

ctxt's architectural strengths have zero counterparts in SurfSense:

| ctxt Differentiator | SurfSense Equivalent |
|---|---|
| Knowledge graph with `@namespace.slug` entity mentions | None — document-centric, no entity resolution |
| Constrained generation (LMQL, instructor, outlines) | None — unconstrained LLM output |
| Focus profiles shaping ingestion | None — flat workspace model |
| RSQL deterministic query language for agents | None — LLM agent or hybrid search only |
| Step-based enrichment pipelines (ADR-004) | None — summary generation only |
| Plugin architecture extending any layer (ADR-012) | None — extension via source modification |
| Entity extraction with constrained schemas | None |
| Decision/task extraction with enum enforcement | None |
| Tag assignment from vocabulary sets | None |
| Federated registry protocol (ADR-008) | None |

### Heavyweight Dependency

SurfSense's minimum deployment (PostgreSQL + pgvector + Redis + Python + Node.js + Celery) directly contradicts ctxt's zero-install, single-binary philosophy. Even containerized, the resource footprint is substantial compared to a Go binary + SQLite.

### Maturity Concerns

- **Not production-ready** — explicitly stated by the project
- **Beta-only releases** (latest: beta-v0.0.13, Feb 2025)
- **Solo maintainer** with community PRs
- **Irregular release cadence** (1-3 week bursts with multi-month gaps)
- **No formal plugin/extension API** — all extension requires forking

### Scope Mismatch

SurfSense is a team collaboration platform optimized for chatting with aggregated SaaS data. ctxt is a personal knowledge substrate optimized for structured capture, constrained enrichment, and graph-based composition. SurfSense's design decisions (RBAC, shared workspaces, connector OAuth flows, Perplexity-style chat) serve a different user and a different problem.

### Team/Cloud Perspective (ADR-031, ADR-032, ADR-033)

ctxt supports three deployment modes: solo local (single binary + SQLite), team/org nodes (cloud-enrolled, RBAC, shared access), and monetized registries ("rent access to know-how" via entitlements and metered JIT pulls). SurfSense's team-oriented features — 28+ OAuth connectors, RBAC with roles, real-time collaboration, two-tiered RAG with citations, web UI — become more relevant when viewed through the team/cloud lens.

However, the adoption conclusion does not change:

1. **ADR-031 defines the cloud as a thin admin client over the same node binary.** The cloud layer does not replace the node runtime with a different stack. Adopting SurfSense's FastAPI/Celery/PostgreSQL would mean running a different runtime for cloud vs. local — which ADR-031 explicitly rejected.

2. **SurfSense's components are not decomposable.** The connector OAuth logic, hybrid search, RBAC system, and team workspaces are wired into FastAPI + SQLAlchemy + Celery. They cannot be extracted as standalone libraries for use in a Go runtime or a separate cloud layer.

3. **What the cloud layer needs** (web UI, connector OAuth, team workspaces) will be built as the **context.help cloud** non-OSS layer, not embedded in the node. That cloud layer can reference SurfSense's patterns without sharing code.

SurfSense is therefore elevated from "pattern reference" to **architectural case study** for the context.help cloud layer — particularly for connector OAuth flows, team workspace UX, and cited RAG response patterns.

### Alternatives Considered

| Option | Verdict |
|---|---|
| **Adopt SurfSense as core** | Rejected — wrong architecture, wrong language, wrong model |
| **Use SurfSense components** | Rejected — no decomposable libraries, Python-only |
| **Reference SurfSense patterns** | Accepted — specific patterns noted below |
| **Ignore entirely** | Rejected — useful reference material in search and connectors |

---

## Consequences

### Positive

- Core architecture remains clean and aligned with local-first, single-binary goals
- No heavyweight Python/PostgreSQL dependencies introduced
- Decision confirms ctxt's architectural differentiation (knowledge graph, constrained generation, plugin system, RSQL) is not available in comparable tools — these are genuine differentiators worth preserving

### Negative

- ctxt must build its own connector ecosystem from scratch when those stories are prioritized
- No shortcut to hybrid search implementation — must be built against SQLite/pgvector

### Neutral / Considerations

- SurfSense's existence validates the market for self-hosted AI knowledge tools
- Its connector list (28+ services) provides a useful priority reference for future ctxt connector plugins

---

## Implementation Notes

No implementation is required. The following SurfSense patterns are noted as reference material for future ctxt development:

### 1. Hybrid Search with RRF (k=60)

SurfSense's two-tiered hybrid search is well-engineered and relevant to ctxt's ADR-022 implementation:

- **Document-level:** Fetch `top_k * 2` candidates from each modality (semantic + keyword), fuse via RRF
- **Chunk-level:** Fetch `top_k * 5` chunks to account for document grouping, aggregate under parent documents, select top-k by best chunk score
- **RRF formula:** `score = 1.0 / (60 + semantic_rank) + 1.0 / (60 + keyword_rank)`

This pattern is applicable when implementing ctxt's multi-strategy search (US-0018).

### 2. Chunking Strategy

SurfSense uses [Chonkie](https://github.com/chonkie-ai/chonkie) with `RecursiveChunker` for text and `CodeChunker` for source code, sized to the embedding model's `max_seq_length`. This auto-sizing approach is worth considering for ctxt's atomic decomposition (ADR-028).

### 3. Browser Extension for Authenticated Content

The Plasmo-based Manifest V3 extension captures rendered page content behind authentication walls — content that URL fetchers cannot reach. This is relevant for a future ctxt browser capture story.

### 4. Connector OAuth Patterns

SurfSense's OAuth flow implementations for 16+ services (Google, Slack, Notion, GitHub, etc.) provide useful reference when building ctxt connector plugins. The pattern of periodic Celery Beat reindexing for staleness management is worth noting.

### 5. Cloud Layer Case Study (ADR-031, ADR-032, ADR-033)

When building the context.help cloud non-OSS layer, SurfSense's implementation provides an architectural case study for:

- **Team workspace UX:** SearchSpaces with RBAC roles (Owner/Admin/Editor/Viewer) map to ADR-033's team RBAC compiled down to scopes. Study their membership management and permission UI patterns.
- **Connector management UI:** How users configure, authorize, and monitor 28+ OAuth connectors. Relevant for the cloud admin UI managing connector plugins on enrolled nodes.
- **Cited RAG responses with provenance:** The `[[citation:chunk_id]]` pattern with source attribution is directly relevant for "rent access to know-how" scenarios (ADR-032) where consumers querying a monetized registry need verifiable provenance for credited responses.
- **Real-time collaboration:** Electric-SQL for live sync between team members. Relevant for team nodes where multiple users share a knowledge base.

---

## References

- ADR-001 – Local-First and Decentralized
- ADR-002 – Use Go
- ADR-004 – Step-Based Pipeline Architecture
- ADR-007 – Transactional Outbox for Ingestion
- ADR-008 – Remote Knowledge Registry Protocol
- ADR-012 – Plugins Extend Any Layer
- ADR-021 – Multi-Backend Storage Strategy
- ADR-022 – Vector Indexing and Hybrid Semantic Search
- ADR-028 – Atomic Notes and Decomposition Strategy
- ADR-031 – Nodes and context.help cloud Boundary
- ADR-032 – Entitlements and Metering as Policy Predicates
- ADR-033 – Team RBAC as an Additive Layer Over Scopes
- US-0018 – Multi-Strategy Search Execution
- [SurfSense GitHub Repository](https://github.com/MODSetter/SurfSense)
- [SurfSense Documentation](https://www.surfsense.com/docs)
- [SurfSense Docker Installation](https://www.surfsense.com/docs/docker-installation)

---
