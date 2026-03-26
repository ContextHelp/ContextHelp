# US-0102: List and Filter Pipelines

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/README.md)

---

## User Goal

As a platform integrator or maintainer, I want to list all available pipelines with filtering options so that I can:
- Browse all configured pipelines
- Check which pipelines are active vs archived
- Understand pipeline configuration before using them
- Debug pipeline availability issues

## Context

Pipelines are stored in a database with metadata including:
- Built-in status (`is_builtin`)
- Archive status (`archived`)
- Creation and update timestamps
- Step definitions
- Sandbox configuration

Users need flexible filtering to:
- See only active pipelines for normal operations
- Include archived pipelines for review or recovery
- Filter by name pattern for search
- Understand pipeline complexity (step count)

## Acceptance Criteria

- **Default list shows only active pipelines** (archived=false)
- **Can include archived pipelines with flag** (`--with-archive`)
- **Can show only archived pipelines** (`--only-archive`)
- **Can filter by name pattern** (substring match)
- **List shows key metadata** for each pipeline (name, description, step count, status)
- **Built-in pipelines are marked** for easy identification
- **Output format is configurable** (text, JSON, YAML)
- **CLI output is human-readable** with table formatting

## Implementation Notes

### API Endpoint

**GET** `/api/v1/pipelines`

**Query Parameters:**
- `name` (optional): Filter by name pattern (substring match)
- `include_archived` (optional, default false): Include archived pipelines
- `only_archived` (optional): Show only archived pipelines
- `format` (optional, default text): Output format (text, json, yaml)

**Examples:**
```
GET /api/v1/pipelines
GET /api/v1/pipelines?include_archived=true
GET /api/v1/pipelines?only_archived=true
GET /api/v1/pipelines?name=legal
```

**Response (text):**
```json
{
  "data": [
    {
      "id": "pipe-abc123",
      "name": "legal-doc-pipeline",
      "description": "Specialized pipeline for legal document analysis",
      "steps_count": 4,
      "is_builtin": false,
      "archived": false,
      "sandbox_enabled": true,
      "created_at": "2025-02-18T10:00:00Z",
      "updated_at": "2025-02-18T10:00:00Z"
    },
    {
      "id": "pipe-builtin-001",
      "name": "text.short",
      "description": "Short text pipeline (< 500 chars)",
      "steps_count": 2,
      "is_builtin": true,
      "archived": false,
      "sandbox_enabled": false,
      "created_at": "2025-01-01T00:00:00Z",
      "updated_at": "2025-01-01T00:00:00Z"
    }
  ],
  "total": 2
}
```

**Response (JSON/YAML):**
```json
{
  "data": [
    {
      "id": "pipe-abc123",
      "name": "legal-doc-pipeline",
      "description": "Specialized pipeline for legal document analysis",
      "steps": [
        {"type": "type_detector"},
        {"type": "legal_classifier", "config": {"model": "legal-v2"}},
        {"type": "tagger"},
        {"type": "citation_extractor", "config": {"format": "bluebook"}}
      ],
      "sandbox": {
        "enabled": true,
        "isolation_level": "container",
        "resource_limits": {
          "max_memory": "1GB",
          "max_cpu": "100%",
          "timeout": "60s"
        }
      },
      "is_builtin": false,
      "archived": false,
      "created_at": "2025-02-18T10:00:00Z",
      "updated_at": "2025-02-18T10:00:00Z"
    }
  ],
  "total": 1
}
```

### CLI Commands

```bash
# List all active pipelines (default)
dpkms pipeline list
dpkms pipeline ls
dpkms pipeline all

# Include archived pipelines
dpkms pipeline list --with-archive

# Show only archived pipelines
dpkms pipeline list --only-archive

# Filter by name pattern
dpkms pipeline list --name legal

# Output as JSON
dpkms pipeline list --format json

# Output as YAML
dpkms pipeline list --format yaml
```

