---
status: shipped
---

# US-0015: Batch Enrichment With Progress

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Operations](../../personas/operations.md), [Maintainers](../../personas/maintainers.md)

---

## User Goal

As an operator or maintainer, I want to trigger enrichment on many objects at once and track progress so I can monitor bulk operations without polling individual jobs.

---

## Context

When onboarding existing content or re-enriching after a pipeline change, operators need to enrich hundreds or thousands of objects efficiently. A batch endpoint accepts a set of object IDs (or a query filter) with a list of enrichment steps, spawns individual jobs, and exposes aggregate progress.

---

## Acceptance Criteria

- [ ] Operator can trigger batch enrichment with a list of object IDs and a list of steps
- [ ] Batch accepts a filter query as an alternative to explicit object ID list
- [ ] A batch job ID is returned immediately; processing is asynchronous
- [ ] Progress endpoint reports total, completed, failed, and pending counts
- [ ] Each per-object job is independently retryable on failure
- [ ] Operator can cancel an in-progress batch
- [ ] Request payload includes the steps list and AI provider

---

## Implementation Notes

### CLI Interface

```bash
# Batch enrich by explicit IDs
ctxt enrich --batch --objects o-abc,o-def,o-ghi \
  --steps extract-entities-and-mentions,assign-tags \
  --ai-provider lmql

# Batch enrich by filter
ctxt enrich --batch --filter "status:pending_enrichment" \
  --steps extract-entities-and-mentions \
  --ai-provider lmql

# Returns immediately with batch job ID
{
  "batch_job_id": "bj-xyz123",
  "total_objects": 3,
  "status": "in_progress"
}

# Check batch progress
ctxt jobs batch bj-xyz123
{
  "batch_job_id": "bj-xyz123",
  "total": 3,
  "completed": 1,
  "failed": 0,
  "pending": 2,
  "status": "in_progress"
}
```

### REST API Endpoint

```
POST /enrich/batch
Content-Type: application/json

{
  "object_ids": ["o-abc", "o-def", "o-ghi"],
  "steps": ["extract-entities-and-mentions", "assign-tags"],
  "ai_provider": "lmql"
}

→ 202 Accepted
{
  "batch_job_id": "bj-xyz123",
  "total_objects": 3,
  "status": "in_progress"
}

GET /enrich/batch/{batch_job_id}/progress

→ 200 OK
{
  "batch_job_id": "bj-xyz123",
  "total": 3,
  "completed": 2,
  "failed": 0,
  "pending": 1,
  "status": "in_progress"
}
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt enrich --batch --objects <ids> --steps <steps> --ai-provider lmql` exits 0 and returns a `batch_job_id`
- [ ] CLI: `--steps <steps>` flag is present in the request payload sent to server as `steps` array (verified via request capture or server log)
- [ ] CLI: `--ai-provider lmql` flag is present in the request payload as `ai_provider` field
- [ ] CLI: `--objects <ids>` flag is present in the request payload as `object_ids` array
- [ ] CLI: `--filter <query>` flag is present in the request payload as `filter` field when provided (alternative to `--objects`)
- [ ] Server: POST `/enrich/batch` receives `object_ids` (or `filter`), `steps`, and `ai_provider` in request body
- [ ] Server: Response contains `batch_job_id`, `total_objects`, and `status: "in_progress"`
- [ ] Progress: GET `/enrich/batch/{batch_job_id}/progress` returns `total`, `completed`, `failed`, `pending` counts
- [ ] Progress: `completed + failed + pending` always equals `total`
- [ ] Progress: Status transitions to `"completed"` when all per-object jobs are done
- [ ] Storage: After batch completes, each object in the batch has enrichment fields populated (verified by GET `/objects/{object_id}` for each)
- [ ] Per-object retry: A failed individual job can be retried without re-running the entire batch
- [ ] Cancel: DELETE `/enrich/batch/{batch_job_id}` cancels pending (not yet started) jobs
- [ ] Error: Batch with invalid object IDs returns 422 with the list of invalid IDs
- [ ] Resilience: Worker crash during batch does not lose progress; completed jobs remain completed

---

## Related Stories

- [US-0009](./US-0009-extract-entities-and-mentions.md) — Entity extraction step used in batch
- [US-0011](./US-0011-assign-tags-from-vocabulary.md) — Tag assignment step used in batch
- [US-0014](./US-0014-constrain-extraction-with-lmql.md) — LMQL provider used by batch steps

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- Automation Builder
- Platform Engineer

---

## E2E Tests

- `test/integration/us0015_batch_enrichment_test.go::TestUS0015_NObjectsEnqueuedAndAllComplete`
- `test/integration/us0015_batch_enrichment_test.go::TestUS0015_AllEnrichedObjectsHaveEnrichmentMetadata`
- `test/integration/us0015_batch_enrichment_test.go::TestUS0015_DistinctObjectIDsForBatchItems`
- `test/integration/us0015_batch_enrichment_test.go::TestUS0015_JobsProcessedIndependently`
- `test/integration/us0015_batch_enrichment_test.go::TestUS0015_BatchProgressCountsConsistent`
