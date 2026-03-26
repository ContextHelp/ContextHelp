# US-0101: Create Custom Pipeline

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/README.md)

---

## User Goal

As a platform integrator or maintainer, I want to create custom ingestion pipelines so that I can tailor content processing to specific needs (domain-specific enrichment, custom workflows, etc.).

## Context

Built-in pipelines (`text.short`, `text.long`) provide general-purpose processing but users often need specialized pipelines for:
- Domain-specific enrichment (legal analysis, technical documentation, marketing copy)
- Custom workflows (multi-stage validation, conditional routing)
- Organization-specific tagging schemes
- Integration with external tools and APIs

Custom pipelines must be defined declaratively, validated, stored persistently, and made available for use in `dpkms pipeline enqueue` and `ctxt analyze`.

## Acceptance Criteria

- **Pipeline can be created from config file** (JSON, YAML, or TOML)
- **Step names are validated** against registered steps (built-in, local, or registry)
- **Pipeline config is stored in database** with metadata and creation timestamp
- **Built-in pipeline names are protected** from being overwritten (e.g., `text.*`)
- **Duplicate pipeline names are rejected** with clear error message
- **Sandbox configuration is parsed and stored** (resource limits, network access, filesystem constraints)
- **Created pipeline is immediately available** for enqueue operations
- **Error handling is clear and actionable** (validation errors, storage failures)

## Implementation Notes

### Pipeline Config Format

Pipelines are defined in JSON, YAML, or TOML format.

**YAML Example:**
```yaml
name: legal-doc-pipeline
description: Specialized pipeline for legal document analysis

sandbox:
  enabled: true
  isolation_level: container
  resource_limits:
    max_memory: 1GB
    max_cpu: "100%"
    timeout: 60s
  network: false
  filesystem:
    read_only:
      - assets/
    write_allowed: false

steps:
  - type: type_detector
  - type: legal_classifier
    config:
      model: legal-v2
  - type: tagger
  - type: citation_extractor
    config:
      format: bluebook
```

### API Endpoint

**POST** `/api/v1/pipelines`

**Request Body:**
```json
{
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
    },
    "network": false,
    "filesystem": {
      "read_only": ["assets/"],
      "write_allowed": false
    }
  }
}
```

**Response:**
```json
{
  "id": "pipe-abc123",
  "name": "legal-doc-pipeline",
  "created_at": "2025-02-18T10:00:00Z"
}
```

**Validation Errors:**
```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Step 'legal_classifier' is not registered",
    "details": {
      "invalid_steps": ["legal_classifier"],
      "available_steps": ["type_detector", "tagger", "sectioner", "..."]
    }
  }
}
```

**Built-in Protection:**
```json
{
  "error": {
    "code": "PROTECTED_PIPELINE",
    "message": "Pipeline names starting with 'text.' are protected"
  }
}
```

**Duplicate Name:**
```json
{
  "error": {
    "code": "DUPLICATE_NAME",
    "message": "Pipeline 'legal-doc-pipeline' already exists"
  }
}
```

### CLI Command

```bash
# Create from YAML file
dpkms pipeline create legal-doc-pipeline ./legal-doc-pipeline.yaml

# Create from JSON file
dpkms pipeline create legal-doc-pipeline ./legal-doc-pipeline.json

# Create from TOML file
dpkms pipeline create legal-doc-pipeline ./legal-doc-pipeline.toml
```

### Validation Logic

1. **Name validation**: Must match pattern `[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*` and not be protected prefix
2. **Step existence**: All step names must be registered in step store
3. **Config schema validation**: If step defines `config_schema`, validate config against it
4. **Sandbox configuration**: Validate isolation level is supported (`process` or `container`)
5. **Resource limits**: Parse and validate memory/CPU/timeout format
6. **Duplicate detection**: Check for existing pipeline with same name

### Storage Schema

```sql
CREATE TABLE IF NOT EXISTS pipelines (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    description TEXT DEFAULT '',
    steps JSON DEFAULT '[]',
    is_builtin INTEGER DEFAULT 0,
    archived INTEGER DEFAULT 0,
    sandbox TEXT DEFAULT NULL,  -- JSON blob of SandboxConfig
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

## E2E Test Checklist

- [ ] Create pipeline from YAML config file with valid steps
- [ ] Create pipeline from JSON config file with valid steps
- [ ] Create pipeline from TOML config file with valid steps
- [ ] Verify request payload contains `name`, `description`, `steps` array, and `sandbox` object
- [ ] Verify `name` in request payload matches CLI argument
- [ ] Verify `steps` array in request payload contains correct step types and configs
- [ ] Verify `sandbox` object in request payload contains all specified fields
  (enabled, isolation_level, resource_limits, network, filesystem)
- [ ] Verify pipeline record stored in DB: query `pipelines` table, confirm `name`, `steps`,
  `sandbox`, `created_at`, `updated_at` fields present and correct
- [ ] Verify `is_builtin=0` for newly created custom pipeline in DB
- [ ] Verify `archived=0` for newly created pipeline in DB
- [ ] Verify pipeline is immediately available for enqueue (GET /api/v1/pipelines/{name} → 200)
- [ ] Attempt to create pipeline with invalid step name → error returned
- [ ] Attempt to create pipeline with protected name prefix → error returned
- [ ] Attempt to create pipeline with duplicate name → error returned
- [ ] Create pipeline with invalid sandbox config → validation error returned
- [ ] Create pipeline with resource limits → limits stored correctly in DB
  (`json_extract(sandbox, '$.resource_limits.max_memory')` matches input)
- [ ] Verify step names are case-sensitive
- [ ] Verify sandbox configuration is parsed and stored as JSON blob in DB

## Related Stories

- [US-0102](./US-0102-list-and-filter-pipelines.md) - List created pipelines
- [US-0103](./US-0103-show-pipeline-details.md) - View pipeline configuration
- [US-0106](./US-0106-enqueue-content-via-dpkms.md) - Use custom pipeline in enqueue
- [US-0107](./US-0107-discover-local-steps.md) - Discover available steps to reference in pipeline
- [US-0108](./US-0108-install-step-from-registry.md) - Download and install step
- [US-0109](./US-0109-fetch-registry-manifest.md) - Get available steps from registry
- [US-0110](./US-0110-check-registry-updates.md) - Check for updates
- [US-0111](./US-0111-configure-registry-autoupdate.md) - Configure auto-update
- [US-0112](./US-0112-configure-sandbox-per-pipeline.md) - Configure sandbox settings
- [US-0113](./US-0113-ctxt-analyze-api-client.md) - ctxt uses dpkms API
- [US-0028](../admin/US-0028-register-custom-pipeline.md) - Original custom pipeline story (superseded)

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Integrators](../../personas/platform-integrators.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

[us0101_create_pipeline_test.go](../../../test/integration/us0101_create_pipeline_test.go)
