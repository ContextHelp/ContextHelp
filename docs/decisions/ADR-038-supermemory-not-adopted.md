# ADR-038 – SuperMemory Evaluated and Not Adopted

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None

---

## Context

[SuperMemory](https://github.com/supermemoryai/supermemory) (MIT license, ~16,500 stars) is an AI memory API platform. It provides persistent, structured memory for LLM-based agents and chatbots — a hosted infrastructure layer so AI applications can remember user preferences, past conversations, and ingested documents across sessions. It positions itself as an alternative to Mem0, Zep, and similar AI memory services.

SuperMemory was evaluated because it operates in a related space — capturing, storing, and retrieving knowledge with AI — and because its memory relationship model, decay mechanism, and MCP server represent concepts potentially relevant to ctxt's agent integration stories (US-0037 through US-0041).

### What SuperMemory Does

- **Memory API** for AI applications — `memory()` to store, `recall()` to retrieve
- **Connectors** for Google Drive, Notion, OneDrive, GitHub, Gmail, web crawling
- **50+ file formats** ingested via text, URL, PDF, image (OCR), video (transcription)
- **Semantic search** via pgvector cosine similarity (vector-only, no keyword fallback)
- **Memory relationships** — three types: `updates`, `extends`, `derives`
- **Memory decay** — less-accessed memories fade, important ones persist via reinforcement
- **Memory versioning** — `isLatest`, `parentMemoryId`, `rootMemoryId` chain
- **User profiles** — dynamic and static facts extracted from interactions
- **100+ LLM models** via Vercel AI SDK (OpenAI, Anthropic, Google, Cerebras, xAI)
- **MCP server** exposing memory/recall tools for AI assistants
- **SDK middleware** for Vercel AI SDK, OpenAI SDK, Mastra
- **Browser extension** (WXT framework, Chrome/Firefox) + Raycast extension
- **Memory graph visualization** (React + d3-force)

### Tech Stack

| Layer | Technology |
|---|---|
| Runtime | Cloudflare Workers (Hono framework) |
| Frontend | Next.js 16, React 19, TypeScript, Tailwind |
| Database | PostgreSQL + pgvector + Drizzle ORM |
| Cache/storage | Cloudflare KV, R2, Hyperdrive |
| AI | Vercel AI SDK v5, Cloudflare AI Gateway |
| Auth | better-auth with organization support |
| Build | Turbo monorepo, Bun 1.2 |
| Real-time | Cloudflare Durable Objects |

Minimum deployment requires: Cloudflare Workers account + PostgreSQL + pgvector + Cloudflare KV + R2 + AI Gateway + Hyperdrive + OpenAI API key + Resend account. 21 environment variables.

---

## Decision

**SuperMemory will not be adopted as a dependency, component, or architectural basis for ContextHelp. Specific concepts from its design are noted as reference material for future ctxt development.**

---

## Rationale

### Fundamental Problem-Space Mismatch

SuperMemory and ctxt/dPKMS solve different problems for different users:

| Dimension | SuperMemory | ctxt/dPKMS |
|---|---|---|
| **Primary purpose** | AI memory API — give agents persistent memory | Personal knowledge substrate — give humans sovereign knowledge |
| **Target user** | Developers building AI applications | Knowledge workers, agents, and platform integrators |
| **Deployment model** | Cloud SaaS on Cloudflare | Single Go binary, local-first |
| **Data model** | Document → chunk → memory (flat, vector-indexed) | Knowledge object → entity → edge (graph, multi-index) |
| **Query model** | Semantic similarity only | RSQL (deterministic) + NLQ (semantic) + graph traversal |

These are not competitors. They operate in adjacent but distinct domains.

### Cloud-Locked Architecture

SuperMemory has a **hard dependency on Cloudflare infrastructure**:

- Cloudflare Workers (compute)
- Cloudflare KV (hot storage)
- Cloudflare R2 (object storage)
- Cloudflare AI Gateway (embeddings)
- Cloudflare Hyperdrive (DB connection pooling)
- Cloudflare Durable Objects (real-time sync)

This is the architectural opposite of ctxt's local-first, zero-install, single-binary philosophy (ADR-001, ADR-002). SuperMemory cannot run offline, on SQLite, without internet, or without a Cloudflare account. Open issue #726 confirms inability to build and run locally. Self-hosting is enterprise-only.

### Zero Overlap on Core Differentiators

| ctxt Differentiator | SuperMemory Equivalent |
|---|---|
| Knowledge graph with `@namespace.slug` mentions | None — flat document/chunk/memory hierarchy |
| Constrained generation (LMQL, instructor, outlines) | None — unconstrained LLM output with Zod post-validation |
| Focus profiles shaping ingestion/search | None — flat workspace model |
| RSQL deterministic query language | None — semantic similarity only |
| Step-based enrichment pipelines (ADR-004) | None — extraction is LLM prompt-based |
| Plugin architecture extending any layer (ADR-012) | None — extension via code modification |
| Entity extraction with schema constraints | None — LLM-based, unstructured |
| Decision/task extraction with enum enforcement | None |
| Tag assignment from vocabulary sets | None |
| Federated registry protocol (ADR-008) | None |
| Hybrid search (FTS + vector + graph) | Semantic only, no keyword or graph |
| Audio-first ingestion | No (only via video transcription) |
| Feed/RSS sync | None |
| Document decomposition into sections | None |

### Team/Cloud Perspective (ADR-031, ADR-032, ADR-033)

ctxt supports three deployment modes: solo local, team/org nodes (cloud-enrolled with RBAC), and monetized registries ("rent access to know-how" via entitlements and metered JIT pulls). SuperMemory's concepts were re-evaluated through this lens:

| SuperMemory Concept | Team/Cloud Relevance |
|---|---|
| Metered memory API (`memory()` / `recall()`) | A knowledge provider could expose `search()` / `compose()` as a metered API — maps to ADR-032 entitlements |
| Container tags for multi-tenancy | Maps to ADR-032's per-object access labels for gating monetized content |
| MCP server | Agents consuming rented knowledge need a standard protocol — directly relevant for monetized registry access |
| Memory decay | Less relevant for monetized knowledge — paid content should not fade |

The **MCP server pattern** is the most directly relevant piece for the monetized scenario. If someone rents access to a curated knowledge base, their AI agents need a standardized way to query it. SuperMemory's `memory()` / `recall()` via MCP is a clean model — though ctxt would expose richer operations (`search()` with RSQL, `compose()` with templates, `ingest()` with pipeline selection).

However, the adoption conclusion does not change:

1. **ADR-031 defines the cloud as a thin admin client over the same node binary.** SuperMemory's Cloudflare Workers runtime is not compatible with this model.
2. **The node binary stays the same** whether solo or cloud-enrolled. Team RBAC (ADR-033) compiles down to scopes. Entitlements (ADR-032) are pluggable policy checks. These are additive layers on the Go binary, not a different application.
3. **SuperMemory's multi-tenancy (container tags)** is a flat isolation model. ctxt's entitlement system is richer — subscription entitlements, credit metering, per-object access labels, signed receipts — and operates at the policy layer, not the data layer.

### Additional Concerns

- **Single-maintainer risk:** 759 of ~1,300 commits from one person
- **Security:** 34 findings flagged in Argus scan (14 high, 11 medium — issue #712)
- **No stable release:** Active development but no production-readiness declaration for self-hosted
- **TypeScript-only:** No Go components, nothing embeddable in the ctxt runtime

### Alternatives Considered

| Option | Verdict |
|---|---|
| **Adopt SuperMemory as core** | Rejected — cloud-locked, wrong problem space, wrong language |
| **Use SuperMemory components** | Rejected — no decomposable libraries, Cloudflare-coupled |
| **Reference SuperMemory concepts** | Accepted — specific concepts noted below |
| **Ignore entirely** | Rejected — memory decay and MCP patterns are useful references |

---

## Consequences

### Positive

- Core architecture remains local-first and cloud-independent
- No Cloudflare or TypeScript dependencies introduced
- Decision confirms ctxt's unique position: no evaluated tool combines local-first execution, constrained generation, knowledge graph, plugin system, and deterministic queries — these are genuine differentiators

### Negative

- ctxt must design its own memory lifecycle management (decay, reinforcement) if that feature is prioritized
- No shortcut to MCP server implementation — must be built from scratch

### Neutral / Considerations

- SuperMemory's cloud-first success validates demand for AI memory infrastructure — ctxt could serve a similar role for local-first and privacy-sensitive contexts
- Their 16,500 stars indicate strong market interest in the "memory for AI" framing

---

## Implementation Notes

No implementation is required. The following SuperMemory concepts are noted as reference material:

### 1. Memory Decay / Intelligent Forgetting

SuperMemory implements memory decay where less-accessed memories fade unless reinforced. Fields: `isForgotten`, `forgetAfter`, `forgetReason`. This concept has no ctxt equivalent today.

Potential ctxt application: A focus profile feature where knowledge objects outside the active profile gradually decrease in search ranking weight, resurfacing only when explicitly queried or when a related entity is mentioned. This aligns with the surfacing engine (ADR-016) and could reduce noise in long-lived knowledge bases.

### 2. Memory Versioning and Relationship Types

SuperMemory tracks memory evolution via:
- `isLatest` / `parentMemoryId` / `rootMemoryId` — version chain
- Three relationship types: `updates` (supersedes), `extends` (adds to), `derives` (inferred from)

ctxt already has a richer model (knowledge graph with entity mentions and typed edges), but the explicit `updates`/`extends`/`derives` taxonomy is worth considering when defining edge types for the `edges` table. It maps cleanly to knowledge evolution patterns:

- `updates` → entity revision (new information supersedes old)
- `extends` → entity elaboration (adds detail to existing)
- `derives` → entity inference (generated from analysis)

### 3. MCP Server Pattern (Elevated: Directly Relevant for Monetized Access)

SuperMemory exposes `memory()` and `recall()` tools via the Model Context Protocol, allowing any MCP-compatible AI assistant to store and retrieve memories. When ctxt implements agent integration (US-0037 through US-0041), an MCP server exposing `ingest()`, `search()`, and `compose()` tools would enable the same integration pattern without coupling to a specific AI framework.

This pattern is **directly relevant for the "rent access to know-how" scenario** (ADR-032). A knowledge provider running a cloud-enrolled node could expose an MCP server as the metered access interface. External agents would call `search()` or `compose()` via MCP, with each call gated by the entitlement provider (subscription check → credit deduction → signed receipt). The MCP protocol provides a clean boundary between the knowledge provider's node and the consuming agent, with the entitlement layer invisible to the agent.

### 4. MemoryBench Evaluation Framework

SuperMemory's open-source [MemoryBench](https://github.com/supermemoryai/memorybench) framework evaluates memory system quality. This could be adapted to benchmark ctxt's search and retrieval quality, particularly for comparing hybrid search strategies (ADR-022).

---

## References

- ADR-001 – Local-First and Decentralized
- ADR-002 – Use Go
- ADR-004 – Step-Based Pipeline Architecture
- ADR-008 – Remote Knowledge Registry Protocol
- ADR-012 – Plugins Extend Any Layer
- ADR-016 – Just-In-Time Surfacing
- ADR-022 – Vector Indexing and Hybrid Semantic Search
- ADR-031 – Nodes and context.help cloud Boundary
- ADR-032 – Entitlements and Metering as Policy Predicates
- ADR-033 – Team RBAC as an Additive Layer Over Scopes
- US-0037 – Agent Discovers Query Schema
- US-0038 – Agent Constructs RSQL Query
- [SuperMemory GitHub Repository](https://github.com/supermemoryai/supermemory)
- [SuperMemory Documentation](https://docs.supermemory.ai)
- [MemoryBench](https://github.com/supermemoryai/memorybench)

---
