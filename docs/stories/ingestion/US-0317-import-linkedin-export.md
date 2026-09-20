---
status: shipped
---

# US-0317: Import LinkedIn Export

**System Types:** ctxt, dpkms (self-hosted), dpkms cloud
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Operations](../../personas/operations.md), [Maintainers](../../personas/maintainers.md)

---

## User Goal

As a knowledge worker, I want to import data from LinkedIn account data export packages so I can make existing knowledge searchable in ctxt without manual copy/paste.

---

## Context

Teams adopting ctxt already have high-value knowledge in external systems. This story defines a source-specific importer that keeps import behavior consistent with existing ingestion flows: parse source records, normalize them into draft knowledge objects, enqueue jobs, and preserve provenance metadata.

The importer must support both initial backfill and repeat sync runs. It should tolerate partial failures, report actionable errors, and avoid duplicate objects when the same source item is imported multiple times.

---

## Acceptance Criteria

- [ ] Importer accepts LinkedIn account data export packages as input and validates required configuration.
- [ ] Auth flow supports export upload for MVP, partner OAuth scopes where available.
- [ ] Importer supports first-time backfill and incremental sync using export timestamps and activity IDs.
- [ ] Imported records map to object types: posts, articles, profile metadata, connections metadata.
- [ ] Importer stores provenance metadata including source, external ID, and import timestamp.
- [ ] Importer deduplicates using source + external ID + canonical URL hash.
- [ ] Partial failures do not abort the full run; failures are reported per item.
- [ ] Import run reports counts: scanned, imported, skipped, failed.
- [ ] Dry-run mode validates parsing and mapping without enqueueing jobs.

---

## Implementation Notes

### CLI Interface

- Example: ctxt import linkedin --file ./linkedin-export.zip --profile default
- Optional flags: --dry-run, --max-items, --since, --profile, --server

### Ingestion Flow

1. Validate config and source access.
2. Read source records (API pages, export files, or local vault files).
3. Normalize into draft objects with source metadata.
4. Enqueue each item through unified enqueue endpoint.
5. Persist checkpoint state for incremental sync.
6. Emit import summary and per-item errors.

### Pipeline and Routing

- Default pipeline for this story: import.linkedin
- Type routing rule: source-specific parser emits content and metadata; existing enrichment pipeline handles downstream extraction.

### API Surface

- POST /api/v1/importers/linkedin/run
- GET /api/v1/importers/runs/{run_id}

---

## E2E Test Checklist

- [ ] CLI: `ctxt import linkedin --file ./linkedin-export.zip --profile default` sends `"importer_key": "linkedin"`, `"file"` path, and `"profile": "default"` in the request payload; server creates a run record with all three fields — GET /api/v1/importers/runs/{run_id} confirms.
- [ ] CLI: `--server` flag value is included in the request payload when provided; server records the target server endpoint in the run record.
- [ ] Import run succeeds with representative LinkedIn export fixture; produced KnowledgeObjects contain post/article content, profile metadata, and activity IDs.
- [ ] Stored KnowledgeObjects have provenance metadata: `source=linkedin`, `external_id` matching the LinkedIn activity ID, and `import_timestamp` set to ingest time.
- [ ] Dry-run: `--dry-run` flag sends `"dry_run": true` in the server request payload; no jobs enqueued — GET /api/v1/importers/runs/{run_id} confirms `enqueued=0` and reports expected item counts.
- [ ] Incremental sync: uses export timestamps and activity IDs; second run with same export and unchanged activities imports zero new records — `skipped` count equals previous `imported` count.
- [ ] Re-running same source data does not create duplicates; dedup table row count unchanged.
- [ ] Auth errors (invalid archive file or expired OAuth credentials) are returned with actionable guidance and `error_type=auth`.
- [ ] Rate-limit errors (429) in API mode trigger retry behavior; run report shows `error_type=rate_limit` for affected items.
- [ ] Network/transient errors trigger retry behavior; run report shows `error_type=transient` for retried items.
- [ ] Import summary `scanned + skipped + failed = total`; `imported` count matches observed enqueued job count — GET /api/v1/importers/runs/{run_id} confirms all counters.

---

## Related Stories

- [US-0008](./US-0008-batch-import-from-file.md) — Batch import foundation
- [US-0106](../pipelines/US-0106-enqueue-content-via-dpkms.md) — Unified enqueue API
- [US-0300](./US-0300-importer-extension-interface.md) — Importer extension contract
- [Importer Plan](../../plans/2026-02-18-importer-linkedin-plan.md) — Source-specific plan

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- Solo Developer

---

## E2E Tests

- `test/integration/us0317_linkedin_export_test.go::TestUS0317_LinkedInParsePosts`
- `test/integration/us0317_linkedin_export_test.go::TestUS0317_LinkedInPostFields`
- `test/integration/us0317_linkedin_export_test.go::TestUS0317_LinkedInPostContent`
- `test/integration/us0317_linkedin_export_test.go::TestUS0317_LinkedInParseArticles`
- `test/integration/us0317_linkedin_export_test.go::TestUS0317_LinkedInArticleFields`
- `test/integration/us0317_linkedin_export_test.go::TestUS0317_LinkedInFilterPostsSince`
- `test/integration/us0317_linkedin_export_test.go::TestUS0317_LinkedInRenderPost`
- `test/integration/us0317_linkedin_export_test.go::TestUS0317_LinkedInRenderArticle`
