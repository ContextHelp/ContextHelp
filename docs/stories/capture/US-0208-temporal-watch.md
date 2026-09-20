---
status: paper
---

# US-0208: Temporal Watch and Change Detection

**System Types:** ctxt, dpkms (self-hosted)
**Personas:** [Researchers & OSINT Analysts](../../personas/researchers-osint.md), [Operations](../../personas/operations.md)

---

## User Goal

As a researcher, I want to set up watches on URLs or entities that periodically re-capture content and alert me to changes so that I can track profile edits, new publications, deleted content, and other temporal signals.

---

## Context

The value of captured knowledge degrades when the underlying source changes without notice. A person updates their LinkedIn title, a company quietly edits a press release, a research paper's preprint is revised, or a social media post is deleted entirely. Without temporal monitoring, these changes go undetected, and the knowledge base silently drifts from reality. For OSINT researchers, detecting these changes is often the most critical signal -- a deleted post or an edited biography can reveal as much as the original content.

Temporal watches address this by periodically re-fetching monitored URLs or entity sources and comparing the new content against the most recent stored snapshot. The system computes a content hash for quick change detection, then generates a human-readable diff for significant changes. Each snapshot is stored as a versioned KnowledgeObject linked to the watch via temporal edges, creating a complete audit trail of how content has evolved over time.

Special handling is required for deleted content: when a previously accessible URL returns a 404 or 410 status, the system preserves the last known state and records a "deleted at" timestamp. This is particularly valuable for social media content that may be removed after public scrutiny. The alerting system supports configurable thresholds so researchers can tune the sensitivity -- ignoring minor formatting changes while being notified of substantive content edits.

---

## Implementation status

None of the criteria below are met. `watch` today means a **local
filesystem directory** (`WatchConfig{Path, Include, Exclude, DebounceMS,
Mode}` in `internal/storage/types.go`), driven by fsnotify with a poll
fallback. The shipped subcommands are `start|stop|status|enable|disable`
— there is no `watch add`, and `watch enable` accepts one watcher name:
`clipboard`. There is no URL/entity watch, no interval scheduler, no
snapshot diffing, and no change-threshold alerting.

The nearest shipped capability is `ctxt ingest --every <duration>`, a
foreground re-poll loop over an ingest adapter.

## Acceptance Criteria

- [ ] `ctxt watch add https://x.com/username --interval 24h` creates a watch that monitors a URL
- [ ] `ctxt watch add @entity.slug --interval 12h` creates a watch that monitors all sources for an entity
- [ ] System periodically re-fetches watched content at the configured interval
- [ ] Change detection compares new content against previous snapshot using content hash and text diff
- [ ] Alerts are generated for significant changes (above configurable threshold)
- [ ] Every snapshot is stored as a versioned KnowledgeObject linked via temporal edges
- [ ] `ctxt watch list` shows active watches with last-check time and change count
- [ ] `ctxt watch history <target>` shows timeline of captured changes
- [ ] `ctxt watch diff <target> --v1 <id1> --v2 <id2>` shows diff between any two snapshots
- [ ] Deleted content is detected (HTTP 404/410) and last known state is preserved with "deleted at" timestamp
- [ ] Watches can be paused, resumed, and deleted
- [ ] Watch scheduler respects rate limits per domain

---

## Implementation Notes

### CLI Interface

