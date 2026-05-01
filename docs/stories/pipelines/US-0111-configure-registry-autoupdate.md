# US-0111: Configure Registry Auto-Update

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Platform Integrators](../../personas/README.md), [Registry Operators](../../personas/README.md), [Maintainers](../../personas/README.md)

---

## User Goal

As a registry operator or maintainer, I want to configure auto-update behavior per registry so that:
- Enable automatic updates when new steps are available
- Disable automatic updates and only update when explicitly triggered
- Configure different update intervals per registry

## Context

Registry supports auto-update configuration:
- When enabled, registry manifests are checked periodically and new steps are automatically downloaded and installed
- Users can disable this and manage updates manually via CLI commands

Default: Disabled
- Users must manually trigger updates using:
- CLI: `dpkms pipeline step registry update <url>` or scheduled checks

## Acceptance Criteria

- **Auto-update is manual by default** - prevents unwanted changes
- **Update intervals can be configured** (hourly, daily, etc.)
- **System reminders are created for available updates** - users can view and decide
- **Manual update commands are available** - dpkms pipeline step registry update <url>`
- **Per-registry settings are stored in database**

## Implementation Notes

### Config Storage

Registry configuration stored in `registry_cache` table:
- `registry_url` (primary key)
- `manifest` (JSON blob) - manifest data
- `last_fetched` (timestamp string)
- `etag` (string) - for change detection)
- `auto_update` (boolean) - per-registry auto-update flag

### Auto-Update Process

When auto-update is enabled:
1. **Scheduled check** (e.g., every hour)
2. **For each registry**:
   - Fetch manifest (GET /api/v1/steps/registries/fetch?url=...)
   - Parse ETag, get version
   - Compare with cached manifest (if available)
   - If different version:
     - Create SystemReminder for new version
3 - Return success or error with manifest details
     - Increment version for all affected steps
     - Set reminder priority based on number of affected steps
3 - Add action_url linking to registry update command

3. **Manual trigger**: Allow users to disable auto-update or force immediate update

### Priority Levels

| Priority | Scenario | Example | Action URL |
|---------|-------------|-------------|------------------------------------|
| **High** | Major version bump, 3+ steps added, step removed | Manual update | Manual update URL |
| **Medium** | Minor version bump, single step added, step changes detected | Update URL |
| **Low** | Single step added, version bump, no action URL (just notify) |

### System Reminder

Create reminder entry with:
- Type: `registry_update`
- Title: "New version available: [pipeline-name] [version]"
- Message: Clear description of what's new and available
- Source: Registry URL
- Action URL: Link to `dpkms pipeline step registry update <url>` (if action required)
- Priority: medium (default)
- Dismissed: false, CreatedAt: timestamp, UpdatedAt: timestamp

### Error Handling

- **Registry unavailable** → Don't fail entire check
  - Log warning, skip creation of reminders
- **Network error** → Retry with exponential backoff, limit retries
- **Invalid manifest** → Return 400 with validation error
- **Parse error** → Return error with manifest details

## CLI Commands

```bash
# Check auto-update status
dpkms pipeline step registry autoupdate

# List all registries with auto-update status
dpkms pipeline step registry list

# Configure auto-update for specific registry
dpkms pipeline step registry autoupdate <url> [--enable|--disable]

# Trigger manual update
dpkms pipeline step registry update https://example.com/registry

# View registry configuration details
dpkms pipeline step registry show --url https://example.com/registry
```

### Configuration Fields

Per-registry settings stored in database:
```sql
CREATE TABLE IF NOT EXISTS registry_cache (
    registry_url TEXT PRIMARY KEY,
    manifest TEXT DEFAULT '{}',
    last_fetched TEXT NOT NULL,
    etag TEXT DEFAULT '',
    auto_update INTEGER DEFAULT 0
);
```

### Environment Variables

```bash
# Auto-update settings (default)
DPKMS_REGISTRY_AUTO_UPDATE=false

# Per-registry settings (user-configured)
DPKMS_REGISTRY_{}_AUTO_UPDATE=true
```

## E2E Test Checklist

- [ ] `dpkms pipeline step registry autoupdate <url> --enable` → request contains `url` and
  `auto_update=true` (or equivalent flag) in payload; verify `auto_update=1` in
  `registry_cache` table for that URL
- [ ] `dpkms pipeline step registry autoupdate <url> --disable` → request contains `url` and
  `auto_update=false`; verify `auto_update=0` in `registry_cache` table
- [ ] `dpkms pipeline step registry autoupdate` (no args) → request shows current status;
  response reflects `auto_update` field from `registry_cache` DB row
- [ ] `dpkms pipeline step registry list` → response includes `auto_update` status per
  registry; values match `registry_cache` table rows
- [ ] `dpkms pipeline step registry update <url>` → request contains `url`; cache row's
  `last_fetched` updated in DB after successful fetch
- [ ] Registry is added to config
- [ ] Check registry returns cached manifest with all steps and ETag
- [ ] New version detected → system reminder created; verify `system_reminders` row with
  correct `type`, `source`, `action_url`
- [ ] Manual update flag is respected (no auto-update unless enabled); `auto_update=0` in DB
  means scheduled checks do not update
- [ ] System reminders list returns ordered by created_at DESC
- [ ] System reminders dismissed flags are correct in DB
- [ ] High priority reminder has oldest created_at
- [ ] Low priority reminder shows updated_at
- [ ] Action URLs are clickable
- [ ] Disable auto-update for specific registry → `auto_update=0` in `registry_cache`
- [ ] Re-enable → `auto_update=1` in `registry_cache`
- [ ] Auto-update flag persists in database across server restarts
- [ ] Check registry unavailable → error handling works correctly
- [ ] New version detected → system reminder created with message
- [ ] Manual update trigger updates `last_fetched` and `etag` in `registry_cache`
- [ ] System reminders can be created, dismissed

## Related Stories

- [US-0107](./US-0107-discover-local-steps.md) - Discover steps on system
- [US-0108](./US-0108-install-step-from-registry.md) - Install from registry
- [US-0109](./US-0109-fetch-registry-manifest.md) - Get available steps from registry
- [US-0110](./US-0110-check-registry-updates.md) - Check for updates
- [US-0112](./US-0112-configure-sandbox-per-pipeline.md) - Configure sandbox
- [US-0113](./US-0113-ctxt-analyze-api-client.md) - ctxt as API client

## Related ADRs

- [ADR-057](../../decisions/ADR-057-registry-update-notification-model.md) - Registry update notification model (this ADR defines)
- [ADR-058](../../decisions/ADR-058-external-step-execution-protocol.md) - External step execution protocol
- [ADR-056](../../decisions/ADR-056-unified-enqueue-api.md) - Unified enqueue API (this ADR defines)

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- [Platform Integrators](../../personas/platform-integrators.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

- `test/integration/us0111_autoupdate_test.go::TestUS0111_AutoUpdate_SkipWhenETagUnchanged`
- `test/integration/us0111_autoupdate_test.go::TestUS0111_AutoUpdate_UpgradeWhenETagChanges`
- `test/integration/us0111_autoupdate_test.go::TestUS0111_AutoUpdate_NotifyWhenNotAutoUpdate`
- `test/integration/us0111_autoupdate_test.go::TestUS0111_AutoUpdate_VersionComparison`
