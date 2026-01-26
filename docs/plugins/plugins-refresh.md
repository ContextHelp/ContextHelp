# Refresh Plugin (Auto-Update & Auto-Fetch Plugin)

This document defines the **Refresh Plugin**, an optional ingestion-extension module that automatically refreshes **existing bookmarks** *and* proactively **fetches new content** from sources that publish updates over time (e.g., podcasts, YouTube channels, newsletters, RSS/Atom feeds, GitHub activity, API endpoints).

The plugin provides scheduled refresh and fetch operations using configurable rules, accessible via CLI, REST, and gRPC. It ensures users always have current context without manual re-ingestion.

---

## Overview

The Refresh Plugin enables:

- **Auto-refreshing** existing knowledge objects (documentation, repos, API specs)
- **Auto-fetching** new items from content-producing sources:
  - Podcast feeds
  - YouTube channels & playlists
  - Newsletter archives
  - RSS/Atom feeds
  - GitHub repos (releases, activity)
  - Documentation changelogs
- **Regex-based rules** defining *what* gets refreshed or fetched
- **User-defined refresh intervals** with minimum enforced thresholds
- **Integration with ingestion pipelines**
- **Non-destructive semantic updating** (mentions, entities, tags)
- **Incremental graph updates**

This plugin ensures data never goes stale and new material is ingested as soon as it appears.

---

## Motivation

Certain sources continually publish new content:

- A podcast publishes weekly.
- A YouTube channel uploads daily.
- A newsletter arrives twice a week.
- An API spec changes frequently.
- A repo issues new releases or commits.

Users should not be responsible for re-ingesting content manually.
ContextHelp, with the Refresh Plugin, becomes an **always-fresh context engine**, powering stable, up-to-date knowledge for agents.

---

## Features

- Scheduled refresh of existing knowledge objects
- Scheduled fetching of new items from feed-like sources
- Regex-based source matching rules
- Interval-based scheduling (`--refresh daily`, `--refresh 3600`)
- Minimum refresh interval (default: 5 minutes)
- Backoff and retry handling
- Full CLI, REST, gRPC integration
- Knowledge object-level override policies
- First-class handling of semantic updates:
  - mentions
  - entity resolution
  - graph edges

---

## Configuration Model

### Global Configuration

```
refresh:
  enabled: true
  min_interval_seconds: 300
  default_interval_seconds: 86400
  rules:
    - match: "https://github.com/.+"
      interval_seconds: 43200
      fetch_new: false

    - match: "https://www.youtube.com/channel/.+"
      interval_seconds: 3600
      fetch_new: true
      fetch_pipeline: "url.generic"

    - match: "https://feeds[.]megaphone[.]fm/.+"
      interval_seconds: 7200
      fetch_new: true
      fetch_pipeline: "audio.transcript"

    - match: "https://newsletter[.]example[.]com/archive"
      interval_seconds: 86400
      fetch_new: true
```

### Knowledge Object-Level Overrides

```
"refresh": {
  "enabled": true,
  "interval_seconds": 3600,
  "fetch_new": true
}
```

### CLI Flags

- `--refresh daily`
- `--refresh 3600`
- `--refresh 0` (disable)
- `--refresh auto`
- `--fetch-new` (force-enable)
- `--no-fetch-new`

### REST Example

```
POST /analyze
{
  "url": "https://www.youtube.com/channel/xyz",
  "refresh": "3600",
  "fetch_new": true
}
```

### gRPC Example

```
RefreshPolicy {
  bool fetch_new;
  uint32 interval_seconds;
}
```

---

## Auto-Fetching New Content

When refresh rules specify `fetch_new: true`, the plugin attempts to detect new items from the source.

Supported by built-in detection for:

- RSS/Atom feeds
- YouTube channel & playlist APIs
- Podcast feeds (RSS-based)
- Newsletter archives (index pages or feeds)
- GitHub releases / commits / tags
- Any endpoint that returns a machine-readable list of items

### Fetching Algorithm

1. Fetch feed or listing endpoint.
2. Extract item identifiers (URL, GUID, videoId, episodeId, release tag).
3. Compare against existing bookmarks.
4. For any new items:
   - Create a **new ingestion job** using the appropriate pipeline.
5. Update `last_fetch_timestamp`.

