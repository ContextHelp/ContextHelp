---
status: shipped
---

# US-0028: Register Custom Pipeline

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/platform-integrators.md),
[Maintainers](../../personas/maintainers.md)

---

## User Goal

As a platform integrator or maintainer, I want to register a custom pipeline so content
can be routed through a bespoke sequence of steps.

---

## Context

dPKMS ships built-in pipelines (`text.short`, `text.long`, `url.generic`, etc.) but
operators need to compose their own step sequences. Custom pipelines are registered via the
`dpkms pipeline create` CLI (reads a YAML/JSON/TOML file) which POSTs to the server's
`POST /api/v1/pipelines` endpoint. The server persists the record; subsequent enqueue calls
can reference the pipeline by name.

---

## Acceptance Criteria

- [ ] Operator can create a pipeline from a YAML, JSON, or TOML config file
- [ ] Pipeline `name`, `description`, and `steps` are required; server rejects missing fields
- [ ] Server returns a pipeline `id` on successful creation
- [ ] Pipeline is retrievable by name via `GET /api/v1/pipelines/{name}`
- [ ] Pipeline appears in `GET /api/v1/pipelines` listing
- [ ] Pipeline can be archived (disabled) and unarchived (re-enabled)
- [ ] Built-in pipelines cannot be deleted
- [ ] Content can be enqueued against a custom pipeline by name
- [ ] `--type`, `--pipeline`, and `--wait` flags are forwarded in the enqueue payload

---

## Implementation Notes

### CLI Commands

```
dpkms pipeline create pipeline.yaml           # create from YAML
dpkms pipeline create pipeline.json           # create from JSON
dpkms pipeline create pipeline.toml           # create from TOML

dpkms pipeline list                           # list all (custom + built-in)
dpkms pipeline list --name <filter>           # filter by name substring
dpkms pipeline list --include-archived        # include archived
dpkms pipeline list --only-archived           # only archived

dpkms pipeline show <name>                    # show details
dpkms pipeline show <name> --raw              # raw JSON

dpkms pipeline archive <name>                 # disable pipeline
dpkms pipeline unarchive <name>               # re-enable pipeline
dpkms pipeline remove <name>                  # delete (custom only)

dpkms pipeline enqueue --type text --pipeline my-pipeline <content>
dpkms pipeline enqueue --type url --pipeline my-pipeline --wait <content>
```

### Pipeline Config File (YAML)

```yaml
name: my-extract-pipeline
description: Extract entities and decisions from long-form text
steps:
  - name: summarise
  - name: extract-entities
  - name: extract-decisions
  - name: embed
```

### REST API

```
POST /api/v1/pipelines
Content-Type: application/json

{
  "name": "my-extract-pipeline",
  "description": "Extract entities and decisions",
  "steps": "[{\"name\":\"summarise\"},{\"name\":\"extract-entities\"}]"
}

→ 201 Created
{ "id": "550e8400-e29b-41d4-a716-446655440000" }

GET /api/v1/pipelines/my-extract-pipeline
→ 200 OK
{
  "id": "550e8400-...",
  "name": "my-extract-pipeline",
  "description": "Extract entities and decisions",
  "steps": [...],
  "is_built_in": false,
  "archived": false,
  "created_at": "...",
  "updated_at": "..."
}

POST /api/v1/pipelines/enqueue
Content-Type: application/json

{
  "content": "Some text to process",
  "type": "text",
  "pipeline": "my-extract-pipeline"
}

→ 202 Accepted
{ "job_id": "abc123" }
```

---

## E2E Test Checklist

### Create — Payload Fields Sent to Server
- [ ] Create: YAML file → CLI sends `{"name":"...","description":"...","steps":"[...]"}` to
  `POST /api/v1/pipelines` (intercept or use `--raw` flag to verify payload shape)
