# US-0110: Check Registry Updates

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/README.md), [Registry Operators](../../personas/README.md), [Maintainers](../../personas/README.md), [Knowledge Workers](../../personas/README.md)

---

## User Goal

As a registry operator or maintainer, I want to check for updates to registry manifests so that I can:
- Monitor when new step versions become available
- Configure auto-update behavior per registry
- Auto-update until explicitly disabled
- View system reminders for pending actions

## Context

Registry manifests include:
- **ETag** (entity tag) for change detection
- **Steps array** (name, path, license, version)
- **Supports** field (agentskills, custom, or both)
- **Version** field for versioning

Caching prevents unnecessary network requests:
- Manifest fetched with ETag is cached
- ETag unchanged → no update needed
- New ETag → change detected, old and new versions stored

Auto-update defaults to **disabled**:
- Manual control only
- Can be enabled per registry
- Updates occur on:
  - Manual trigger (`dpkms pipeline step registry update <url>`)
  - Scheduled check (e.g., nightly cron, hourly)
  - Notification on successful fetch

## Acceptance Criteria

- **Check returns available step versions for all configured registries**
- **Create system reminders when new version is detected**
- **Reminders track**: Registry URL, step name, old version, new version, count
- **Manual update control**: User can trigger registry update or disable auto-update

- **Auto-update is manual by default** — respects user control
- Settings persisted per registry in database
- **Reminders dismissed** once acknowledged

## Implementation Notes

### API Endpoint

**GET** `/api/v1/system/reminders`

**Query Parameters:**
- `active` (optional, default true): Show only active reminders
- `source` (optional): Filter reminders by source

**Response:**
```json
{
  "data": [
    {
      "id": "rem-001",
      "type": "registry_update",
      "title": "New version available: legal-doc-analyzer v1.2.0",
      "message": "Step legal-doc-analyzer has new version 1.2.0 available from https://example.com/registry (download and install to use)",
      "source": "https://example.com/registry",
      "action_url": "dpkms pipeline step registry update https://example.com/registry",
      "created_at": "2025-02-18T10:00:00Z",
      "updated_at": "2025-02-18T10:00:00Z",
      "dismissed": false
      "priority": "medium",
      "action_required": false
    },
    {
      "id": "rem-002",
      "type": "step_available",
      "title": "3 new steps added",
      "message": "Steps entity-extractor, custom-step, code-identifier (0,5) available from registry",
      "source": "https://github.com/example-org/custom-steps",
      "action_url": "dpkms pipeline step install entity-extractor",
      "created_at": "2025-02-18T10:05:00Z",
      "updated_at": "2025-02-18T10:05:00Z",
      "dismissed": false,
      "priority": "medium",
      "action_required": false
    }
  ]
}
```

### CLI Commands

```bash
# List all active reminders
dpkms system reminders list [--active]

# List all reminders including dismissed
dpkms system reminders list

# Dismiss reminder
dpkms system reminders dismiss rem-001

# Mark reminder as viewed
dpkms system reminders view rem-001

# Clear all reminders
dpkms system reminders clear --all
```

# Trigger manual update
dpkms pipeline step registry update https://example.com/registry
dpkms pipeline step registry autoupdate <url> [--enable|--disable]
```

### Storage Operations

```sql
-- Get all reminders
SELECT id, type, title, message, source, action_url, dismissed, created_at, updated_at
FROM system_reminders
WHERE dismissed = 0
ORDER BY created_at DESC
LIMIT 100

-- Create reminder
INSERT INTO system_reminders (id, type, title, message, source, action_url, dismissed, created_at, updated_at)
VALUES (?, 'step_available', 'New version available: legal-doc-analyzer v1.2.0', 'https://example.com/registry', 'dpkms pipeline step registry update https://example.com/registry', ?, ?, ?, ?, NOW(), FALSE)

-- Dismiss reminder
UPDATE system_reminders SET dismissed = ? WHERE id = ?

-- Clear all reminders
-- DELETE FROM system_reminders WHERE dismissed = 0
-- UPDATE registry_cache SET auto_update = 1 WHERE registry_url = ?
```
```