**Table Output (default):**
```
Name                | Description                      | Steps | Built-in | Archived | Sandbox
--------------------|----------------------------------|-----------|----------|---------
legal-doc-pipeline   | Specialized for legal docs...  | 4     | No        | No       | container
text.short          | Short text pipeline (< 500)     | 2     | Yes       | No       | -
text.long           | Long text pipeline (>= 500)     | 3     | Yes       | No       | -
```

### Storage Query

```sql
SELECT 
    id,
    name,
    description,
    json_array_length(steps) as steps_count,
    is_builtin,
    archived,
    json_extract(sandbox, '$.enabled') as sandbox_enabled,
    created_at,
    updated_at
FROM pipelines
WHERE (1 = :include_archived OR archived = 0)
  AND (1 = :only_archived OR archived = 1)
  AND (:name = '' OR name LIKE '%' || :name || '%')
ORDER BY 
    is_builtin ASC,
    name ASC
```

### Filtering Logic

1. **Default**: `archived = false` (active pipelines only)
2. **Include archived**: No archive filter (all pipelines)
3. **Only archived**: `archived = true`
4. **Name filter**: `name LIKE '%pattern%'` (case-insensitive)
5. **Sorting**: Built-in pipelines first (for visibility), then alphabetical by name

### Output Formats

**Text (table):**
- Formatted with columns: Name, Description, Steps, Built-in, Archived, Sandbox
- Truncated long text with ellipsis
- Status indicators (✓/✗)

**JSON:**
- Full pipeline objects with steps and sandbox config
- Includes metadata fields

**YAML:**
- Human-readable full config
- Useful for config inspection and export

## E2E Test Checklist

- [ ] List active pipelines shows only non-archived pipelines
- [ ] `--with-archive` → request contains `include_archived=true` query param; response includes
  archived pipelines
- [ ] `--only-archive` → request contains `only_archived=true` query param; response contains
  only archived pipelines
- [ ] `--name legal` → request contains `name=legal` query param; only matching pipelines returned
- [ ] `--format json` → request contains `format=json` query param; response is JSON
- [ ] `--format yaml` → request contains `format=yaml` query param; response is YAML
- [ ] Default (no flags) → request omits `include_archived` and `only_archived` params;
  only active pipelines returned
- [ ] Verify results match DB state: active pipelines in response correspond to
  `SELECT * FROM pipelines WHERE archived=0` records
- [ ] Verify archived pipelines in `--with-archive` response match
  `SELECT * FROM pipelines` (all records)
- [ ] Built-in pipelines are identified with flag (`is_builtin=true` in response)
- [ ] Step count is accurate for each pipeline
- [ ] Output format text shows table correctly
- [ ] Output format json returns full pipeline objects
- [ ] Output format yaml returns valid YAML
- [ ] Sorting places built-in before custom pipelines
- [ ] Empty list returns appropriate message
- [ ] Filter combinations work correctly (e.g., `--name legal --only-archive` → both params
  present in request)

## Related Stories

- [US-0101](./US-0101-create-custom-pipeline.md) - Create pipelines to list
- [US-0103](./US-0103-show-pipeline-details.md) - View full pipeline configuration
- [US-0105](./US-0105-archive-pipeline.md) - Archive pipelines
- [US-0106](./US-0106-enqueue-content-via-dpkms.md) - Use listed pipelines in enqueue
- [US-0107](./US-0107-discover-local-steps.md) - Discover available steps to reference in pipeline
- [US-0108](./US-0108-install-step-from-registry.md) - Install from registry
- [US-0109](./US-0109-fetch-registry-manifest.md) - Get available steps from registry
- [US-0110](./US-0110-check-registry-updates.md) - Check for updates
- [US-0111](./US-0111-configure-registry-autoupdate.md) - Configure auto-update
- [US-0112](./US-0112-configure-sandbox-per-pipeline.md) - Configure sandbox
- [US-0028](../admin/US-0028-register-custom-pipeline.md) - Original custom pipeline story (superseded)

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Integrators](../../personas/platform-integrators.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

[us0102_list_pipelines_test.go](../../../test/integration/us0102_list_pipelines_test.go)