```bash
# Add a watch on a URL
ctxt watch add https://x.com/username --interval 24h
# -> Watch created: w-url-3f1a2b (checking every 24h)

# Add a watch on an entity (monitors all known source URLs)
ctxt watch add @person.jane-doe --interval 12h
# -> Watch created: w-ent-7c8d9e (checking 3 sources every 12h)

# Add a watch with custom alert threshold
ctxt watch add https://linkedin.com/in/jane-doe --interval 6h --threshold 0.05
# -> Watch created: w-url-2d3e4f (alerting on > 5% content change)

# List all active watches
ctxt watch list
# ->
# ID             Target                              Interval  Last Check           Changes  Status
# w-url-3f1a2b   https://x.com/username              24h       2026-02-17T08:00:00Z  7       active
# w-ent-7c8d9e   @person.jane-doe                    12h       2026-02-18T04:00:00Z  3       active
# w-url-2d3e4f   https://linkedin.com/in/jane-doe    6h        2026-02-18T10:00:00Z  1       active

# Show change history for a watched target
ctxt watch history https://x.com/username
# ->
# Snapshot          Captured At              Change Type       Summary
# snap-a1b2c3       2026-02-17T08:00:00Z     content_update    Bio text changed
# snap-d4e5f6       2026-02-15T08:00:00Z     new_content       New pinned post
# snap-g7h8i9       2026-02-10T08:00:00Z     content_deleted   Post removed (ID: 1234567890)

# Show diff between two specific snapshots
ctxt watch diff https://x.com/username --v1 snap-a1b2c3 --v2 snap-d4e5f6
# -> Text diff output showing changes between snapshots

# Pause a watch
ctxt watch pause w-url-3f1a2b
# -> Watch w-url-3f1a2b paused

# Resume a watch
ctxt watch resume w-url-3f1a2b
# -> Watch w-url-3f1a2b resumed (next check: 2026-02-18T16:00:00Z)

# Delete a watch (preserves captured snapshots)
ctxt watch delete w-url-3f1a2b
# -> Watch w-url-3f1a2b deleted (12 snapshots preserved)

# Force an immediate check
ctxt watch check w-url-3f1a2b --now
# -> Checking https://x.com/username... No changes detected.
```

### REST API

```
# Create a watch
POST /api/v1/watches
Content-Type: application/json

{
  "target_url": "https://x.com/username",
  "interval": "24h",
  "alert_threshold": 0.05
}

-> 201 Created
{
  "id": "w-url-3f1a2b",
  "target_url": "https://x.com/username",
  "interval": "24h",
  "status": "active",
  "created_at": "2026-02-18T10:30:00Z",
  "next_check": "2026-02-19T10:30:00Z"
}

# Create an entity watch
POST /api/v1/watches
Content-Type: application/json

{
  "target_entity": "@person.jane-doe",
  "interval": "12h"
}

-> 201 Created
{
  "id": "w-ent-7c8d9e",
  "target_entity": "@person.jane-doe",
  "monitored_urls": [
    "https://x.com/janedoe",
    "https://linkedin.com/in/jane-doe",
    "https://github.com/jdoe"
  ],
  "interval": "12h",
  "status": "active",
  "created_at": "2026-02-18T10:30:00Z"
}

# List watches
GET /api/v1/watches?status=active

-> 200 OK
{
  "watches": [...],
  "total": 3
}

# Get watch history
GET /api/v1/watches/{watch_id}/history

-> 200 OK
{
  "watch_id": "w-url-3f1a2b",
  "snapshots": [
    {
      "id": "snap-a1b2c3",
      "object_id": "o-snap-x1y2z3",
      "content_hash": "sha256:abc123...",
      "change_type": "content_update",
      "diff_summary": "Bio text changed: 'Senior Engineer' -> 'Staff Engineer'",
      "captured_at": "2026-02-17T08:00:00Z"
    },
    ...
  ]
}

# Get diff between snapshots
GET /api/v1/watches/{watch_id}/diff?v1=snap-a1b2c3&v2=snap-d4e5f6

-> 200 OK
{
  "v1": "snap-a1b2c3",
  "v2": "snap-d4e5f6",
  "diff": "--- v1 (2026-02-17)\n+++ v2 (2026-02-15)\n@@ -3,7 +3,7 @@\n-Staff Engineer at Acme Corp\n+Senior Engineer at Acme Corp",
  "change_percentage": 0.03,
  "sections_changed": ["bio"]
}

# Pause/resume/delete
PATCH /api/v1/watches/{watch_id}
Content-Type: application/json
{ "status": "paused" }

DELETE /api/v1/watches/{watch_id}

# Force immediate check
POST /api/v1/watches/{watch_id}/check
```

### Pipeline Steps

**watch.check** (scheduled pipeline):
```
CookieFetcher -> ContentHasher -> DiffGenerator -> AlertEvaluator -> SnapshotStorer
```

Each step implements the `PipelineStep` interface:

