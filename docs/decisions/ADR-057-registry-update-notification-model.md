# ADR-057 – Registry Update Notification Model

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

Users can configure multiple step registries to discover and install pipeline steps. These registries are:
- **External services** (e.g., GitHub, custom registries)
- **Mutable** – new steps added, versions updated, steps removed
- **Cached locally** to avoid repeated network requests
- **Configurable for auto-update** – can automatically check for updates

**Problem 1: Users don't know when updates are available**
- Registries may add new steps or update existing ones
- Users must manually check each registry
- No notification when something relevant to them is available

**Problem 2: Manual update process is tedious**
- Users must remember to check for updates
- Must manually inspect each registry's manifest
- No way to track what has changed since last check

**Problem 3: Update frequency varies by user**
- Some users want automatic updates for all registries
- Some users want manual control for specific registries
- Some users want scheduled checks (daily, weekly)
- Some users want notifications only (no auto-download)

**Problem 4: Different types of updates matter differently**
- **Major version bump** (e.g., 1.2.0 → 2.0.0): Breaking changes, important to know
- **Minor version bump** (e.g., 1.2.0 → 1.3.0): New features, nice to know
- **New step added:** May be immediately useful, should notify
- **Step removed:** May break pipelines, urgent to notify

**The question:** How should the system notify users about registry updates, and what notification model should be used?

---

## Decision

**Use a system reminders table to track registry updates. Reminders are created when registry manifests change (detected via ETag). Reminders have priority levels, action URLs, and dismissal state. Auto-update is manual by default – users must explicitly enable per-registry auto-update.**

### System Reminders Table