- [ ] Create: JSON file → same payload fields present in request body
- [ ] Create: TOML file → same payload fields present in request body
- [ ] Create: Missing `name` → server returns `400 INVALID_REQUEST`
- [ ] Create: Missing `steps` → server returns `400 INVALID_REQUEST`
- [ ] Create: Invalid steps JSON → server returns `400 INVALID_REQUEST`

### Server-Side Receipt and Storage
- [ ] Storage: After create, `GET /api/v1/pipelines/{name}` returns object with
  `name`, `description`, `steps[]`, `is_built_in=false`, `archived=false`
- [ ] Storage: `GET /api/v1/pipelines` listing includes newly created pipeline
- [ ] Storage: `id` in create response matches `id` in GET response

### List Flags → Request Payload / Query Params
- [ ] List: `--name <filter>` sends `?name=<filter>` query param; only matching pipelines returned
- [ ] List: `--include-archived` sends `?include_archived=true`; archived pipelines visible
- [ ] List: `--only-archived` sends `?only_archived=true`; only archived pipelines returned
- [ ] List: No flags → archived pipelines absent from default listing

### Archive / Unarchive
- [ ] Archive: `dpkms pipeline archive <name>` sends `POST /api/v1/pipelines/{name}/archive`;
  server responds `{"status":"archived"}`
- [ ] Archive: After archive, `GET /api/v1/pipelines/{name}` shows `"archived":true`
- [ ] Unarchive: `dpkms pipeline unarchive <name>` sends `POST /api/v1/pipelines/{name}/unarchive`;
  server responds `{"status":"unarchived"}`
- [ ] Unarchive: After unarchive, `GET /api/v1/pipelines/{name}` shows `"archived":false`

### Delete
- [ ] Delete: `dpkms pipeline remove <name>` sends `DELETE /api/v1/pipelines/{name}`;
  server returns `204 No Content`
- [ ] Delete: After delete, `GET /api/v1/pipelines/{name}` returns `404 NOT_FOUND`
- [ ] Delete: Attempt to delete built-in pipeline → server returns `403 PROTECTED_PIPELINE`

### Enqueue — Payload Fields Sent to Server
- [ ] Enqueue: `--type text` sends `{"type":"text"}` in `POST /api/v1/pipelines/enqueue` body
- [ ] Enqueue: `--pipeline my-pipeline` sends `{"pipeline":"my-pipeline"}` in request body
- [ ] Enqueue: `--wait` flag causes CLI to poll `GET /api/v1/jobs/{id}` until `status=completed`
- [ ] Enqueue: Resulting job record has `pipeline` field set to the custom pipeline name

### Show / Raw Flag
- [ ] Show: `dpkms pipeline show <name>` prints `name`, `description`, `archived`, `is_built_in`,
  steps list
- [ ] Show: `dpkms pipeline show <name> --raw` outputs valid JSON matching GET response body

---

## Related Stories

- [US-0029](US-0029-install-and-enable-plugin.md) — step install needed before custom pipeline
- [US-0037](../agents/US-0037-agent-discovers-query-schema.md) — agents enqueue via pipelines
- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) — enqueue path shared with ctxt

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- Platform Engineer
- [Operations](../../personas/operations.md)

---

## E2E Tests

- `test/integration/us0028_register_pipeline_test.go::TestUS0028_RegisterPipelineViaAPI`
- `test/integration/us0028_register_pipeline_test.go::TestUS0028_RegisteredPipelineRetrievableByName`
- `test/integration/us0028_register_pipeline_test.go::TestUS0028_RegisteredPipelineAppearsInListing`
- `test/integration/us0028_register_pipeline_test.go::TestUS0028_IngestWithCustomPipelineNameExecutesSteps`
- `test/integration/us0028_register_pipeline_test.go::TestUS0028_MissingNameReturns400`
- `test/integration/us0028_register_pipeline_test.go::TestUS0028_MissingStepsReturns400`
- `test/integration/us0028_register_pipeline_test.go::TestUS0028_EnqueueViaHTTPRecordsJobPipeline`