```go
type CookieFetcherStep struct {
    httpClient  *http.Client
    cookieStore CookieStore
}

func (s *CookieFetcherStep) Name() string { return "cookie_fetcher" }

func (s *CookieFetcherStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    targetURL := draft.Metadata["target_url"].(string)

    // Inject cookies if available for domain
    cookies, err := s.cookieStore.GetForDomain(ctx, extractDomain(targetURL))
    if err == nil && len(cookies) > 0 {
        s.httpClient.Jar.SetCookies(parseURL(targetURL), cookies)
    }

    resp, err := s.httpClient.Get(targetURL)
    if err != nil {
        return nil, fmt.Errorf("cookie_fetcher: request failed: %w", err)
    }
    defer resp.Body.Close()

    // Handle deleted content
    if resp.StatusCode == 404 || resp.StatusCode == 410 {
        draft.Metadata["content_deleted"] = true
        draft.Metadata["deleted_at"] = time.Now().UTC().Format(time.RFC3339)
        draft.Metadata["http_status"] = resp.StatusCode
        return draft, nil
    }

    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("cookie_fetcher: unexpected status %d", resp.StatusCode)
    }

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("cookie_fetcher: read body failed: %w", err)
    }

    draft.RawContent = body
    draft.Metadata["http_status"] = resp.StatusCode
    draft.Metadata["content_length"] = len(body)

    return draft, nil
}
```

```go
type ContentHasherStep struct{}

func (s *ContentHasherStep) Name() string { return "content_hasher" }

func (s *ContentHasherStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    if deleted, ok := draft.Metadata["content_deleted"].(bool); ok && deleted {
        draft.Metadata["content_hash"] = "deleted"
        return draft, nil
    }

    // Extract text content for hashing (ignore HTML structure changes)
    textContent := extractTextFromHTML(draft.RawContent)

    hash := sha256.Sum256([]byte(textContent))
    draft.Metadata["content_hash"] = fmt.Sprintf("sha256:%x", hash)
    draft.Metadata["text_content"] = textContent

    return draft, nil
}
```

```go
type DiffGeneratorStep struct {
    snapshotStore SnapshotStore
}

func (s *DiffGeneratorStep) Name() string { return "diff_generator" }

func (s *DiffGeneratorStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    watchID := draft.Metadata["watch_id"].(string)
    currentHash := draft.Metadata["content_hash"].(string)

    // Get previous snapshot
    prev, err := s.snapshotStore.GetLatest(ctx, watchID)
    if err != nil {
        // First snapshot; no diff needed
        draft.Metadata["is_first_snapshot"] = true
        draft.Metadata["has_changes"] = true
        return draft, nil
    }

    // Quick check: if hash unchanged, no diff needed
    if prev.ContentHash == currentHash {
        draft.Metadata["has_changes"] = false
        return draft, nil
    }

    // Generate text diff
    prevText := prev.TextContent
    currText := draft.Metadata["text_content"].(string)

    diff := generateUnifiedDiff(prevText, currText)
    changePercentage := calculateChangePercentage(prevText, currText)

    draft.Metadata["has_changes"] = true
    draft.Metadata["diff_from_previous"] = diff
    draft.Metadata["change_percentage"] = changePercentage
    draft.Metadata["previous_snapshot_id"] = prev.ID

    // Detect deletion
    if deleted, ok := draft.Metadata["content_deleted"].(bool); ok && deleted {
        draft.Metadata["change_type"] = "content_deleted"
    } else {
        draft.Metadata["change_type"] = "content_update"
    }

    return draft, nil
}
```

```go
type AlertEvaluatorStep struct {
    alertService AlertService
}

func (s *AlertEvaluatorStep) Name() string { return "alert_evaluator" }

func (s *AlertEvaluatorStep) Run(ctx context.Context,
    draft *KnowledgeObject) (*KnowledgeObject, error) {

    hasChanges := draft.Metadata["has_changes"].(bool)
    if !hasChanges {
        return draft, nil
    }

    threshold := draft.Metadata["alert_threshold"].(float64)
    changePercentage, _ := draft.Metadata["change_percentage"].(float64)

    // Always alert on deletion
    if changeType, ok := draft.Metadata["change_type"].(string); ok && changeType == "content_deleted" {
        s.alertService.Send(ctx, Alert{
            WatchID:  draft.Metadata["watch_id"].(string),
            Type:     "content_deleted",
            Summary:  "Monitored content has been deleted or removed",
            Severity: "high",
        })
        return draft, nil
    }

    // Alert if change exceeds threshold
    if changePercentage >= threshold {
        s.alertService.Send(ctx, Alert{
            WatchID:          draft.Metadata["watch_id"].(string),
            Type:             "content_update",
            Summary:          fmt.Sprintf("%.1f%% content change detected", changePercentage*100),
            ChangePercentage: changePercentage,
            Severity:         classifySeverity(changePercentage),
        })
    }

    return draft, nil
}
```

