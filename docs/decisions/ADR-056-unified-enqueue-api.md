# ADR-056 – Unified Enqueue API

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

Previously, `ctxt analyze` and other clients had to directly enqueue jobs to the job queue via the storage layer's `Queue.Enqueue()` method. This caused several problems:

**Problem 1: Inconsistent pipeline logic**
- Each client reimplemented pipeline selection logic
- No validation that pipeline exists or is not archived
- Sandbox configuration not enforced at enqueue time

**Problem 2: No single source of truth for enqueue**
- Multiple code paths to enqueue jobs (CLI, API, tests)
- Difficult to add features that affect all enqueues (e.g., sandbox enforcement, pipeline validation)
- Inconsistent error handling across clients

**Problem 3: Direct queue access bypassed pipeline system**
- Jobs could reference non-existent pipelines
- Jobs could use archived pipelines
- No way to enforce pipeline-specific settings (resource limits, sandbox)

**Problem 4: Future features difficult to add**
- Want to enforce sandbox constraints at enqueue time
- Want to validate pipeline configuration before job creation
- Want to track enqueue metrics across all clients
- Direct queue access makes these cross-cutting concerns impossible to implement consistently

**The question:** Should we provide a unified enqueue API that encapsulates pipeline selection, validation, and job creation, or continue allowing direct queue access?

---

## Decision

**All job enqueueing must go through a unified API endpoint `/api/v1/pipelines/enqueue`. Direct queue access from clients is deprecated and will be removed.**

### Unified Enqueue Endpoint

**POST** `/api/v1/pipelines/enqueue`

**Request:**
```json
{
  "content": "some text content",
  "type": "text|url|image|audio|video|feed|auto",
  "pipeline": "custom-pipeline",
  "source": "cli"
}
```

**Response:**
```json
{
  "job_id": "job-abc123",
  "message": "Content enqueued for processing"
}
```

**Error Responses:**

400 Bad Request:
```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "Content is required"
  }
}
```

404 Not Found:
```json
{
  "error": {
    "code": "NOT_FOUND",
    "message": "Pipeline 'custom-pipeline' not found"
  }
}
```

409 Conflict:
```json
{
  "error": {
    "code": "CONFLICT",
    "message": "Pipeline 'custom-pipeline' is archived and cannot be used"
  }
}
```

### Service Layer Implementation

**Before (direct queue access):**
```go
// Client code in ctxt CLI
func analyze(serverURL, content string) (string, error) {
    pipeline := selectPipeline(content)  // Reimplemented in client

    job := &storage.Job{
        ID:       uuid.New().String(),
        Type:     "ingest:text",
        Payload:  content,
        Pipeline: pipeline,
        // ... other fields
    }

    if err := queue.Enqueue(ctx, job); err != nil {
        return "", err
    }
    return job.ID, nil
}
```

**After (unified enqueue API):**
```go
// dPKMS service layer
func (s *Service) Enqueue(ctx context.Context, req EnqueueRequest) (string, error) {
    now := time.Now().Truncate(time.Second)

    // 1. Validate content
    if req.Content == "" {
        return "", fmt.Errorf("content is required")
    }

    // 2. Validate or auto-select pipeline
    pipelineName := req.Pipeline
    if pipelineName == "" {
        pipelineName = s.Pipes.SelectPipeline(req.Content)
    }

    // 3. Validate pipeline exists and is not archived
    pipeline, err := s.Pipes.GetByName(ctx, pipelineName)
    if err != nil {
        return "", fmt.Errorf("pipeline '%s' not found", pipelineName)
    }
    if pipeline.Archived {
        return "", fmt.Errorf("pipeline '%s' is archived", pipelineName)
    }

    // 4. Create job
    job := &storage.Job{
        ID:         uuid.New().String(),
        Type:       "ingest:" + req.Type,
        Status:     storage.JobPending,
        Pipeline:   pipelineName,
        Payload:    req.Content,
        Source:     req.Source,
        MaxRetries: 3,
        CreatedAt:  now,
        UpdatedAt:   now,
    }

    // 5. Enqueue to job queue
    if err := s.Queue.Enqueue(ctx, job); err != nil {
        return "", err
    }

    // 6. Return job ID
    return job.ID, nil
}
```