### Auto-Update Process

When auto-update is enabled:
1. **Scheduled check** - On interval (e.g., hourly, daily)
2. **For each registry**:
   a. Fetch manifest (GET /api/v1/steps/registries/fetch?url=...)
   b. Parse ETag, get version
   c. Compare with cached manifest
   c. Create reminders if new version detected
3. **Manual trigger** - Allow disabling

### System Reminder Priority

| Priority | When Auto-Update | Action URL |
|---------|----------|-------------|----------------------------------|
| High     | Version change (major version bump), 3 steps added | Manual update URL |
| Medium  | Multiple steps added, step changes detected | Manual update URL |
| Low     | Single step added, version bump, one step removed | Update URL |
| Lowest  | Single step added | No action URL |

### Error Conditions

- Registry offline → Don't fail entire check, just log warning
- Network error → Retry with exponential backoff, limit retries
- No change → No reminder, try again next scheduled check
- No step changes → Don't create reminders
- Step count = 0 → No reminders needed

## E2E Test Checklist

- [ ] `dpkms system reminders list --active` → request contains `active=true` query param;
  response includes only reminders with `dismissed=false` in DB
- [ ] `dpkms system reminders list` (no flag) → request omits `active` param; response
  includes all reminders regardless of dismissed status
- [ ] `dpkms system reminders list --source <url>` → request contains `source=<url>` query
  param; response matches `SELECT * FROM system_reminders WHERE source=?`
- [ ] `dpkms system reminders dismiss <id>` → request sent to dismiss endpoint with reminder
  ID; verify `dismissed=1` in `system_reminders` table for that ID
- [ ] Fetch manifest returns valid manifest with ETag
- [ ] Registry cache is cleared on manual update → manifest saved with new ETag in DB
- [ ] New version detected → system reminder created; verify `system_reminders` row with
  correct `type`, `title`, `source`, `action_url`, `dismissed=0`
- [ ] Version comparison works correctly
- [ ] All step packages extracted and validated
- [ ] Registry cache is cleared when manual update triggered
- [ ] Manual update flag respected (no auto-update unless enabled); `auto_update` unchanged
  in `registry_cache`
- [ ] System reminders list returns reminders ordered by created_at DESC; verify ordering
  matches DB query `ORDER BY created_at DESC`
- [ ] System reminders dismissed flags are correct in DB after dismiss operation
- [ ] High priority reminder has oldest created_at
- [ ] Low priority reminder shows updated_at
- [ ] Action URLs are clickable

## Related Stories

- [US-0107](./US-0107-discover-local-steps.md) - Discover steps on system
- [US-0108](./US-0108-install-step-from-registry.md) - Install from registry
- [US-0109](./US-0109-fetch-registry-manifest.md) - Get available steps from registry
- [US-0111](./US-0111-configure-registry-autoupdate.md) - Configure auto-update
- [US-0112](./US-0112-configure-sandbox-per-pipeline.md) - Configure sandbox
- [US-0113](./US-0113-ctxt-analyze-api-client.md) - ctxt as API client

## Related ADRs

- [ADR-004](../../decisions/ADR-004-step-based-pipeline.md) - Step-based pipeline architecture (existing)
- [ADR-057](../../decisions/ADR-057-registry-update-notification-model.md) - Registry update notification model (this ADR)
- [ADR-058](../../decisions/ADR-058-external-step-execution-protocol.md) - External step execution protocol
- [ADR-056](../../decisions/ADR-056-unified-enqueue-api.md) - Unified enqueue API (this ADR defines)

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Integrators](../../personas/platform-integrators.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

- `test/integration/us0110_reminders_test.go::TestUS0110_ListReminders`
- `test/integration/us0110_reminders_test.go::TestUS0110_DismissReminder`
- `test/integration/us0110_reminders_test.go::TestUS0110_ActiveOnlyFilter`
- `test/integration/us0110_reminders_test.go::TestUS0110_DismissThenVerify`
- `test/integration/us0110_reminders_test.go::TestUS0110_MultipleTypes`
- `test/integration/us0110_reminders_test.go::TestUS0110_DismissNonExistentIsIdempotent`