```sql
CREATE TABLE IF NOT EXISTS system_reminders (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,               -- 'registry_update', 'step_available', etc.
    title TEXT NOT NULL,
    message TEXT NOT NULL,
    source TEXT,                      -- Registry URL or other source
    action_url TEXT,                   -- CLI command or HTTP URL
    priority TEXT DEFAULT 'medium',    -- 'high', 'medium', 'low'
    action_required INTEGER DEFAULT 0,   -- Boolean
    dismissed INTEGER DEFAULT 0,         -- Boolean
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

### Reminder Types

#### Type 1: Registry Version Change

Created when registry manifest version changes (detected via ETag comparison).

**Priority:** `medium` or `high` (based on significance)

**Example:**
```json
{
  "id": "rem-001",
  "type": "registry_update",
  "title": "New version available: contexthelp-steps v1.1.0",
  "message": "Registry contexthelp-steps updated from v1.0.0 to v1.1.0. 3 new steps added: legal-classifier, entity-extractor, citation-parser.",
  "source": "https://example.com/registry/MANIFEST.yaml",
  "action_url": "dpkms pipeline step registry update https://example.com/registry",
  "priority": "medium",
  "action_required": false,
  "dismissed": false,
  "created_at": "2026-02-18T10:00:00Z",
  "updated_at": "2026-02-18T10:00:00Z"
}
```

#### Type 2: Step Available

Created when a specific step is added or updated that matches user's interests (e.g., steps used in their pipelines).

**Priority:** `medium` or `low`

**Example:**
```json
{
  "id": "rem-002",
  "type": "step_available",
  "title": "New step available: legal-classifier v1.2.0",
  "message": "Step legal-classifier (used in your pipeline 'legal-doc-pipeline') has new version 1.2.0 available with improved accuracy.",
  "source": "https://example.com/registry/MANIFEST.yaml",
  "action_url": "dpkms pipeline step install legal-classifier --registry https://example.com/registry",
  "priority": "high",
  "action_required": false,
  "dismissed": false,
  "created_at": "2026-02-18T10:05:00Z",
  "updated_at": "2026-02-18T10:05:00Z"
}
```

#### Type 3: Step Removed (Breaking Change)

Created when a step is removed from a registry that the user has installed.

**Priority:** `high`, `action_required: true`

**Example:**
```json
{
  "id": "rem-003",
  "type": "step_removed",
  "title": "Breaking change: citation-parser removed",
  "message": "Step citation-parser (used in pipeline 'legal-doc-pipeline') was removed from registry https://example.com/registry. Your pipeline may fail. Consider finding an alternative step.",
  "source": "https://example.com/registry/MANIFEST.yaml",
  "action_url": "dpkms pipeline show legal-doc-pipeline",
  "priority": "high",
  "action_required": true,
  "dismissed": false,
  "created_at": "2026-02-18T10:10:00Z",
  "updated_at": "2026-02-18T10:10:00Z"
}
```

### Priority Levels

| Priority | When Used | Action URL | Action Required |
|---------|------------|-------------|-----------------|
| **High** | Major version bump (X.Y.0 → X+1.0.0), step used by user removed, 3+ steps added | Manual update URL | Yes (if step removed) or No (if version bump) |
| **Medium** | Minor version bump (X.Y.Z → X.Y+1.0), single new step, step used by user updated | Manual update URL or install URL | No |
| **Low** | Patch version bump (X.Y.Z → X.Y.Z+1), step not used by user added/updated | None or view URL | No |

### ETag Change Detection

Registry manifests include an `ETag` header (entity tag) for change detection.

**Detection Process:**
```go
func (rc *RegistryCache) CheckForUpdates(ctx context.Context, registryURL string) ([]Reminder, error) {
    // 1. Fetch cached manifest with ETag
    cached, err := rc.Get(ctx, registryURL)
    if err != nil {
        return nil, err
    }

    // 2. Fetch current manifest with ETag
    current, etag, err := fetchManifest(ctx, registryURL)
    if err != nil {
        return nil, err
    }

    // 3. Compare ETags
    if cached.ETag == etag {
        // No change
        return nil, nil
    }

    // 4. Compare versions and steps
    var reminders []Reminder

    // 4a. Version changed?
    if cached.Manifest.Version != current.Version {
        reminders = append(reminders, Reminder{
            Type:    "registry_update",
            Title:   fmt.Sprintf("New version available: %s %s", current.Name, current.Version),
            Message: buildVersionChangeMessage(cached.Manifest, current.Manifest),
            Source:  registryURL,
            ActionURL: "dpkms pipeline step registry update " + registryURL,
            Priority: calculateVersionPriority(cached.Manifest.Version, current.Version),
        })
    }

    // 4b. Steps added/removed/updated?
    for _, step := range current.Steps {
        cachedStep := findStep(cached.Manifest.Steps, step.Name)
        if cachedStep == nil {
            // New step added
            reminders = append(reminders, Reminder{
                Type:    "step_available",
                Title:   fmt.Sprintf("New step available: %s %s", step.Name, step.Version),
                Message: fmt.Sprintf("Step %s added to registry %s", step.Name, registryURL),
                Source:  registryURL,
                ActionURL: "dpkms pipeline step install " + step.Name,
                Priority: "low",
            })
        } else if cachedStep.Version != step.Version {
            // Step version changed
            if isStepUsedByUser(step.Name) {
                reminders = append(reminders, Reminder{
                    Type:    "step_available",
                    Title:   fmt.Sprintf("Step updated: %s %s → %s", step.Name, cachedStep.Version, step.Version),
                    Message: fmt.Sprintf("Step %s (used in your pipelines) updated to %s", step.Name, step.Version),
                    Source:  registryURL,
                    ActionURL: "dpkms pipeline step install " + step.Name,
                    Priority: "high",
                })
            }
        }
    }

    // 4c. Steps removed?
    for _, cachedStep := range cached.Manifest.Steps {
        currentStep := findStep(current.Steps, cachedStep.Name)
        if currentStep == nil && isStepUsedByUser(cachedStep.Name) {
            // Step removed (breaking change if used by user)
            reminders = append(reminders, Reminder{
                Type:           "step_removed",
                Title:          fmt.Sprintf("Breaking change: %s removed", cachedStep.Name),
                Message:         fmt.Sprintf("Step %s (used in your pipelines) was removed from registry %s", cachedStep.Name, registryURL),
                Source:          registryURL,
                ActionURL:        "dpkms pipeline show <pipeline-name>",
                Priority:        "high",
                ActionRequired:   true,
            })
        }
    }

    // 5. Update cache with new manifest and ETag
    rc.Update(ctx, registryURL, current, etag)

    return reminders, nil
}
```

### Auto-Update Configuration

**Default:** Auto-update disabled for all registries.

**Configuration:**
```sql
CREATE TABLE IF NOT EXISTS registry_cache (
    registry_url TEXT PRIMARY KEY,
    manifest TEXT DEFAULT '{}',
    etag TEXT DEFAULT '',
    last_fetched TEXT NOT NULL,
    auto_update INTEGER DEFAULT 0  -- Boolean
);
```

**Per-registry control:**
```bash
# Disable auto-update (default)
dpkms pipeline step registry autoupdate https://example.com/registry --disable

# Enable auto-update
dpkms pipeline step registry autoupdate https://example.com/registry --enable

