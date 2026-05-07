---
title: "Workflow: Ingestion and Capture"
description: Capture raw content quickly and reliably so it becomes searchable and composable context.
---

## Goal

Capture raw content quickly and reliably so it becomes searchable and composable context.

## Scope

- Text, URL, image, audio, video, documents, feed sync, batch import
- Source importers (browser bookmarks, cloud drives, chat exports, note vaults, social archives)

## Primary stories

- `US-0001` to `US-0008`
- `US-0300` to `US-0317` (importer interface + source-specific importers)
- `US-0106` (enqueue via dPKMS API)
- `US-0113` (ctxt analyze API client)

## Prerequisites

1. `dpkms` server is running:

```bash
dpkms serve
```

2. `ctxt` client is configured (`~/.config/contexthelp/config.yaml`).
3. You know the content type you are ingesting (`text`, `url`, `image`, `audio`, `video`, `feed`, or `auto`).

## Procedure

### Step 1: Ingest content

Text:

```bash
echo "Checkout conversion dropped after copy change" | ctxt analyze --type text --hints "#checkout #conversion"
```

URL:

```bash
ctxt analyze https://example.com/postmortem --type url
```

Local file:

```bash
ctxt analyze --file ./notes/meeting.md --type text
```

Image:

```bash
ctxt analyze --file ./assets/wireframe.png --type image
```

Agent/integration path via dPKMS pipeline command:

```bash
dpkms pipeline enqueue --type text --pipeline text.short "Quick ingestion payload"
```

Importer path (current runtime + story target):

```bash
# Implemented runtime path (Chrome bookmarks)
ctxt import chrome --file ./bookmarks.html --dry-run
ctxt import chrome --file ./bookmarks.html

# Story-target paths defined by US-0305+ (when implemented)
ctxt import gdrive --since 2026-01-01
ctxt import notion --max-items 500
ctxt import linkedin --file ./linkedin-export.zip
```

### Step 2: Apply context controls when needed

Use focus profile:

```bash
ctxt analyze "Pricing experiment notes" --type text --profile growth
```

Force specific pipeline:

```bash
ctxt analyze --file ./docs/architecture.md --type text --pipeline text.long
```

Attach explicit mentions:

```bash
ctxt analyze "Action for checkout redesign" --type text --mention "@project.checkout-redesign"
```

### Step 3: Track job execution

List recent jobs:

```bash
ctxt job list --limit 20
```

Filter by state:

```bash
ctxt job list --state pending --limit 20
ctxt job list --state failed --limit 20
```

Inspect one job:

```bash
ctxt job status <job_id>
ctxt job log <job_id>
```

Retry failures:

```bash
ctxt job retry <job_id>
```

### Step 4: Verify resulting objects

List objects by type/tag/date:

```bash
ctxt list --type url --limit 20
ctxt list --tag checkout,conversion --after 2026-02-01
```

Open a specific object:

```bash
ctxt open <object_id> --output json
```

## Batch ingestion patterns

Batch URL ingestion from a file:

```bash
xargs -I{} ctxt analyze "{}" --type url < urls.txt
```

Batch Markdown ingestion from a directory:

```bash
find ./imports -type f -name "*.md" -print0 | xargs -0 -I{} ctxt analyze --file "{}" --type text
```

## Outputs to validate

- Object created with stable ID
- Source metadata and timestamps captured
- Enrichment kickoff successfully queued
- Failed jobs are visible and retryable
- Importer runs report scanned/imported/skipped/failed counts

## Common failure modes

### Jobs remain pending too long

- Confirm `dpkms serve` is running.
- Check worker load via `ctxt job list --state running`.
- Reduce ingest volume and retry failed jobs.

### Wrong pipeline/type detected

- Set `--type` explicitly.
- Force `--pipeline` for known document classes.

### Captured object missing expected context

- Re-ingest with `--hints`, `--mentions`, and `--profile`.
- Validate with `ctxt open <object_id> --output json`.

## Related references

- [`../reference/api-cli-reference.md`](../reference/api-cli-reference.md)
- [`../operations/runbook.md`](../operations/runbook.md)
- [`../troubleshooting/faq.md`](../troubleshooting/faq.md)
- [`../../stories/ingestion/US-0300-importer-extension-interface.md`](../../stories/ingestion/US-0300-importer-extension-interface.md)
