# US-0113: ctxt Analyze API Client

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/README.md), [Knowledge Workers](../../personas/README.md)

---

## User Goal

As a knowledge worker or agent using `ctxt`, I want to enqueue content for ingestion through the dPKMS API.

Previously, `ctxt analyze` had to:
- Parse and validate input
- Select pipeline based on content length or explicit `--pipeline` flag
- Create job and enqueue directly to Queue via Queue.Enqueue() Return job ID

This caused issues:
- No pipeline configuration validation
- No sandbox isolation
- No support for future features
- Inconsistent behavior with pipeline configuration
- Direct Queue access bypassed pipeline logic

Now, `dpkms pipeline enqueue` is:
1. **Unified endpoint** at `/api/v1/pipelines/enqueue`
2. Pipeline selection with `SelectPipeline()` method
3. Job queue creation with validated pipeline
4. Consistent behavior across all clients

`ctxt analyze` is refactored to call this API.

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

### CLI Refactor

**Before:**
```go
reqBody := map[string]string{
    "content": content,
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
    "content": content,
    "type":     reqType,
    "pipeline": "",
    "source":   "cli",
}

resp, _ := http.Post(serverURL+"/api/v1/pipelines/enqueue", "application/json", ...)
```
```

### Service Layer Changes

**Remove Analyze() method:**
The old `Analyze()` method is removed or refactored to call `Enqueue()` entirely

**New Enqueue() method** replaces it:
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

### Configuration Updates

**New config option:**
Add to config schema:
```yaml
server:
  pipelines:
    auto_update_enabled: false  # Default disabled
```

**Server URL config:**
```yaml
server:
  url: "http://localhost:8080"  # Default
```

## E2E Test Checklist

- [ ] `ctxt analyze "..."` → request sent to `POST /api/v1/pipelines/enqueue` (not old
  `/api/v1/analyze`); request body contains `"content"` matching input
- [ ] `ctxt analyze "..."` → request body contains `"source": "cli"`
- [ ] `ctxt analyze "..." --type url` → request body contains `"type": "url"`
- [ ] `ctxt analyze "..." --pipeline legal-doc-pipeline` → request body contains
  `"pipeline": "legal-doc-pipeline"`
- [ ] `ctxt analyze "..."` (no `--pipeline`) → request body has `"pipeline": ""` or omitted;
  server auto-selects pipeline
- [ ] Enqueue with auto-selected pipeline → job record in DB has correct
  `pipeline` (auto-selected) and `source="cli"`
- [ ] Enqueue with `--pipeline legal-doc-pipeline` → job record in DB has
  `pipeline="legal-doc-pipeline"` and `source="cli"`
- [ ] Enqueue URL content → type detected correctly as "url"; job `type="ingest:url"` in DB
- [ ] Enqueue file content → type detected as "markdown"; job `type="ingest:markdown"` in DB
- [ ] Pipeline not found → 404 error returned; no job record created in DB
- [ ] Archived pipeline selected → 409 error returned; no job record created in DB
- [ ] Job ID is returned for tracking
- [ ] `ctxt analyze` refactored correctly to call enqueue endpoint (verify no call to old
  `/api/v1/analyze` or direct Queue.Enqueue)
- [ ] CLI maintains same interface (no breaking changes)

## Related Stories

- [US-0101](./US-0101-create-custom-pipeline.md) - Create custom pipelines for enqueue
- [US-0102](./US-0102-list-and-filter-pipelines.md) - List pipelines to find pipeline to enqueue
- [US-0103](./US-0103-show-pipeline-details.md) - View pipeline configuration (includes sandbox)
- [US-0106](./US-0106-enqueue-content-via-dpkms.md) - Enqueue (only method, enforces sandbox)
- [US-0107](./US-0107-discover-local-steps.md) - Discover steps to reference in pipeline
- [US-0108](./US-0108-install-step-from-registry.md) - Install from registry
- [US-0109](./US-0109-fetch-registry-manifest.md) - Get available steps from registry
- [US-0110](./US-0110-check-registry-updates.md) - Check for updates
- [US-0111](./US-0111-configure-registry-autoupdate.md) - Configure auto-update
- [US-0112](./US-0112-configure-sandbox-per-pipeline.md) - Configure sandbox

## Related ADRs

- [ADR-056](../../decisions/ADR-056-unified-enqueue-api.md) - Unified enqueue API (this ADR defines)
- [ADR-004](../../decisions/ADR-004-step-based-pipeline.md) - Step-based pipeline architecture (existing)