**Client code (ctxt CLI):**
```go
func analyze(serverURL, content string) (string, error) {
    reqBody := map[string]string{
        "content":  content,
        "type":     "auto",
        "source":   "cli",
    }

    resp, err := http.Post(
        serverURL+"/api/v1/pipelines/enqueue",
        "application/json",
        strings.NewReader(jsonString(reqBody)),
    )
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var result struct {
        JobID   string `json:"job_id"`
        Message string `json:"message"`
    }
    json.NewDecoder(resp.Body).Decode(&result)

    return result.JobID, nil
}
```

### Pipeline Selection Logic

The `SelectPipeline()` method (from ADR-004) is encapsulated in the service layer:

```go
func (ps *PipelineService) SelectPipeline(content string) string {
    if len(content) < 500 {
        return "text.short"
    }
    return "text.long"
}
```

Clients no longer implement this logic themselves.

---

## Rationale

### Alternatives Considered

#### 1. Continue Direct Queue Access (Rejected)

Reject because:
- Each client reimplements pipeline validation and selection
- No way to enforce sandbox or other cross-cutting features
- Inconsistent error handling across clients
- Difficult to add features that affect all enqueues
- Violates single responsibility principle (clients shouldn't know about queue internals)

#### 2. Multiple Enqueue Endpoints (Rejected)

Reject because:
- Multiple endpoints (e.g., `/api/v1/analyze`, `/api/v1/ingest`) create confusion
- Feature divergence (one endpoint gets new features, others don't)
- Inconsistent validation and error handling
- Doesn't solve the problem of inconsistent behavior

#### 3. Only HTTP API, No Service Layer (Rejected)

Reject because:
- API handlers would duplicate business logic (pipeline validation, selection)
- Difficult to test (HTTP layer required for testing enqueue)
- Service layer provides better separation of concerns
- CLI would need to make HTTP requests even when running locally

#### 4. Queue Library with Validation Middleware (Rejected)

Reject because:
- Middleware still requires direct queue access
- Validation logic scattered across multiple places
- Doesn't provide a clear contract for enqueue behavior
- Less explicit than a unified API endpoint

### Benefits of Chosen Approach

- **Single source of truth:** All enqueues go through same code path
- **Consistent validation:** Pipeline existence, archive status, sandbox config enforced uniformly
- **Future-proof:** Easy to add features (sandbox enforcement, metrics, rate limiting) in one place
- **Clear contract:** API endpoint defines enqueue behavior explicitly
- **Separation of concerns:** Clients don't need to know about queue or pipeline selection internals
- **Testability:** Service layer can be tested independently of HTTP layer

---

## Consequences

### Positive

- All clients benefit from consistent enqueue behavior
- Pipeline validation and selection is centralized
- Easy to add cross-cutting features (sandbox enforcement, metrics)
- Clear separation between clients and dPKMS internals
- Improved error messages (consistent validation errors)
- Future sandbox enforcement possible (validate at enqueue time)

### Negative

- Breaking change for existing direct queue access users (migration path required)
- Slight overhead of HTTP request when dPKMS and client run locally (negligible)
- Additional API endpoint to maintain

### Neutral

- Existing `/api/v1/analyze` endpoint deprecated (kept for backward compatibility during migration period)
- Queue.Enqueue() method still exists (internal use only, not documented for external clients)

---

## Implementation Notes

### Migration Path

1. **Deprecate direct queue access** in documentation and examples
2. **Add warning logs** when clients use old `/api/v1/analyze` endpoint
3. **Keep old endpoint** for 2 release cycles (or until all known clients migrated)
4. **Remove old endpoint** after migration period

### Error Handling

- **Pipeline not found:** 404 with clear message
- **Pipeline archived:** 409 with clear message (not 404)
- **Invalid request:** 400 with validation details
- **Queue error:** 500 with queue-specific error (internal)

### Metrics

Track enqueue metrics:
- Total enqueues per pipeline
- Enqueue success rate
- Enqueue latency (time to queue job)
- Pipeline selection distribution (auto vs explicit)

---

## References

- **ADR-004** – Step-Based Pipeline Architecture (defines PipelineStep and pipeline selection)
- **US-0106** – Enqueue Content via dPKMS (user story for unified enqueue)
- **US-0113** – ctxt Analyze API Client (ctxt CLI refactoring)
- **US-0112** – Configure Sandbox per Pipeline (future sandbox enforcement at enqueue time)

---
