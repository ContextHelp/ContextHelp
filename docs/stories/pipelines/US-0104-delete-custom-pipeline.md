# US-0104: Delete Custom Pipeline

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/README.md)

---

## User Goal

As a platform integrator or maintainer, I want to delete a custom pipeline that is no longer needed or was created incorrectly.

## Context

Custom pipelines are stored in database with metadata including:
- Built-in status flag
- Archive status flag
- Creation and update timestamps

Users need the ability to:
- Remove accidentally created pipelines
- Clean up after testing
- Archive pipelines temporarily instead of permanent deletion
- Maintain proper pipeline hygiene

## Acceptance Criteria

- **Custom pipelines can be deleted** but built-in pipelines are protected
- **Archive status is separate from deletion**
- **Built-in pipelines cannot be deleted** (protected names starting with `text.*`)
- **Delete is permanent** - no undo, requires recreation
- **Archive is reversible** - can be unarchived to restore
- **Confirmation prompt** for delete operations requiring explicit confirmation
- **Clear error messages** explain what protection prevents
- **Pipeline becomes unavailable** after deletion

## Implementation Notes

### API Endpoint

**DELETE** `/api/v1/pipelines/{name}`

**Request Headers:**
```
Content-Type: application/json
X-Confirmation: true/false (default: false for safety)
```

**Response Examples:**

**Success (200):**
```json
{
  "message": "Pipeline 'legal-doc-pipeline' deleted successfully"
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

**Protected pipeline (403):**
```json
{
  "error": {
    "code": "PROTECTED_PIPELINE",
    "message": "Pipeline names starting with 'text.' are protected from deletion"
  }
}
```

**Not Found (404):**
```json
{
  "error": {
    "code": "NOT_FOUND",
    "message": "Pipeline 'non-existent-pipeline' not found"
  }
}
```

### CLI Command

```bash
# Delete pipeline with confirmation prompt
dpkms pipeline remove legal-doc-pipeline
Pipeline 'legal-doc-pipeline' will be permanently deleted. Continue? (y/N): y

# Delete pipeline without confirmation
dpkms pipeline remove legal-doc-pipeline --no-confirm
# Force delete (dangerous)
dpkms pipeline remove legal-doc-pipeline --force

# Archive pipeline instead of deleting
dpkms pipeline archive legal-doc-pipeline
dpkms pipeline unarchive legal-doc-pipeline
```

### Storage Operations

```sql
-- Get pipeline for validation
SELECT * FROM pipelines WHERE name = ?

-- Delete custom pipeline
DELETE FROM pipelines WHERE name = ? AND is_builtin = 0

-- Archive custom pipeline
UPDATE pipelines SET archived = 1 WHERE name = ? AND is_builtin = 0

-- Unarchive pipeline
UPDATE pipelines SET archived = 0 WHERE name = ?
```

### Cascade Considerations

When deleting a pipeline, consider:
- Should associated steps be uninstalled?
- Should there be a warning about in-use jobs?
- Should jobs with this pipeline fail gracefully or continue with built-in fallback?

## E2E Test Checklist

- [ ] `dpkms pipeline remove <name>` (with confirmation) → request sent to
  `DELETE /api/v1/pipelines/{name}` with `X-Confirmation: true` header
- [ ] `dpkms pipeline remove <name> --no-confirm` → request sent with `X-Confirmation: true`
  header (skips prompt, still sends confirmation header)
- [ ] `dpkms pipeline remove <name> --force` → request sent with `X-Confirmation: true` header
- [ ] Delete without confirmation prompt accepted → request sent with `X-Confirmation: false`
  (or header absent) → server rejects if confirmation required
- [ ] Delete custom pipeline returns 200 with success message
- [ ] Verify pipeline record removed from DB: `SELECT * FROM pipelines WHERE name=?` returns
  no rows after successful delete
- [ ] Delete built-in pipeline returns 403 protected error
- [ ] Verify built-in pipeline record unchanged in DB after rejected delete attempt
- [ ] Delete non-existent pipeline returns 404 not found error
- [ ] Delete pipeline currently in use returns 409 conflict error
- [ ] Archive command sets archived flag correctly; verify `archived=1` in DB
- [ ] Unarchive command makes pipeline available again; verify `archived=0` in DB

## Related Stories

- [US-0101](./US-0101-create-custom-pipeline.md) - Create pipelines to delete
- [US-0102](./US-0102-list-and-filter-pipelines.md) - List pipelines before delete
- [US-0103](./US-0103-show-pipeline-details.md) - View pipeline before delete
- [US-0105](./US-0105-archive-pipeline.md) - Archive instead of delete if appropriate

## Related ADRs

- [ADR-004](../../decisions/ADR-004-step-based-pipeline.md) - Step-based pipeline architecture (existing)
- [ADR-049](../../decisions/ADR-049-edges-table-for-mentions.md) - Edges for mentions (existing)
