# Knowledge Workers Guide

This chapter is a complete operating guide for knowledge workers using `ctxt` to capture, retrieve, and compose context.

## Goal

Use `ctxt` to move from raw notes and links to a shareable brief with provenance, in a repeatable workflow.

## Prerequisites

1. `ctxt` and `dpkms` binaries are installed.
2. A config file exists at `~/.config/contexthelp/config.yaml`.
3. `dpkms` server is running in a separate terminal:

```bash
dpkms serve
```

Note: some older story docs use `ctxt add`. In the current CLI, the canonical command is `ctxt analyze`.

## End-to-End Workflow

### Step 1: Capture content quickly

Capture a text insight:

```bash
echo "Signup drop-off spikes after payment step" | ctxt analyze --type text --hints "#signup #conversion"
```

Capture a URL:

```bash
ctxt analyze https://example.com/onboarding-teardown --type url
```

Capture a local file:

```bash
ctxt analyze --file ./notes/interview-notes.md --type text
```

When you need synchronous behavior:

```bash
ctxt analyze "Critical decision note" --type text --wait
```

### Step 2: Verify ingestion and enrichment status

List recent jobs:

```bash
ctxt job list
```

Inspect one job:

```bash
ctxt job status <job_id>
ctxt job log <job_id>
```

If a job fails:

```bash
ctxt job retry <job_id>
```

### Step 3: Discover context with search

Run natural-language search:

```bash
ctxt find "what are recent onboarding decisions"
```

Limit results for fast triage:

```bash
ctxt find "signup friction causes" --limit 5
```

Switch to object listing when you need filter control:

```bash
ctxt list --type url --tag signup,onboarding
ctxt list --after 2026-02-01 --before 2026-02-18
```

Inspect a specific object in detail:

```bash
ctxt open <object_id>
```

### Step 4: Apply focus profiles for role-specific relevance

Check available profiles:

```bash
ctxt profile list
ctxt profile view <profile_name>
```

Use a profile in capture and search:

```bash
ctxt analyze "Need pricing test plan" --type text --profile growth
ctxt find "pricing experiment risks" --profile growth
```

Set your default profile if needed:

```bash
ctxt profile set <profile_name>
```

### Step 5: Compose shareable outputs

Generate a brief from recent/tagged knowledge:

```bash
ctxt make brief --tag signup,onboarding --since 2026-02-01
```

Generate an action plan:

```bash
ctxt make plan --mention @project.signup-redesign --since 2026-02-01
```

Save output to a file:

```bash
ctxt make brief --tag signup,onboarding --since 2026-02-01 --output-file onboarding-brief.md
```

### Step 6: Validate provenance before sharing

1. Use `ctxt list` to identify contributing objects.
2. Open high-impact objects with `ctxt open <id>`.
3. Confirm timestamp, source, and mentions align with the draft.
4. Regenerate with narrower filters if the draft is too broad.

## 30-Minute Onboarding Path

1. Start server: `dpkms serve`
2. Ingest one text note and one URL.
3. Confirm both jobs complete.
4. Run one `ctxt find` query.
5. Generate one brief and save it.
6. Open at least one source object and validate provenance.

## Quality Checklist

- Capture commands return quickly and queue jobs.
- Job state reaches `completed` for core samples.
- Search results include expected topic signal in top results.
- Brief output is understandable without manual reformatting.
- Provenance can be traced to source objects.

## Common Pitfalls and Fixes

### Search feels irrelevant

- Add profile context with `--profile`.
- Narrow with `ctxt list` filters (`--tag`, `--after`, `--type`).
- Reduce result count and iterate query phrasing.

### Results are missing after capture

- Check `ctxt job list` for pending/running jobs.
- Use `--wait` for critical captures.
- Check failed job logs and retry.

### Generated brief is too generic

- Use stronger filters (`--tag`, `--mention`, `--since`).
- Split large topic into two focused briefs.
- Validate source objects and rerun composition.

## Story Alignment

- Ingestion: `US-0001` to `US-0008`, `US-0301` to `US-0317`
- Enrichment consumption: `US-0009` to `US-0012`, `US-0047`, `US-0048`
- Search: `US-0016`, `US-0019`, `US-0020`, `US-0021`, `US-0051`, `US-0053` to `US-0055`, `US-0061`
- Composition: `US-0022`, `US-0023`, `US-0025`, `US-0026`, `US-0056` to `US-0060`

## Related Manual Sections

- [`../workflows/ingestion-capture.md`](../workflows/ingestion-capture.md)
- [`../workflows/search-retrieval.md`](../workflows/search-retrieval.md)
- [`../workflows/composition-reporting.md`](../workflows/composition-reporting.md)
- [`../troubleshooting/faq.md`](../troubleshooting/faq.md)
