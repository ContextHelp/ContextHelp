# US-0106: Enqueue Content via dPKMS

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/README.md), [Knowledge Workers](../../personas/README.md)

---

## User Goal

As a user (knowledge worker, agent, or platform integrator), I want to enqueue content for ingestion through dPKMS API.

This is the **only** supported method for enqueueing content:
- `ctxt analyze` must use this API
- Direct Queue access is not available

## Context

Previously, `ctxt analyze` had to:
1. Parse and validate input
2. Select pipeline based on content length or explicit `--pipeline` flag
3. Create job and enqueue directly to Queue via `Queue.Enqueue()`
4. Return job ID

This caused issues:
- Inconsistent behavior with pipeline configuration
- No support for future features (sandbox, resource limits)
- Direct Queue access bypassed pipeline logic

Now, `dpkms pipeline enqueue` is:
1. **Unified endpoint** at `/api/v1/pipelines/enqueue`
2. Pipeline selection with `SelectPipeline()` method
3. Job queue creation with validated pipeline
4. Consistent behavior across all clients

## Acceptance Criteria

- **`dpkms pipeline enqueue` is the sole enqueue method**
- **Content can be enqueued from stdin, file, or argument**
- **Pipeline selection respects `--pipeline` flag or automatic selection**
- **Returns job ID for tracking**
- **`ctxt analyze` must be refactored to call this endpoint**

- **No direct Queue access from CLI or other API**

## Implementation Notes

### API Endpoint

**POST** `/api/v1/pipelines/enqueue`

**Request Body:**
```json
{
  "content": "some text content",
  "type": "text|url|image|audio|video|feed|auto",
  "pipeline": "custom-pipeline", // optional, defaults to auto-selected
  "source": "cli" // or "dpkms" or custom
}
```

**Response:**
```json
{
  "job_id": "job-abc123",
  "message": "Content enqueued for processing"
}
```

**Error Cases:**

**400 Bad Request:**
```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "Content is required"
  }
}
```

**404 Not Found**
```json
{
  "error": {
    "code": "NOT_FOUND",
    "message": "Pipeline 'custom-pipeline' not found"
  }
}
```

**409 Conflict**
```json
{
  "error": {
    "code": "CONFLICT",
    "message": "Pipeline 'custom-pipeline' is archived and cannot be used"
  }
}
```

### CLI Command

```bash
# Enqueue text content (using auto-selected pipeline)
dpkms pipeline enqueue "Analyze this text"

# Enqueue with specific pipeline
dpkms pipeline enqueue "Analyze this text" --pipeline legal-doc-pipeline

# Enqueue URL content
dpkms pipeline enqueue "https://example.com/article" --type url

# Enqueue from file
dpkms pipeline enqueue README.md --type markdown
```

### Pipeline Selection Logic

The `SelectPipeline()` method in service layer:
1. If `--pipeline` flag is provided → use that pipeline
2. Else if content length < 500 chars → use `text.short`
3. Else → use `text.long`

### Service Layer Updates

**Remove Analyze() method**
The old `Analyze()` method is removed or refactored to call `Enqueue()`

**New Enqueue() method** replaces it entirely:
```go
func (s *Service) Enqueue(ctx context.Context, req AnalyzeRequest) (string, error) {
    now := time.Now().Truncate(time.Second)
    pipelineName := req.Pipeline
    if pipelineName == "" {
        pipelineName = s.Pipes.SelectPipeline(req.Content)
    }

    job := &storage.Job{
        ID:         uuid.New().String(),
        Type:       "ingest:" + req.Type,
        Status:     storage.JobPending,
        Payload:    req.Content,
        Pipeline:   pipelineName,
        Source:     req.Source,
        MaxRetries: 3,
        CreatedAt:  now,
        UpdatedAt: now,
    }

    if err := s.Queue.Enqueue(ctx, job); err != nil {
        return "", err
    }
    return job.ID, nil
}
```

### CLI Refactor

**Before:**
```go
reqBody := map[string]string{
    "content":  content,
    "type":     viper.GetString("analyze.type"),
    "pipeline": viper.GetString("analyze.pipeline"),
    "source":   "cli",
}

// POST to dpkms
resp, _ := http.Post(serverURL+"/api/v1/analyze", ...)
```

