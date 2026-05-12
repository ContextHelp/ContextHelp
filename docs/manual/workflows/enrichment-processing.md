# Workflow: Enrichment and Processing

## Goal

Ensure ingested content is transformed into high-quality structured context (entities, tags, decisions, summaries, and related signals).

## Scope

- Entity, decision, task, tag, summary extraction
- Constrained extraction
- Batch enrichment and progress tracking

## Primary stories

- `US-0009` to `US-0015`
- `US-0046` to `US-0050`
- `US-0041`

## Prerequisites

1. Ingestion path is operational (`ctxt analyze` or `dpkms pipeline enqueue`).
2. Pipelines/providers are configured for your environment.
3. You can inspect jobs and objects (`ctxt job`, `ctxt open`).

## Procedure

### Step 1: Choose enrichment strategy

Default enrichment through ingestion:

```bash
ctxt analyze "Decision: defer tablet support to Q2" --type text --hints "#decision #mobile"
```

Pipeline-targeted enrichment:

```bash
ctxt analyze --file ./notes/architecture-review.md --type text --pipeline text.long
```

Integration path:

```bash
dpkms pipeline enqueue --type text --pipeline text.long "Architecture tradeoff summary"
```

### Step 2: Add structure cues for better extraction

Use hints and mentions to reduce ambiguity:

```bash
ctxt analyze "Owner: team growth. Action: redesign checkout CTA." \
  --type text \
  --hints "#action #checkout" \
  --mentions "@team.growth"
```

Use role context to bias extraction:

```bash
ctxt analyze "Investigate auth error surge" --type text --profile engineering
```

### Step 3: Monitor enrichment throughput and failures

```bash
ctxt job list --state running --limit 20
ctxt job list --state completed --limit 20
ctxt job list --state failed --limit 20
```

Inspect one failed job:

```bash
ctxt job status <job_id>
ctxt job log <job_id>
ctxt job retry <job_id>
```

### Step 4: Inspect enriched outputs

Find recent objects likely affected:

```bash
ctxt list --after 2026-02-01 --limit 20
ctxt list --pipeline text.long --limit 20
```

Inspect full object payload:

```bash
ctxt open <object_id> --output json
```

Look for:

1. Expected summary/section fields
2. Mentions and tags present
3. Decision/task extraction when applicable
4. Correct pipeline attribution

## Constrained extraction note

Stories `US-0014` and `US-0041` define constrained extraction patterns. Availability depends on configured provider/pipeline capabilities in your runtime.

## Outputs to validate

- Expected enrichment fields are present
- Constrained output is schema-valid
- Batch progress and failure details are available
- Enrichment quality is consistent across repeated similar inputs

## Common failure modes

### Low-quality extraction

- Re-ingest with better `--hints` and `--mentions`.
- Route to a richer pipeline with `--pipeline text.long`.
- Apply profile context for domain focus.

### Batch backlog

- Watch running/pending job counts.
- Reduce batch size temporarily.
- Prioritize critical content using dedicated pipeline runs.

### Schema inconsistency across outputs

- Validate provider and pipeline configuration.
- Compare successful and failed job logs for the same content class.

## Related references

- [`./entity-auto-enrichment.md`](./entity-auto-enrichment.md)
- [`../reference/query-language-and-ranking.md`](../reference/query-language-and-ranking.md)
- [`../operations/runbook.md`](../operations/runbook.md)
- [`../troubleshooting/faq.md`](../troubleshooting/faq.md)
