# Persona: Agents, LLMs, Tools

**Primary Role:** Autonomous systems (agentic frameworks, scheduled jobs, external tools, AI assistants)

---

## Goals

- Query the knowledge base reproducibly
- Capture content and know when enrichment has finished
- Get ranked results with an explanation of why they ranked
- Compose briefs, plans and drafts with citations back to source objects
- Run unattended: stable exit codes, machine-readable output, no prompts

---

## Interaction Pattern

### Query path

**MCP (agent clients)** — dpkms serves one read-only MCP endpoint, `/api/v1/mcp/` (scope `read:mcp` on protected instances), with two tools:

- `search` — `query` (required), `top_k` (1–100, default 10), `mode` (`hybrid` default, `fts`, `vector`), `min_score`, and filters `profile`, `meta_type`, `topic`, `person`, `source_type`, `since`, `until`.
- `schema` — the storage taxonomy: object kinds, edge types, pipelines, metadata fields. Call it before building structured queries.

Guide: [manual/workflows/mcp-agents.md](../manual/workflows/mcp-agents.md).

**REST / CLI (structured, deterministic)**

- RSQL over `GET /api/v1/search?q=<rsql>&limit=&offset=&profile=` or `ctxt list --q "<rsql>"`. Fields: `type`, `subtype`, `pipeline`, `source`, `created_at`, `updated_at`, `tag`, `mention`, `related` (shared mention targets), `similar`.
- Hybrid ranked search: `POST /api/v1/find` or `ctxt find "<query>"`; `--explain` adds the per-signal score breakdown, `--fts` / `--semantic` pick one leg.

```bash
ctxt list --q "type==url;tag=in=(checkout,pricing)" --format json
ctxt find "incident patterns" --explain --format json
```

There is no natural-language query translation on the server; `ctxt shell` only treats unknown input as a `find` query.

### Capture path

- `POST /api/v1/analyze` or `ctxt analyze ... --wait` (blocks until the job completes and returns the object ID).
- Poll `GET /api/v1/jobs/{id}`, or `WatchJob` over gRPC for a stream of status updates.
- `--idempotency-key` replays the recorded output for a repeated call; `--source-key` deduplicates by an external key (Slack ts, tweet ID).
- Exit codes: `70` when the instance is unreachable, `5` when it rejects the token. Nothing is queued locally.

### Enrichment

Enrichment runs in dpkms pipelines: language detection, tagging, entity extraction and resolution, summaries and sections, code-block extraction, OCR, transcription, vision, embeddings, c12n classification. Outputs are written to the object (tags, mentions, metadata). There is no constrained-decoding layer (LMQL, `instructor`, `outlines`): structured fields come from the step's own parsing and validation.

### Composition path

`ctxt compose brief|plan|summary|draft` renders locally from up to 100 objects selected by `--tagged`, `--mention`, `--since`, `--until`. Output carries inline `[ref:ID]` citations by default; `--export json` returns structured citations.

```bash
ctxt compose brief --tagged launch --export json
```

---

## Key Pain Points

- **Schema drift**: object fields evolve; call `schema` or read [ctxt/schema-object.md](../ctxt/schema-object.md) rather than hard-coding.
- **Result merging**: hybrid ranking fuses FTS and vector legs with RRF; weights (`--fts-weight`, `--vector-weight`, `--rrf-k`) change order.
- **Async enrichment**: capture returns before enrichment ends; use `--wait` or poll the job.
- **Auth on remote instances**: a token with the right role (`reader` for MCP and search, `writer` for capture).

---

## User Stories

Story pages are requirements; several describe behaviour that is not built (noted).

### Discovery & Bootstrap
- [US-0037](../stories/agents/US-0037-agent-discovers-query-schema.md) — Agent Discovers Query Schema (served by the MCP `schema` tool)
- [US-0038](../stories/agents/US-0038-agent-constructs-rsql-query.md) — Agent Constructs RSQL Query

### Content Ingestion
- [US-0039](../stories/agents/US-0039-agent-ingests-content-and-waits.md) — Agent Ingests Content and Waits

### MCP
- [US-0219](../stories/capture/US-0219-mcp-agent-integration.md) — MCP agent integration (only the dpkms endpoint with `search` and `schema` exists)
- [US-0220](../stories/capture/US-0220-local-mcp-during-network-loss.md) — Local MCP during network loss (not implemented: no local MCP server)

### Enrichment
- [US-0009](../stories/enrichment/US-0009-extract-entities-and-mentions.md) — Extract Entities and Mentions
- [US-0010](../stories/enrichment/US-0010-extract-decisions-and-tasks.md) — Extract Decisions and Tasks
- [US-0011](../stories/enrichment/US-0011-assign-tags-from-vocabulary.md) — Assign Tags from Vocabulary
- [US-0012](../stories/enrichment/US-0012-generate-summaries-and-sections.md) — Generate Summaries and Sections
- [US-0013](../stories/enrichment/US-0013-detect-and-extract-code-snippets.md) — Detect and Extract Code Snippets
- [US-0014](../stories/enrichment/US-0014-constrain-extraction-with-lmql.md) — Constrain Extraction with LMQL (not implemented)
- [US-0041](../stories/agents/US-0041-agent-uses-constrained-enrichment.md) — Agent Uses Constrained Enrichment (not implemented)

### Search & Retrieval
- [US-0018](../stories/search/US-0018-multi-strategy-search-execution.md) — Multi-Strategy Search Execution
- [US-0021](../stories/search/US-0021-search-with-result-explanation.md) — Search with Result Explanation
- [US-0051](../stories/search/US-0051-semantic-search-with-embeddings.md) — Semantic Search with Embeddings
- [US-0016](../stories/search/US-0016-natural-language-search.md) — Natural Language Search (not implemented)
- [US-0019](../stories/search/US-0019-federated-registry-search.md) — Federated Registry Search (not implemented)

### Composition
- [US-0040](../stories/agents/US-0040-agent-composes-brief-programmatically.md) — Agent Composes Brief Programmatically

---

## API Contracts

### REST (`/api/v1`)
- `GET /search?q=<rsql>` — RSQL query
- `POST /find` — hybrid ranked search
- `POST /analyze` — capture and enqueue
- `GET /objects/{id}`, `GET /objects/{id}/related` — object and related objects
- `GET /jobs/{id}` — job status
- `/mcp/` — MCP (`search`, `schema`)

Reference: [api/api-rest.md](../api/api-rest.md).

### gRPC
- `AnalyzeService.Analyze`; `JobService.GetJob`, `ListJobs`, `WatchJob` (stream)
- `QueryService.Search`, `ListObjects`, `GetObject`, `NodeAwareSearch`
- `EntityService.ListEntities`, `GetEntity`, `GetEntityBacklinks`

Reference: [api/api-grpc.md](../api/api-grpc.md), `api/proto/dpkms.proto`.

---

## Collaboration with Other Personas

- **Maintainers:** agents report API gaps and schema drift
- **Knowledge Workers:** agents consume what humans capture and return cited compositions
- **Operations:** agents surface failed jobs (`dpkms job list --state failed`) and slow queries