Example: YouTube channel
- Poll channel playlist feed
- Detect new video URLs
- Enqueue ingestion via `url.generic`

Example: Podcast
- Poll RSS feed
- Detect new episodes
- Use `audio.transcript` pipeline or fallback to URL pipeline

---

## Refresh Algorithm (Existing Bookmarks)

1. Check `next_refresh_timestamp`
2. Skip if within `min_interval_seconds`
3. Re-ingest using original pipeline
4. Update:
   - content
   - mentions
   - entity resolution
   - graph edges
5. Increment `refresh_count`
6. Reset timestamps

---

## Error Handling

- Network/timeouts handled with retries
- Incrementing `refresh_error_count`
- Backoff applied for failing sources
- Auto-disable after a configured number of failures
- Errors logged in refresh job history

---

## Storage Extensions

Bookmarks include:

```
"refresh": {
  "enabled": true,
  "interval_seconds": 86400,
  "fetch_new": true,
  "last_refresh_timestamp": 1710000000,
  "next_refresh_timestamp": 1710086400,
  "error_count": 0
}
```

New tables (if needed):

- `refresh_state`
- `fetch_history`

Indexes:

- `next_refresh_timestamp`
- `source_url` (for fetch deduplication)

---

## Pipeline Integration

The plugin hooks into ingestion:

### Pre-Ingest

- Parse refresh policies
- Attach rules to bookmark

### Post-Ingest

- Initialize refresh timers
- Register source for fetch checks if `fetch_new` is enabled

### Refresh Jobs

- Preserve original pipeline
- Recompute mentions
- Re-run entity resolution
- Update graph edges

### Fetch Jobs

- Produce new bookmarks
- Inherit refresh rules from parent source

---

## API Integration

### CLI Examples

```
ch analyze https://github.com/user/repo --refresh daily
ch analyze https://youtube.com/channel/xyz --refresh=3600 --fetch-new
ch refresh run
ch refresh pending
ch refresh stats
```

### REST Examples

```
GET /refresh/pending
POST /refresh/run
GET /fetch/sources
```

### gRPC Examples

```
rpc RunRefresh(RefreshRequest) returns (RefreshResponse);
rpc ListFetchSources(FetchListRequest) returns (FetchListResponse);
```

---

## Security & Safety

- External network access allowed only if refresh is enabled
- Feed parsing rejects unsafe content
- Fetching from arbitrary URLs must respect domain allowlist if configured
- No registry mutation allowed
- Non-destructive updates (old content preserved in history if configured)

---

## Performance Notes

- Heavy refresh schedules should warn users
- Fetch operations must use incremental cursors when possible
- Backlink updates performed in batches
- WAL tuning recommended for frequent fetch jobs

---

## Use Cases

### 1. Podcast Feeds

```
match: "https://podcast.example.com/feed"
fetch_new: true
interval_seconds: 3600
fetch_pipeline: "audio.transcript"
```

### 2. YouTube Channels

```
match: "youtube.com/channel/.+"
fetch_new: true
fetch_pipeline: "url.generic"
```

### 3. Newsletters

```
match: "newsletter.example.com/archive"
fetch_new: true
interval_seconds: 86400
```

### 4. GitHub Activity

```
match: "github.com/.+"
fetch_new: true
interval_seconds: 43200
```

New releases become new bookmarks.

### 5. Documentation Pages

```
match: "docs.example.com/.+"
interval_seconds: 7200
fetch_new: false
```

Refresh only, no new content expected.

---

## Future Extensions

- Diff-based semantic updates
- Per-source adaptive refresh intervals
- Machine-learned freshness prediction
- Webhook-driven refresh mode
- Plugin hook system: `onFetch`, `onRefresh`, `onDelta`
- Feed schema inference (auto-detection of RSS/Atom/JSONFeed)

---

## Summary

The Refresh Plugin extends ContextHelp into a **proactive**, **always-fresh** knowledge engine:

- Automatically refreshes existing bookmarks
- Automatically fetches new content from evolving sources
- Ensures mentions, entities, and graph edges reflect latest changes
- Works across CLI, REST, gRPC
- Fully configurable and extensible

This plugin is essential for keeping personal and agent context aligned with fast-changing information ecosystems such as YouTube, podcasts, feeds, GitHub, news, and documentation.