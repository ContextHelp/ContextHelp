# US-0300: Importer Extension Interface

**System Types:** dpkms (self-hosted), dpkms cloud, ctxt
**Personas:** [Platform Integrators](../../personas/platform-integrators.md), [Maintainers](../../personas/maintainers.md)

---

## User Goal

As a platform integrator, I want a stable importer interface so I can build and ship custom importers without patching core ingestion logic.

---

## Context

Supporting many sources requires an extension model that keeps source logic isolated while preserving common ingestion guarantees: validation, dedup, checkpointing, retries, metrics, and enqueue behavior. A formal importer contract lets teams add new connectors with predictable behavior and lower maintenance cost.

This story establishes the core importer interface and lifecycle for registration, discovery, execution, and observability. Source-specific stories (Chrome, Drive, Slack, etc.) depend on this contract.

---

## Acceptance Criteria

- [ ] Core importer interface defines required methods for config schema, health check, backfill, and incremental sync.
- [ ] Importer registration mechanism supports built-in and external importers.
- [ ] Import run lifecycle is standardized: validate -> fetch -> normalize -> dedup -> enqueue -> checkpoint -> summarize.
- [ ] Importer config is validated against a schema before execution.
- [ ] Importers emit a common result payload with scanned/imported/skipped/failed counters.
- [ ] Checkpoint persistence is standardized across importers.
- [ ] Error classification is standardized (auth, rate_limit, malformed_input, transient, internal).
- [ ] Importer execution is observable via logs/metrics and run IDs.
- [ ] CLI and API can execute any registered importer by key.
- [ ] Registered importers are discoverable via `GET /api/v1/importers` and listed with correct key and metadata.

---

## Implementation Notes

### Interface Contract

- Importer key and metadata (name, supported modes, auth type)
- ValidateConfig(config) error
- Backfill(ctx, state, sink) -> ImportResult
- Sync(ctx, checkpoint, state, sink) -> ImportResult
- HealthCheck(ctx, config) error

### Shared Components

- Config schema registry for importer-specific options
- Checkpoint store keyed by profile + importer + account
- Dedup helper keyed by source + external ID + canonical URL hash
- Import run store for status, progress, and errors

### API and CLI Surface

- CLI: ctxt import <importer-key> [flags]
- API: POST /api/v1/importers/{key}/run
- API: GET /api/v1/importers/runs/{run_id}
- API: GET /api/v1/importers

---

## E2E Test Checklist

- [ ] Register a mock importer and verify it appears in GET /api/v1/importers list with correct key and metadata.
- [ ] CLI: `ctxt import <importer-key> --profile default --server http://host` sends `"importer_key"`, `"profile"`, and `"server"` in the request payload to the server; stored run record reflects all three fields.
- [ ] API: `POST /api/v1/importers/{key}/run` with `{"profile": "default", "dry_run": false}` creates a run record — GET /api/v1/importers/runs/{run_id} confirms `importer_key`, `profile`, and `status` fields in the stored run.
- [ ] CLI and API produce the same stored run structure (same fields, same schema) when invoked with identical parameters.
- [ ] Dry-run: `--dry-run` flag sends `"dry_run": true` in the server request payload; no jobs are enqueued — GET /api/v1/importers/runs/{run_id} confirms `status=dry_run_complete` and `enqueued=0`.
- [ ] Checkpoint resume: after forced interruption, re-running the importer sends the persisted checkpoint in the request payload; server resumes from the last checkpoint position — run report shows previously imported items as skipped.
- [ ] Standardized error types are surfaced: auth error, rate_limit, malformed_input, transient, internal errors each appear in run report with the correct `error_type` field.
- [ ] Run summaries include `scanned`, `imported`, `skipped`, `failed` counters — GET /api/v1/importers/runs/{run_id} confirms all four fields present and non-negative.
- [ ] Metrics/logs emitted for success case: run ID appears in structured log output with outcome=success.
- [ ] Metrics/logs emitted for failure case: run ID appears in structured log output with outcome=failed and non-empty error detail.

---

## Related Stories

- [US-0106](../pipelines/US-0106-enqueue-content-via-dpkms.md) — Unified enqueue API
- [US-0107](../pipelines/US-0107-discover-local-steps.md) — Discovery model reference
- [US-0108](../pipelines/US-0108-install-step-from-registry.md) — Registry installation patterns
- [US-0301](./US-0301-import-chrome-bookmarks.md) — First concrete importer on this interface
