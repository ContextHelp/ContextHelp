---
title: "Workflow: Search and Retrieval"
description: Retrieve high-signal context quickly while keeping a deterministic path for repeatable analysis.
---

## Goal

Retrieve high-signal context quickly while keeping a deterministic path for repeatable analysis.

## Scope

- NLQ and structured query filtering
- Multi-strategy retrieval (metadata, FTS, vector, graph, registry)
- Explanations, saved searches, history, alerts

## Primary stories

- `US-0016` to `US-0021`
- `US-0051` to `US-0055`
- `US-0061`
- `US-0037`, `US-0038`

## Prerequisites

1. Content is ingested and completed in the job queue.
2. You can run both semantic (`ctxt find`) and structured (`ctxt list`) queries.
3. You can inspect object details (`ctxt open`).

## Query modes in current runtime

1. Semantic mode for exploration: `ctxt find "<natural language question>"`
2. Structured mode for deterministic retrieval: `ctxt list --q "<query-expression>"` plus explicit filters

Story-target note: `US-0037`/`US-0038` describe explicit query-schema and RSQL contracts that may be introduced or refined beyond current runtime behavior.

## Procedure

### Step 1: Start with semantic retrieval

```bash
ctxt find "recent decisions about checkout conversion"
ctxt find "authentication incident patterns" --limit 10
```

Apply profile context:

```bash
ctxt find "pricing rollout risks" --profile growth --limit 10
```

### Step 2: Narrow with structured filters

Type/date/tag filters:

```bash
ctxt list --type url --after 2026-02-01 --limit 20
ctxt list --tag checkout,pricing --limit 20
```

Query-language filters:

```bash
ctxt list --q "type==url;tag=in=(checkout,pricing)" --limit 20
ctxt list --mention @project.checkout-redesign --after 2026-02-01 --limit 20
```

### Step 3: Validate top results

Inspect candidate objects:

```bash
ctxt open <object_id>
ctxt open <object_id> --output json
```

Check:

1. Source and timestamp relevance
2. Mention/tag alignment with the query intent
3. Pipeline/source type appropriateness

### Step 4: Iterate toward a stable query

1. Start broad in semantic mode.
2. Move to structured filters when relevance improves.
3. Keep final structured query string for repeatability.
4. Re-run and compare top results to verify stability.

### Step 5: Prepare retrieval for downstream composition

Use structured filters to produce a bounded input set before running `ctxt make`.

## Outputs to validate

- Result relevance for top results
- Explanation payload correctness
- Stable result order for deterministic queries
- Query and filter set is reusable by other users/agents

## Common failure modes

### Too many irrelevant semantic matches

- Add profile context and reduce limit.
- Switch to structured filters (`--type`, `--tag`, `--after`, `--q`).

### Structured query returns nothing

- Remove one condition at a time.
- Verify underlying data exists with simpler `ctxt list` filters.
- Inspect a known object with `ctxt open` and adjust fields accordingly.

### Inconsistent repeated results

- Prefer explicit structured filters for high-stakes workflows.
- Avoid relying on broad semantic phrasing for deterministic use cases.

## Related references

- [`../reference/query-language-and-ranking.md`](../reference/query-language-and-ranking.md)
- [`../reference/api-cli-reference.md`](../reference/api-cli-reference.md)
- [`../troubleshooting/faq.md`](../troubleshooting/faq.md)
