---
title: "Workflow: Composition and Reporting"
description: Convert retrieved knowledge into usable deliverables (briefs, plans, summaries, drafts) with clear provenance.
---

## Goal

Convert retrieved knowledge into usable deliverables (briefs, plans, summaries, drafts) with clear provenance.

## Scope

- Briefs, plans, timelines, stakeholder analysis, impact assessments, recommendations
- Export and sharing
- Custom templates

## Primary stories

- `US-0022` to `US-0026`
- `US-0056` to `US-0060`
- `US-0024`, `US-0040`

## Prerequisites

1. Relevant content is already ingested and searchable.
2. You can identify source scope via `ctxt list`/`ctxt find`.
3. You know the output type required (`brief`, `plan`, `summary`, `draft`).

## Procedure

### Step 1: Define source scope before composing

Identify candidate content:

```bash
ctxt find "checkout redesign decisions" --limit 10
ctxt list --tag checkout,decision --after 2026-02-01 --limit 20
```

Inspect key objects:

```bash
ctxt open <object_id>
```

### Step 2: Generate the right composition type

Brief:

```bash
ctxt make brief --tag checkout,pricing --since 2026-02-01
```

Plan:

```bash
ctxt make plan --mention @project.checkout-redesign --since 2026-02-01
```

Summary:

```bash
ctxt make summary --tag onboarding --since 2026-02-01
```

Draft:

```bash
ctxt make draft --tag launch --since 2026-02-01
```

### Step 3: Write output to artifacts

```bash
ctxt make brief --tag checkout,pricing --since 2026-02-01 --output-file checkout-brief.md
ctxt make plan --mention @project.checkout-redesign --since 2026-02-01 --output-file checkout-plan.md
```

### Step 4: Validate provenance and coverage

1. Check that major claims map back to known source objects.
2. Verify coverage of key stakeholders, decisions, and actions.
3. Regenerate with tighter filters if content is too generic.

### Step 5: Share or convert output

Primary output is Markdown via `--output-file`.

If PDF is required and your environment has conversion tooling:

```bash
pandoc checkout-brief.md -o checkout-brief.pdf
```

## Iteration pattern

1. Compose from broad scope.
2. Review and identify weak sections.
3. Tighten source filters (`--tag`, `--mention`, `--since`).
4. Regenerate and compare.

## Outputs to validate

- Provenance links for each major claim
- Output format compatibility (Markdown/PDF)
- Stakeholder and decision coverage completeness
- Deliverable is actionable for target audience

## Common failure modes

### Output is too generic

- Narrow source scope with stronger filters.
- Compose separately per subtopic instead of one broad document.

### Missing key stakeholder or decision

- Add mention-based filters and regenerate.
- Validate source ingestion and enrichment completeness first.

### Inconsistent output style across runs

- Standardize profile, time window, and tag set before each run.
- Persist command invocations in team runbooks for repeatability.

## Related references

- [`../reference/api-cli-reference.md`](../reference/api-cli-reference.md)
- [`./search-retrieval.md`](./search-retrieval.md)
- [`../troubleshooting/faq.md`](../troubleshooting/faq.md)
