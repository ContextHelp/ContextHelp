# US-0109: Fetch Registry Manifest

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/README.md), [Registry Operators](../../personas/README.md)

---

## User Goal

As a registry operator, I want to fetch manifest from a registry to:
- View available steps
- Get version information
- Check for updates since last fetch
- Cache manifest locally for offline access

## Context

Registries provide manifest files:
- **MANIFEST.yaml** at registry root with name, description, version, steps array
- **steps/... section** entries
- **ETag** for change detection
**Supports** field (agentskills, custom, or both)

Caching prevents unnecessary network requests:
- Manifest fetched with ETag is cached
- ETag unchanged → no update
- New ETag → change detected, old and new versions stored

## Acceptance Criteria

- **Registry URL is accessible** and returns valid manifest
- **Manifest format follows specification**
- **ETag changes detected via HEAD or If-Modified-Since header**
- **Step list is parsed and validated**

## Implementation Notes

### API Endpoint

**GET** `/api/v1/steps/registries/fetch`

**Query Parameters:**
- `url` (required) - Registry URL to fetch

**Examples:**
```
GET /api/v1/steps/registries/fetch?url=https://example.com/registry/MANIFEST.yaml
```

**Response (success):**
```json
{
  "manifest": {
    "name": "contexthelp-steps",
    "description": "Collection of pipeline steps for ContextHelp",
    "version": "1.0",
    "steps": [
      {
        "name": "sentiment-analyzer",
        "path": "/steps/sentiment-analyzer",
        "license": "MIT",
        "version": "1.2.0"
      }
    ],
    "supports": ["agentskills", "custom"]
  },
  "etag": "abc123def"
}
```

**Response (error):**
```json
{
  "error": {
    "code": "REGISTRY_UNAVAILABLE",
    "message": "Failed to connect to registry: https://example.com",
    "details": "Network error or invalid response"
  }
}
```

### CLI Commands

```bash
# Fetch manifest from registry
dpkms pipeline step registry fetch https://example.com/registry

# List all registries
dpkms pipeline step registry list

# Fetch and cache manifest (manual update trigger)
dpkms pipeline step registry update https://example.com/registry # Manual update
```

# View cached manifest details
dpkms pipeline step registry show --url https://example.com/registry
```
```

### Storage Operations

```sql
-- Get cache
SELECT manifest, etag, last_fetched, auto_update
FROM registry_cache
WHERE registry_url = ?

-- Update or insert cache entry after fetch
INSERT OR REPLACE INTO registry_cache (registry_url, manifest, etag, last_fetched)
VALUES (?, ?, ?, ?)

-- Invalidate cache (for manual update)
UPDATE registry_cache
SET last_fetched = NULL
WHERE registry_url = ?
```

### ETag Change Detection

Compare etag values:
- Changed: "new_etag123" != "old_etag456"
  Updated: New etag + 3 versions available

### System Reminder Creation

When version changes detected:
Create entry in system_reminders:
- Type: "registry_update"
- Title: "New version available: legal-doc-analyzer v1.2.0"
- Message: "Step legal-doc-analyzer has new version 1.2.0 available from registry https://example.com/registry (download and install to use)"
- Source: "https://example.com/registry"
- ActionURL: "dpkms pipeline step registry update https://example.com/registry"

### E2E Test Checklist

- [ ] `dpkms pipeline step registry fetch <url>` → request sent to
  `GET /api/v1/steps/registries/fetch?url=<url>` with `url` query param matching CLI arg
- [ ] `dpkms pipeline step registry update <url>` → request sent to fetch endpoint with
  `url` query param; cache invalidated before re-fetch
- [ ] Fetch manifest returns valid manifest with all steps
- [ ] Registry unavailable → error with connection details
- [ ] ETag returned, manifest saved to cache; verify `registry_cache` table row has correct
  `etag`, `manifest`, `last_fetched` fields after fetch
- [ ] Subsequent fetch with unchanged ETag → no DB update (same `last_fetched` or no new row)
- [ ] New version detected → system reminder created with appropriate message; verify
  `system_reminders` table row with `type="registry_update"` and correct `source` URL
- [ ] Version comparison works correctly (same, newer, older)
- [ ] All step packages are extracted and validated correctly
- [ ] Registry cache is cleared when manual update triggered: `last_fetched` reset in DB
- [ ] Manual update flag respected (no auto-update unless enabled); `auto_update` field in
  `registry_cache` remains unchanged after manual fetch

## Related Stories

- [US-0101](./US-0101-create-custom-pipeline.md) - Create pipelines using installed steps
- [US-0102](./US-0102-list-and-filter-pipelines.md) - List pipelines to discover available steps
- [US-0107](./US-0107-discover-local-steps.md) - Discover steps on system
- [US-0108](./US-0108-install-step-from-registry.md) - Install from registry
- [US-0110](./US-0110-check-registry-updates.md) - Check for updates
- [US-0111](./US-0111-configure-registry-autoupdate.md) - Configure auto-update
- [US-0112](./US-0112-configure-sandbox-per-pipeline.md) - Configure sandbox

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Integrators](../../personas/platform-integrators.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

[us0109_registry_test.go](../../../test/integration/us0109_registry_test.go)
