# US-0305: Import Google Drive

**System Types:** ctxt, dpkms (self-hosted), dpkms cloud
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Operations](../../personas/operations.md), [Maintainers](../../personas/maintainers.md)

---

## User Goal

As a knowledge worker, I want to import data from Google Drive files and Google Docs exports so I can make existing knowledge searchable in ctxt without manual copy/paste.

---

## Context

Teams adopting ctxt already have high-value knowledge in external systems. This story defines a source-specific importer that keeps import behavior consistent with existing ingestion flows: parse source records, normalize them into draft knowledge objects, enqueue jobs, and preserve provenance metadata.

The importer must support both initial backfill and repeat sync runs. It should tolerate partial failures, report actionable errors, and avoid duplicate objects when the same source item is imported multiple times.

---

## Acceptance Criteria

- [ ] Importer accepts Google Drive files and Google Docs exports as input and validates required configuration.
- [ ] Auth flow supports OAuth 2.0 with Google.
- [ ] Importer supports first-time backfill and incremental sync using Drive changes token plus modified time fallback.
- [ ] Imported records map to object types: documents, sheets/slides exports, PDFs, text files.
- [ ] Importer stores provenance metadata including source, external ID, and import timestamp.
- [ ] Importer deduplicates using source + external ID + canonical URL hash.
- [ ] Partial failures do not abort the full run; failures are reported per item.
- [ ] Import run reports counts: scanned, imported, skipped, failed.
- [ ] Dry-run mode validates parsing and mapping without enqueueing jobs.

---

## Implementation Notes

### CLI Interface

- Example: ctxt import gdrive --profile default --since 2026-01-01
- Optional flags: --dry-run, --max-items, --since, --profile, --server

### Ingestion Flow

1. Validate config and source access.
2. Read source records (API pages, export files, or local vault files).
3. Normalize into draft objects with source metadata.
4. Enqueue each item through unified enqueue endpoint.
5. Persist checkpoint state for incremental sync.
6. Emit import summary and per-item errors.

### Pipeline and Routing

- Default pipeline for this story: import.gdrive
- Type routing rule: source-specific parser emits content and metadata; existing enrichment pipeline handles downstream extraction.

### API Surface

- POST /api/v1/importers/gdrive/run
- GET /api/v1/importers/runs/{run_id}

---

## E2E Test Checklist

- [ ] Import run succeeds with representative source fixture.
- [ ] Dry-run reports expected item counts and no job enqueue.
- [ ] Incremental sync only imports changed/new records.
- [ ] Re-running same source data does not create duplicates.
- [ ] Auth errors are returned with actionable guidance.
- [ ] Network/transient errors trigger retry behavior and proper reporting.
- [ ] Import summary matches observed enqueued job count.

---

## Related Stories

- [US-0008](./US-0008-batch-import-from-file.md) — Batch import foundation
- [US-0106](../pipelines/US-0106-enqueue-content-via-dpkms.md) — Unified enqueue API
- [US-0300](./US-0300-importer-extension-interface.md) — Importer extension contract
- [Importer Plan](../../plans/2026-02-18-importer-gdrive-plan.md) — Source-specific plan
