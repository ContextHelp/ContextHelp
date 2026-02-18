# US-0103: Show Pipeline Details

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/README.md)

---

## User Goal

As a platform integrator or maintainer, I want to view detailed configuration of a specific pipeline so that I can:
- Understand what steps a pipeline executes
- Verify sandbox and resource limits
- Debug pipeline issues
- Export pipeline configuration for backup or sharing
- Review step configurations and parameters

## Context

Pipelines are stored in a database with:
- Step definitions (type and config)
- Sandbox configuration (isolation level, resource limits, network access, filesystem rules)
- Metadata (creation time, update time, built-in status, archive status)

Users need a detailed view to:
- See exact step sequence and parameters
- Understand security constraints (sandboxing, resource limits)
- Verify configuration before using pipeline
- Export configuration for version control or sharing

## Acceptance Criteria

- **Show full pipeline configuration** including all steps with configs
- **Display sandbox settings** (isolation level, resource limits, network access, filesystem rules)
- **Show metadata** (creation/updated times, built-in status, archive status)
- **Support multiple output formats** (text, JSON, YAML, TOML)
- **Return 404 for non-existent pipeline name**
- **Human-readable text output** with clear formatting

## Implementation Notes

### API Endpoint

**GET** `/api/v1/pipelines/{name}`

**Response:**
```json
{
  "id": "pipe-abc123",
  "name": "legal-doc-pipeline",
  "description": "Specialized pipeline for legal document analysis",
  "steps": [
    {
      "type": "type_detector",
      "config": {}
    },
    {
      "type": "legal_classifier",
      "config": {
        "model": "legal-v2"
      }
    },
    {
      "type": "tagger",
      "config": {}
    },
    {
      "type": "citation_extractor",
      "config": {
        "format": "bluebook"
      }
    }
  ],
  "sandbox": {
    "enabled": true,
    "isolation_level": "container",
    "resource_limits": {
      "max_memory": "1GB",
      "max_cpu": "100%",
      "timeout": "60s"
    },
    "network": false,
    "filesystem": {
      "read_only": ["assets/"],
      "write_allowed": false
    }
  },
  "is_builtin": false,
  "archived": false,
  "created_at": "2025-02-18T10:00:00Z",
  "updated_at": "2025-02-18T10:00:00Z"
}
```

**Not Found:**
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
# Show pipeline details (default text format)
dpkms pipeline show legal-doc-pipeline
dpkms pipeline view legal-doc-pipeline

# Output as JSON
dpkms pipeline show legal-doc-pipeline --format json

# Output as YAML
dpkms pipeline show legal-doc-pipeline --format yaml

# Output as TOML
dpkms pipeline show legal-doc-pipeline --format toml
```

**Text Output:**
```
Pipeline: legal-doc-pipeline
Description: Specialized pipeline for legal document analysis
ID: pipe-abc123
Built-in: No
Archived: No
Created: 2025-02-18 10:00:00 UTC
Updated: 2025-02-18 10:00:00 UTC

Steps:
  1. type_detector
   2. legal_classifier (model: legal-v2)
  3. tagger
  4. citation_extractor (format: bluebook)

Sandbox:
  Enabled: Yes
  Isolation: container
  Memory: 1GB
  CPU: 100%
  Timeout: 60s
  Network: Disabled
  Read-only paths: assets/
  Write allowed: No
```

**JSON Output:**
```json
{
  "name": "legal-doc-pipeline",
  "description": "Specialized pipeline for legal document analysis",
  "steps": [
    {"type": "type_detector", "config": {}},
    {"type": "legal_classifier", "config": {"model": "legal-v2"}},
    {"type": "tagger", "config": {}},
    {"type": "citation_extractor", "config": {"format": "bluebook"}}
  ],
  "sandbox": {
    "enabled": true,
    "isolation_level": "container",
    "resource_limits": {
      "max_memory": "1GB",
      "max_cpu": "100%",
      "timeout": "60s"
    },
    "network": false,
    "filesystem": {
      "read_only": ["assets/"],
      "write_allowed": false
    }
  },
  "is_builtin": false,
  "archived": false,
  "created_at": "2025-02-18T10:00:00Z",
  "updated_at": "2025-02-18T10:00:00Z"
}
}
```

**TOML Output:**
```toml
name = "legal-doc-pipeline"
description = "Specialized pipeline for legal document analysis"

[[steps]]
type = "type_detector"

[[steps]]
type = "legal_classifier"
[steps.config]
model = "legal-v2"

[[steps]]
type = "tagger"

[[steps]]
type = "citation_extractor"
[steps.config]
format = "bluebook"

[sandbox]
enabled = true
isolation_level = "container"

[sandbox.resource_limits]
max_memory = "1GB"
max_cpu = "100%"
timeout = "60s"

[sandbox.filesystem]
read_only = ["assets/"]
write_allowed = false

is_builtin = false
archived = false
created_at = "2025-02-18T10:00:00Z"
updated_at = "2025-02-18T10:00:00Z"
```

### Storage Retrieval

```sql
SELECT 
    id,
    name,
    description,
    steps,
    sandbox,
    is_builtin,
    archived,
    created_at,
    updated_at
FROM pipelines
WHERE name = ?
```

### Output Formatting

**Text:**
- Section-based layout (Metadata, Steps, Sandbox)
- Clear step numbering
- Compact config display

**JSON:**
- Full database object
- Nested structure for readability

**YAML:**
- Clean YAML formatting
- Appropriate for config files

**TOML:**
- Simple key-value syntax
- Easy to read and edit
```
## E2E Test Checklist

- [ ] Show existing custom pipeline returns full configuration
- [ ] Show built-in pipeline returns full configuration
- [ ] Show archived pipeline returns full configuration
- [ ] Request non-existent pipeline returns 404
- [ ] Text output is human-readable and well-formatted
- [ ] JSON output is valid JSON with all fields
- [ ] YAML output is valid YAML with correct structure
- [ ] TOML output is valid TOML with correct structure
- [ ] All steps with their configs are displayed
- [ ] Sandbox configuration is completely displayed
- [ ] Metadata (timestamps, flags) are accurate

## Related Stories

- [US-0101](./US-0101-create-custom-pipeline.md) - Create pipelines to show
- [US-0102](./US-0102-list-and-filter-pipelines.md) - List and discover pipelines
- [US-0104](./US-0104-delete-custom-pipeline.md) - Delete pipeline if no longer needed
- [US-0105](./US-0105-archive-pipeline.md) - Archive instead of deleting
- [US-0106](./US-0106-enqueue-content-via-dpkms.md) - Use listed pipelines in enqueue
- [US-0107](./US-0107-discover-local-steps.md) - Discover available steps to reference in pipeline
- [US-0108](./US-0108-install-step-from-registry.md) - Install from registry
- [US-0109](./US-0109-fetch-registry-manifest.md) - Get available steps from registry
- [US-0110](./US-0110-check-registry-updates.md) - Check for updates
- [US-0111](./US-0111-configure-registry-autoupdate.md) - Configure auto-update
- [US-0112](./US-0112-configure-sandbox-per-pipeline.md) - Configure sandbox
- [US-0113](./US-0113-ctxt-analyze-api-client.md) - ctxt as API client
- [US-0028](../admin/US-0028-register-custom-pipeline.md) - Original custom pipeline story (superseded)