### Backend Processing

1. User creates a watch via CLI or REST API, specifying target URL or entity and interval
2. Watch record is created in the `watches` table with status `active`
3. Scheduler picks up active watches whose `last_check + interval < now()`
4. For entity watches, the scheduler resolves the entity to all known source URLs
5. For each URL, a `watch.check` job is enqueued
6. `CookieFetcher` fetches the URL content, injecting stored cookies for authenticated sources
7. `ContentHasher` extracts text content and computes SHA-256 hash
8. `DiffGenerator` compares against the latest snapshot; if hash is unchanged, marks `has_changes: false`
9. If changes detected, `DiffGenerator` produces a unified text diff and change percentage
10. `AlertEvaluator` checks change percentage against threshold and sends alert if exceeded
11. `SnapshotStorer` creates a new snapshot record and versioned KnowledgeObject
12. Watch record is updated with `last_check`, `last_change` (if applicable), and `change_count`
13. For deleted content (HTTP 404/410): last known state is preserved, "deleted at" timestamp recorded

### Database Schema

```sql
CREATE TABLE watches (
    id TEXT PRIMARY KEY,
    target_url TEXT,
    target_entity TEXT,
    interval TEXT NOT NULL DEFAULT '24h',
    alert_threshold REAL NOT NULL DEFAULT 0.05,
    status TEXT NOT NULL DEFAULT 'active',
    last_check TEXT,
    last_change TEXT,
    change_count INTEGER DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (target_url IS NOT NULL OR target_entity IS NOT NULL)
);

CREATE INDEX idx_watches_status ON watches(status);
CREATE INDEX idx_watches_next_check ON watches(status, last_check);

CREATE TABLE snapshots (
    id TEXT PRIMARY KEY,
    watch_id TEXT NOT NULL REFERENCES watches(id),
    object_id TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    text_content TEXT,
    diff_from_previous TEXT,
    change_type TEXT,
    change_percentage REAL,
    captured_at TEXT NOT NULL
);

CREATE INDEX idx_snapshots_watch ON snapshots(watch_id, captured_at DESC);
CREATE INDEX idx_snapshots_hash ON snapshots(content_hash);
```

### Configuration

