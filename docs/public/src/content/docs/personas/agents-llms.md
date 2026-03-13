---
title: Agents and LLMs Guide
description: This chapter is for autonomous clients that need reproducible retrieval, structured enrichment, and programmatic composition.
---

This chapter is for autonomous clients that need reproducible retrieval, structured enrichment, and programmatic composition.

## Goal

Build an agent loop that can ingest, retrieve, and compose context with predictable behavior and auditability.

## Current Implementation Path (Use This Today)

The current codebase supports deterministic filtering and job orchestration via CLI and core REST endpoints.

### 1. Start and verify runtime

```bash
dpkms serve
ctxt version
```

Optional health probe:

```bash
curl http://127.0.0.1:8080/health
```

### 2. Ingest content asynchronously

```bash
ctxt analyze "Investigate checkout funnel anomalies" --type text --hints "#checkout #funnel"
ctxt analyze https://example.com/incident-postmortem --type url
```

Track execution:

```bash
ctxt job list
ctxt job status <job_id>
ctxt job log <job_id>
```

Retry failures:

```bash
ctxt job retry <job_id>
```

### 3. Retrieve deterministically first, semantically second

Deterministic filter path:

```bash
ctxt list --type url --after 2026-02-01 --limit 20
ctxt list --q "type==url;tag=in=(checkout,pricing)" --limit 20
```

Semantic exploration path:

```bash
ctxt find "recent checkout architecture decisions" --limit 10
```

Inspect individual objects:

```bash
ctxt open <object_id> --output json
```

### 4. Compose programmatic outputs

```bash
ctxt make brief --tag checkout,funnel --since 2026-02-01 --output-file agent-brief.md
ctxt make plan --mention @project.checkout-redesign --since 2026-02-01 --output-file agent-plan.md
```

### 5. Agent loop contract (recommended)

1. Ingest (`ctxt analyze`)
2. Poll (`ctxt job status`)
3. Deterministic retrieval (`ctxt list --q ...`)
4. Semantic retrieval (`ctxt find ...`) if confidence is low
5. Compose (`ctxt make ...`)
6. Save artifacts and source IDs for audit replay

## REST and gRPC Programmatic Surface (Current Docs)

For non-CLI agents, use:

- REST: `POST /analyze`, `GET /jobs`, `GET /jobs/{id}`, `GET /objects`, `POST /compose`
- gRPC: `AnalyzeService`, `JobService`, `ObjectService`, `CompositionService`

References:

- [`../../api/api-rest.md`](../../api/api-rest.md)
- [`../../api/api-grpc.md`](../../api/api-grpc.md)

## Story-Target Path (Planned/Spec-Oriented)

The following story contracts describe intended agent ergonomics and may be ahead of current implementation:

- `US-0037`: `/query-schema` bootstrap contract
- `US-0038`: explicit RSQL construction contract
- `US-0039`: async ingestion and wait/poll patterns
- `US-0040`: programmatic brief composition
- `US-0041`: constrained enrichment usage

Use these as design targets for integrations that need future-compatible behavior.

## 30-Minute Agent Smoke Test

1. Start server and check health.
2. Submit one text and one URL ingestion request.
3. Wait until both jobs complete.
4. Run one deterministic `ctxt list --q ...` query.
5. Run one semantic `ctxt find ...` query.
6. Generate one brief and one plan.
7. Persist output files and source object IDs.

## Quality Checklist

- Job state transitions are captured and observable.
- Deterministic retrieval queries are stable across repeated runs.
- Semantic retrieval is used as a fallback, not the only path.
- Composition outputs include enough metadata to trace evidence.
- Failure handling covers timeout, empty result, and provider errors.

## Common Pitfalls and Fixes

### Ambiguous retrieval results

- Prefer `ctxt list --q` for strict filters.
- Use `ctxt find` only when deterministic filtering is insufficient.
- Store query string with each generated artifact.

### Ingestion-composition race conditions

- Never compose immediately after ingestion without polling.
- Gate composition on `completed` job state.

### Overlooking registry latency

- Separate local-first runs from federated runs in your agent policy.
- Track end-to-end latency per strategy.

### Contract drift between stories and runtime

- Validate commands/endpoints against current binaries and API docs.
- Treat `US-0037`/`US-0038` contracts as forward-looking when unavailable in current runtime.

## Story Alignment

- Bootstrap: `US-0037`, `US-0038`
- Ingestion/composition: `US-0039`, `US-0040`
- Constrained enrichment: `US-0014`, `US-0041`
- Advanced retrieval: `US-0018`, `US-0051`, `US-0052`, `US-0061`

## Related Manual Sections

- [`../workflows/search-retrieval.md`](../workflows/search-retrieval.md)
- [`../workflows/enrichment-processing.md`](../workflows/enrichment-processing.md)
- [`../reference/query-language-and-ranking.md`](../reference/query-language-and-ranking.md)
- [`../reference/api-cli-reference.md`](../reference/api-cli-reference.md)
