# US-0320: Generic Adapter Ingestion Interface

**System Types:** ctxt
**Personas:** [Platform Integrators](../../personas/platform-integrators.md)

---

## User Goal

As a developer, I create ingestion adapters using a standard
interface so external sources feed into ctxt without custom
plumbing.

---

## Context

ctxt has many built-in importers (Chrome, Slack, etc.) but each
is wired directly to the HTTP enqueue API. A generic adapter
contract lets any external tool emit structured JSON and have
ctxt handle dedup, storage, and indexing uniformly.

The adapter contract:
- Adapter outputs JSON array to stdout
- Each object: `{id, type, content, tags[], metadata{}}`
- ctxt reads, deduplicates by `source:id`, indexes

---

## Acceptance Criteria

- [ ] `ingest.Adapter` interface: `Name() string`,
      `Fetch(ctx) ([]Object, error)`
- [ ] `ingest.Runner` deduplicates by source_key
      (`adapter_name:object_id`)
- [ ] `ingest.Registry` holds named adapter factories
- [ ] CLI: `ctxt ingest --source <name>` executes adapter
- [ ] CLI: `ctxt ingest --source <name> --stdin` reads JSON
      from stdin
- [ ] CLI: `ctxt ingest --source <name> --watch --interval 5m`
      polls adapter on interval
- [ ] Objects stored with `status: raw`, `source: adapter_name`
- [ ] Tags from adapter output mapped to `storage.Tag`
- [ ] Metadata from adapter output stored on KnowledgeObject

---

## Implementation Notes

- `internal/ingest/adapter.go` — Object type + Adapter interface
- `internal/ingest/runner.go` — Runner with dedup + store
- `internal/ingest/registry.go` — adapter factory registry
- `cmd/ctxt/cmd/ingest.go` — CLI command

---

## Related Stories

- [US-0300](./US-0300-importer-extension-interface.md) —
  full importer lifecycle (heavier)
- [US-0321](./US-0321-cardamum-contacts-ingestion.md) —
  first adapter on this interface

---

## E2E Tests

- planned: `test/integration/us0320_adapter_ingestion_test.go::TestAdapterIngest_RegistersAdapter`
- planned: `test/integration/us0320_adapter_ingestion_test.go::TestAdapterIngest_RoutesObjectByMime`
- planned: `test/integration/us0320_adapter_ingestion_test.go::TestAdapterIngest_ProvenanceRecorded`
- planned: `test/integration/us0320_adapter_ingestion_test.go::TestAdapterIngest_AdapterFailureBackoff`

## Personas

- [Solo Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/solo-developer.md)
