---
status: shipped
---

# US-0105: Archive Pipeline

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/README.md)

---

## User Goal

As a platform integrator or maintainer, I want to archive pipelines that are temporarily not needed (e.g., experimental pipelines, deprecated workflows, seasonal campaigns).

Archiving:
- Sets `archived = true`
- Prevents pipeline from being used in new jobs
- Preserves pipeline configuration for potential restoration
- No data loss

## Context

Pipelines have archive status:
- Active: `archived = false` — available for enqueue
- Archived: `archived = true` — disabled but retained

Archived pipelines cannot be:
- Be selected by default pipeline selection
- Be manually specified with `--pipeline` flag
- Appear in filtered lists only with `--with-archive` flag

## Acceptance Criteria

- **Custom pipelines can be archived**
- **Built-in pipelines cannot be archived** (protected from both deletion and archiving)
- **Archive is reversible** — can be unarchived to restore
- **Archive prevents pipeline use** - jobs fail gracefully with "pipeline not found" error
- **Archive sets archived flag but keeps record**

- **Archived pipelines cannot be selected**
- `SelectPipeline()` skips archived pipelines by default
- **Jobs requesting archived pipeline return error**

- **Clear error messaging** explain why pipeline is unavailable

## Implementation Notes

### API Endpoint

**POST** `/api/v1/pipelines/{name}/archive`

**Request Body:**
```json
{}
```

**Response Examples:**

**Success (200):**
```json
{
  "message": "Pipeline 'legal-doc-pipeline' archived successfully"
}
```

**Conflict (409):**
```json
{
  "error": {
    "code": "CONFLICT",
    "message": "Pipeline 'legal-doc-pipeline' is currently in use"
  }
}
```

**Not Found (404):**
```json
{
  "error": {
    "code": "NOT_FOUND",
    "message": "Pipeline 'legal-doc-pipeline' not found"
  }
}
```

### CLI Command

```bash
# Archive pipeline
dpkms pipeline archive legal-doc-pipeline

# Unarchive pipeline
dpkms pipeline unarchive legal-doc-pipeline
```

### Storage Operations

```sql
-- Archive pipeline
UPDATE pipelines
SET archived = 1
WHERE name = ? AND is_builtin = 0

-- Unarchive pipeline
UPDATE pipelines
SET archived = 0
WHERE name = ? AND is_builtin = 0
```

### Validation

- Archive is reversible operation
- No data loss occurs
- Pipeline configuration and step metadata preserved
- Only `archived` flag changes

### Job Queue Impact

When a pipeline is archived:
- New jobs requesting this pipeline via `SelectPipeline()` get error: "pipeline not found"
- Existing jobs with this pipeline continue with their steps (if any)
- Built-in fallback mechanism for unknown pipelines

### E2E Test Checklist

- [ ] `dpkms pipeline archive <name>` → request sent to
  `POST /api/v1/pipelines/{name}/archive` with empty JSON body `{}`
- [ ] `dpkms pipeline unarchive <name>` → request sent to
  `POST /api/v1/pipelines/{name}/unarchive` (or equivalent endpoint)
- [ ] Archive custom pipeline returns 200 with success message
- [ ] Verify `archived=1` in DB after archive: `SELECT archived FROM pipelines WHERE name=?`
- [ ] Archive built-in pipeline returns 403 protected error
- [ ] Verify built-in pipeline `archived` field unchanged in DB after rejected attempt
- [ ] Archive non-existent pipeline returns 404 not found error
- [ ] Archive pipeline already archived returns 200 with message
- [ ] Unarchive command makes pipeline available again
- [ ] Verify `archived=0` in DB after unarchive: `SELECT archived FROM pipelines WHERE name=?`
- [ ] Archive command sets archived flag correctly
- [ ] Verify archived pipeline cannot be used: enqueue request with archived pipeline name
  → 409 error; `SelectPipeline()` returns error

## Related Stories

- [US-0101](./US-0101-create-custom-pipeline.md) - Create pipelines to archive
- [US-0102](./US-0102-list-and-filter-pipelines.md) - List pipelines to find pipeline to archive
- [US-0103](./US-0103-show-pipeline-details.md) - View pipeline before archive
- [US-0106](./US-0106-enqueue-content-via-dpkms.md) - Use pipeline (now fails gracefully due to archive)
- [US-0110](./US-0110-check-registry-updates.md) - Check for updates

## Related ADRs

- [ADR-004](../../decisions/ADR-004-step-based-pipeline.md) - Step-based pipeline architecture (existing)

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Integrators](../../personas/platform-integrators.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

- `test/integration/us0105_archive_pipeline_test.go::TestUS0105_ArchiveSetsFlag`
- `test/integration/us0105_archive_pipeline_test.go::TestUS0105_UnarchiveMakesAvailable`
- `test/integration/us0105_archive_pipeline_test.go::TestUS0105_ArchiveIdempotent`
- `test/integration/us0105_archive_pipeline_test.go::TestUS0105_ArchiveExcludesFromDefaultList`
- `test/integration/us0105_archive_pipeline_test.go::TestUS0105_UnarchiveIdempotent`