# Check auto-update status
dpkms pipeline step registry show --url https://example.com/registry
```

**When auto-update is enabled:**
- Manifest is fetched periodically (e.g., hourly, daily)
- Reminders are created for detected changes
- Steps are **not** automatically installed (users must manually install)
- Users review reminders and decide to update or not

**Why not auto-install?**
- Users may want to review changes before installing
- New versions may have breaking changes
- Some steps may not be relevant to user's pipelines

### Scheduled Checks

When auto-update is enabled for a registry:

```go
func (rc *RegistryCache) ScheduledCheckLoop(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            registries, _ := rc.ListAutoUpdateEnabled(ctx)
            for _, registryURL := range registries {
                reminders, err := rc.CheckForUpdates(ctx, registryURL)
                if err != nil {
                    log.Warnf("Failed to check registry %s: %v", registryURL, err)
                    continue
                }
                rc.CreateReminders(ctx, reminders)
            }
        case <-ctx.Done():
            return
        }
    }
}
```

### Reminder Lifecycle

1. **Created** when change detected
2. **Displayed** to user via `dpkms system reminders list`
3. **Dismissed** by user via `dpkms system reminders dismiss <id>`
4. **Archived** (not deleted) for audit trail

**CLI Commands:**
```bash
# List active reminders
dpkms system reminders list

# List all reminders (including dismissed)
dpkms system reminders list --all

# Dismiss specific reminder
dpkms system reminders dismiss rem-001

# Clear all active reminders
dpkms system reminders clear --active
```

---

## Rationale

### Alternatives Considered

#### 1. Push Notifications (e.g., email, webhooks) (Rejected)

Reject because:
- Requires user email configuration
- Webhooks require external service endpoints
- Privacy concerns (sending user activity to external service)
- Overkill for simple update notifications
- Doesn't work offline

#### 2. In-App Toast/Banner Notifications (Rejected)

Reject because:
- dPKMS is primarily a CLI tool, not a GUI
- Users may not be actively running the CLI when updates available
- Difficult to prioritize multiple simultaneous updates
- No persistent record of past notifications

#### 3. Automatic Step Updates (Rejected)

Reject because:
- Breaking changes may break existing pipelines
- Users may not want automatic updates
- Difficult to rollback if update causes issues
- Violates principle of user control over their environment

#### 4. No Reminders, Just Manifest Cache (Rejected)

Reject because:
- Users have no way to know updates available
- Must manually inspect registry manifests (tedious)
- No prioritization of important updates (e.g., breaking changes)

### Benefits of Chosen Approach

- **User control:** Auto-update is manual by default, users must explicitly enable
- **Prioritization:** High priority for breaking changes, low for minor updates
- **Actionable:** Each reminder has clear action URL (what to do next)
- **Persistent:** Reminders stored in database, reviewed at user's convenience
- **Dismissible:** Users can dismiss reminders they've seen/acted on
- **Audit trail:** All reminders tracked (not deleted when dismissed)

---

## Consequences

### Positive

- Users discover relevant updates without manual checking
- Breaking changes (step removal) are clearly marked and prioritized
- Action URLs guide users to next steps
- Reminder system is extensible (can add other reminder types later)
- No external dependencies (all data in local database)

### Negative

- Requires database table for reminders (new schema)
- Reminders can accumulate (need cleanup or archival strategy)
- Users must actively check reminders (no push mechanism)
- Auto-update is manual, not automatic (users still must install steps)

### Neutral

- Reminders are per-user (not shared across users)
- Scheduled checks add background processing (small CPU overhead)
- ETag caching reduces unnecessary network requests

---

## Implementation Notes

### Cleanup Strategy

Reminders should be periodically cleaned up:
- Keep dismissed reminders for 30 days
- Delete older dismissed reminders
- Keep active reminders indefinitely (until dismissed)

### Step Usage Tracking

To identify when a step is "used by user" (for higher priority reminders):
```sql
-- Find steps used in user's pipelines
SELECT DISTINCT json_each(steps).value->>'type' as step_type
FROM pipelines
WHERE is_builtin = 0;
```

### Error Handling

- **Registry unavailable:** Log warning, don't fail entire check
- **Manifest parse error:** Return 500 with error details
- **ETag mismatch but no version change:** Compare step arrays directly
- **Network timeout:** Retry with exponential backoff

---

## References

- **US-0110** – Check Registry Updates (user story for update detection)
- **US-0111** – Configure Registry Auto-Update (user story for auto-update configuration)
- **ADR-004** – Step-Based Pipeline Architecture (defines step discovery and installation)
- **HTTP ETag Specification** – RFC 7232 (entity tags for change detection)

---
