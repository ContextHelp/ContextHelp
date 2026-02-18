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

- [ ] Register a mock importer and verify it appears in list/discovery output.
- [ ] Run mock importer via CLI and API with same outcome.
- [ ] Validate checkpoint resume behavior after forced interruption.
- [ ] Validate standardized error types are surfaced.
- [ ] Verify run summaries and metrics are emitted for success and failure cases.

---

## Related Stories

- [US-0106](../pipelines/US-0106-enqueue-content-via-dpkms.md) — Unified enqueue API
- [US-0107](../pipelines/US-0107-discover-local-steps.md) — Discovery model reference
- [US-0108](../pipelines/US-0108-install-step-from-registry.md) — Registry installation patterns
- [US-0301](./US-0301-import-chrome-bookmarks.md) — First concrete importer on this interface