```yaml
# In configuration.yaml
pipelines:
  watch.check:
    steps:
      - cookie_fetcher
      - content_hasher
      - diff_generator
      - alert_evaluator
      - snapshot_storer

watch:
  scheduler:
    checkInterval: 60s          # How often scheduler scans for due watches
    maxConcurrent: 10           # Max concurrent watch checks
    retryAttempts: 3            # Retries on transient failure
    retryBackoff: 30s           # Backoff between retries

  defaults:
    interval: 24h               # Default check interval
    alertThreshold: 0.05        # Default: alert on > 5% change
    maxSnapshots: 365           # Max snapshots per watch before archival

  rateLimits:
    "x.com": 5/min
    "linkedin.com": 2/min
    "github.com": 10/min
    default: 5/min

  alerts:
    enabled: true
    channels:
      - type: stdout             # Print to CLI output
      - type: webhook            # POST to URL
        url: ${ALERT_WEBHOOK_URL}
      - type: file               # Append to file
        path: /data/alerts.jsonl

  deleted:
    preserveLastState: true      # Keep last known content when URL goes 404
    retryDeletedAfter: 7d       # Re-check deleted URLs after 7 days
    maxDeleteRetries: 3          # Stop re-checking after N failed retries
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt watch add https://x.com/username --interval 24h` sends `target_url` and `interval: 24h` in the `POST /api/v1/watches` request payload; creates a watch and returns watch ID
- [ ] CLI: `ctxt watch add @entity.slug --interval 12h` sends `target_entity` and `interval: 12h` in the `POST /api/v1/watches` request payload; creates an entity watch monitoring all known sources
- [ ] CLI: `ctxt watch add <url> --interval 6h --threshold 0.05` sends `alert_threshold: 0.05` in the `POST /api/v1/watches` request payload
- [ ] CLI: `ctxt watch list` shows all active watches with correct metadata
- [ ] CLI: `ctxt watch history <target>` shows chronological snapshot timeline
- [ ] CLI: `ctxt watch diff <target> --v1 <id1> --v2 <id2>` sends `v1` and `v2` as query parameters in the `GET /api/v1/watches/{id}/diff` request and shows unified text diff
- [ ] CLI: `ctxt watch pause <id>` sends `status: paused` in the `PATCH /api/v1/watches/{id}` request payload; `ctxt watch resume <id>` sends `status: active`
- [ ] CLI: `ctxt watch delete <id>` deletes watch but preserves captured snapshots
- [ ] CLI: `ctxt watch check <id> --now` sends a `POST /api/v1/watches/{id}/check` request and triggers immediate check
- [ ] Scheduler: Active watches are checked at their configured interval
- [ ] Scheduler: Paused watches are not checked
- [ ] Scheduler: Concurrent watch checks respect `maxConcurrent` limit
- [ ] Detection: Unchanged content (same hash) does not create a new snapshot
- [ ] Detection: Changed content creates a new snapshot with diff and change percentage
- [ ] Detection: Content deletion (HTTP 404) is detected and last known state preserved
- [ ] Detection: Content deletion (HTTP 410) is detected and marked as permanently removed
- [ ] Diff: Text diff accurately reflects additions, removals, and modifications
- [ ] Diff: Change percentage calculation is accurate for small and large changes
- [ ] Alert: Changes exceeding threshold trigger alerts
- [ ] Alert: Changes below threshold do not trigger alerts
- [ ] Alert: Deleted content always triggers an alert regardless of threshold
- [ ] Snapshots: Each snapshot stored as versioned KnowledgeObject with temporal edges
- [ ] Snapshots: Snapshot count respects `maxSnapshots` limit with archival of oldest
- [ ] REST API: `POST /api/v1/watches` request payload contains `target_url` (or `target_entity`), `interval`, and `alert_threshold`; returns 201 with watch record including `id`, `status: active`, and `next_check`
- [ ] REST API: Watch record persisted in storage -- `GET /api/v1/watches` lists the created watch with correct `interval` and `alert_threshold`
- [ ] REST API: `GET /api/v1/watches` returns list of watches with filtering
- [ ] REST API: `GET /api/v1/watches/{id}/history` returns snapshot timeline with `content_hash`, `change_type`, and `diff_summary` per snapshot
- [ ] REST API: `GET /api/v1/watches/{id}/diff?v1=X&v2=Y` returns diff between snapshots with `diff` text and `change_percentage`
- [ ] REST API: `PATCH /api/v1/watches/{id}` updates watch status (pause/resume)
- [ ] REST API: `DELETE /api/v1/watches/{id}` deletes watch
- [ ] Rate Limiting: Watch checks respect per-domain rate limits
- [ ] Resilience: Transient network failure retries up to 3 times with backoff
- [ ] Resilience: Permanent failure marks watch as `error` with diagnostic message

---

## Related Stories

- [US-0206](./US-0206-osint-entity-aggregation.md) -- OSINT entity aggregation (watches feed entity profiles)
- [US-0209](./US-0209-authenticated-web-fetch.md) -- Authenticated fetch (watches may need cookies)
- [US-0210](./US-0210-cross-platform-entity-resolution.md) -- Entity resolution for entity-level watches
- [US-0054](../search/US-0054-saved-search-and-alerts.md) -- Saved search and alerts (related alerting pattern)
- [US-0002](../ingestion/US-0002-url-capture-and-extraction.md) -- URL capture (base fetch functionality)
- [US-0047](../enrichment/US-0047-extract-temporal-information.md) -- Temporal information extraction

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- Solo Developer
- Automation Builder

---

## E2E Tests

- `test/integration/us0208_temporal_watch_test.go::TestUS0208_InitialSnapshotIngested`
- `test/integration/us0208_temporal_watch_test.go::TestUS0208_ChangeDetectedOnNewVersion`
- `test/integration/us0208_temporal_watch_test.go::TestUS0208_NoChangeWhenContentIdentical`