**After:**
```go
serverURL := viper.GetString("server.url")
if serverURL == "" {
    serverURL = "http://localhost:8080"
}

reqBody := map[string]string{
    "content":  content,
    "type":     reqType,
    "pipeline": "",
    "source":   "cli",
}

resp, _ := http.Post(serverURL+"/api/v1/pipelines/enqueue", "application/json", ...)
```
```

## E2E Test Checklist

- [ ] `dpkms pipeline enqueue "..."` → request body contains `content` field matching input
- [ ] `dpkms pipeline enqueue "..." --pipeline legal-doc-pipeline` → request body contains
  `"pipeline": "legal-doc-pipeline"`
- [ ] `dpkms pipeline enqueue "..." --type url` → request body contains `"type": "url"`
- [ ] `dpkms pipeline enqueue "..." --type markdown` → request body contains
  `"type": "markdown"`
- [ ] Request body always contains `"source": "cli"` when invoked from CLI
- [ ] Enqueue with auto-selected pipeline (no `--pipeline`) → request body has `"pipeline": ""`
  or omits field; server selects correct pipeline; job record in DB has correct pipeline name
- [ ] Enqueue with `--pipeline legal-doc-pipeline` → job record in DB has
  `pipeline="legal-doc-pipeline"`
- [ ] Verify job record stored in DB: `SELECT * FROM jobs WHERE id=?` returns row with correct
  `type`, `status="pending"`, `payload`, `pipeline`, `source` fields
- [ ] Enqueue URL content → type detected correctly as "url"; job `type="ingest:url"` in DB
- [ ] Enqueue file content → type detected as "markdown"; job `type="ingest:markdown"` in DB
- [ ] Enqueue image content → type detected as "image"; job `type="ingest:image"` in DB
- [ ] Pipeline not found → 404 error returned; no job record created in DB
- [ ] Archived pipeline selected → 409 error returned; no job record created in DB
- [ ] Job ID is returned for tracking
- [ ] `ctxt analyze` refactored correctly to call enqueue endpoint
- [ ] CLI maintains same interface (no breaking changes)

## Related Stories

- [US-0101](./US-0101-create-custom-pipeline.md) - Create pipelines to enqueue
- [US-0102](./US-0102-list-and-filter-pipelines.md) - List pipelines to find pipeline to enqueue
- [US-0103](./US-0103-show-pipeline-details.md) - View pipeline configuration
- [US-0105](./US-0105-archive-pipeline.md) - Archive pipelines (now fails gracefully due to archive)
- [US-0107](./US-0107-discover-local-steps.md) - Discover steps to reference in pipeline
- [US-0108](./US-0108-install-step-from-registry.md) - Install from registry
- [US-0109](./US-0109-fetch-registry-manifest.md) - Fetch manifest to get available steps
- [US-0110](./US-0110-check-registry-updates.md) - Check for updates
- [US-0111](./US-0111-configure-registry-autoupdate.md) - Configure auto-update
- [US-0112](./US-0112-configure-sandbox-per-pipeline.md) - Configure sandbox
- [US-0113](./US-0113-ctxt-analyze-api-client.md) - ctxt analyze as enqueue API client

## Related ADRs

- [ADR-056](../../decisions/ADR-056-unified-enqueue-api.md) - Unified enqueue API (this ADR defines)
- [ADR-004](../../decisions/ADR-004-step-based-pipeline.md) - Step-based pipeline architecture

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Integrators](../../personas/platform-integrators.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

- `test/integration/us0106_enqueue_test.go::TestUS0106_EnqueueBasic`
- `test/integration/us0106_enqueue_test.go::TestUS0106_JobHasCorrectPipeline`
- `test/integration/us0106_enqueue_test.go::TestUS0106_AutoSelectsPipeline`
- `test/integration/us0106_enqueue_test.go::TestUS0106_MissingContentReturns400`
- `test/integration/us0106_enqueue_test.go::TestUS0106_TypeDefaultsToText`
- `test/integration/us0106_enqueue_test.go::TestUS0106_JobTypeFormat`
- `test/integration/us0106_enqueue_test.go::TestUS0106_MultipleJobsUniqueIDs`
- `test/integration/us0106_enqueue_test.go::TestUS0106_CreateThenEnqueue`
- `test/integration/us0106_enqueue_test.go::TestUS0106_EnqueueInvalidPipelineRejected`
- `test/integration/us0106_enqueue_test.go::TestUS0106_EnqueueEmptySourceAccepted`
