# Core Workflows

Use this section when you want task-oriented execution, regardless of persona.

## Choose the right chapter

| If your goal is to... | Start here |
|---|---|
| Bring new content into the system | [`ingestion-capture.md`](./ingestion-capture.md) |
| Verify and improve extraction quality | [`enrichment-processing.md`](./enrichment-processing.md) |
| Retrieve relevant context fast | [`search-retrieval.md`](./search-retrieval.md) |
| Produce briefs, plans, and summaries | [`composition-reporting.md`](./composition-reporting.md) |

## Recommended execution order

`Ingestion -> Enrichment -> Search -> Composition`

This is the default chain for reliable output quality.

## Workflow contract

Each workflow chapter includes:

1. Goal and scope
2. Prerequisites
3. Step-by-step command flow
4. Output validation checklist
5. Common failure modes and fixes
6. Story alignment to `US-XXXX` references

## Runtime vs story targets

Some stories define future contracts (for example, explicit query-schema or RSQL-native endpoints). Workflow chapters call out where behavior is current-runtime versus story-target so you can build safely without guessing.
