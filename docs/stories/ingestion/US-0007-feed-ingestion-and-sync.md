# US-0007: Feed Ingestion and Sync

**System Types:** ctxt, dpkms (self-hosted)
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Operations](../../personas/operations.md)

---

## User Goal

As a knowledge worker, I want to subscribe to RSS, Atom, and JSON feeds and have new items automatically ingested into my knowledge base so that I stay current without manual effort.

---

## Context

Knowledge workers follow dozens of blogs, newsletters, research feeds, and industry publications. Manually copying links into the system every time a new article appears is unsustainable. Feed subscriptions solve this by turning ingestion into a background process: subscribe once, and every new item flows into the knowledge base automatically.

Feed ingestion is a first-class subsystem, not a one-shot import. It maintains persistent subscriptions with scheduled polling, conditional HTTP fetching (ETag/Last-Modified) to respect upstream bandwidth, and per-item deduplication to prevent re-ingestion. Each feed item is enqueued as its own ingestion job, reusing existing pipelines (`url.article`, `text.long`) for extraction and enrichment.

Operations teams need visibility into feed health: which feeds are active, when they last synced, whether a feed has gone stale or returned errors. The system must handle real-world feed problems gracefully: temporarily unreachable URLs, malformed XML/JSON, feeds that disappear (410 Gone), and rate limiting (429 Too Many Requests).

---

## Acceptance Criteria

- [ ] User can subscribe to an RSS 2.0, Atom 1.0, or JSON Feed 1.1 URL via CLI or REST API
- [ ] User can list all active feed subscriptions with last-sync timestamps
- [ ] User can manually trigger a sync for all feeds or a single feed
- [ ] User can unsubscribe from a feed, stopping future syncs
- [ ] Scheduled sync runs at a configurable interval (default: 1 hour) for all active feeds
- [ ] Sync uses conditional GET (ETag/Last-Modified) to avoid re-fetching unchanged feeds
- [ ] Each new feed item is enqueued as a separate ingestion job with its own KnowledgeObject
- [ ] Items are deduplicated by GUID or link to prevent re-ingestion across syncs
- [ ] Feed errors (unreachable, malformed, 410 Gone, 429) are logged and surfaced in feed status
- [ ] Feed metadata (title, description, site URL) is extracted and stored on first subscribe

---

## Implementation Notes

### CLI Interface
```bash
# Subscribe to a feed
ctxt feed add https://blog.example.com/rss
# → {
# →   "feed_id": "f-abc123",
# →   "url": "https://blog.example.com/rss",
# →   "title": "Example Blog",
# →   "format": "rss2.0",
# →   "status": "active",
# →   "sync_interval": "1h"
# → }

# Subscribe with a custom sync interval
ctxt feed add https://research.example.org/atom.xml --interval 30m

# List all subscriptions
ctxt feed list
# → FEED ID    URL                                  TITLE          LAST SYNC              STATUS
# → f-abc123   https://blog.example.com/rss         Example Blog   2026-02-18T09:00:00Z   active
# → f-def456   https://research.example.org/atom.xml Research Feed  2026-02-18T09:15:00Z   active
# → f-ghi789   https://dead.example.com/feed.json   Dead Feed      2026-02-17T12:00:00Z   error

# Manual sync of all feeds
ctxt feed sync
# → Syncing 3 feeds...
# → f-abc123: 2 new items enqueued
# → f-def456: 0 new items (unchanged)
# → f-ghi789: error — connection refused
# → Sync complete: 2 new items, 1 error

# Manual sync of a single feed
ctxt feed sync --url https://blog.example.com/rss
# → f-abc123: 2 new items enqueued

# Unsubscribe
ctxt feed remove https://blog.example.com/rss
# → Feed f-abc123 removed. No further syncs will occur.
# → Existing items from this feed are retained in the knowledge base.
```

### REST API
```
POST /feeds
Content-Type: application/json

{
  "url": "https://blog.example.com/rss",
  "sync_interval": "1h"
}

--> 201 Created
{
  "feed_id": "f-abc123",
  "url": "https://blog.example.com/rss",
  "title": "Example Blog",
  "format": "rss2.0",
  "status": "active",
  "sync_interval": "1h",
  "created_at": "2026-02-18T10:00:00Z"
}
```

```
GET /feeds

--> 200 OK
{
  "feeds": [
    {
      "feed_id": "f-abc123",
      "url": "https://blog.example.com/rss",
      "title": "Example Blog",
      "format": "rss2.0",
      "status": "active",
      "sync_interval": "1h",
      "last_sync": "2026-02-18T09:00:00Z",
      "items_total": 47,
      "items_last_sync": 2
    }
  ],
  "total": 1
}
```

```
POST /feeds/f-abc123/sync

--> 202 Accepted
{
  "feed_id": "f-abc123",
  "sync_job_id": "j-sync-001",
  "status": "syncing"
}
```

```
DELETE /feeds/f-abc123

--> 204 No Content
```

### Pipeline Steps

The `feed.sync` pipeline runs at the subscription level (periodic polling). Individual feed items are dispatched to content pipelines:

- **feed.sync** --> FeedFetcher --> FeedParser --> ItemDeduplicator --> ItemEnqueuer
- Each new item enqueued to **url.article** (if item has a link) or **text.long** (if item is content-only)

```go
// FeedFetcher performs conditional GET against the feed URL.
type FeedFetcher struct{}

func (s *FeedFetcher) Name() string { return "feed_fetcher" }

func (s *FeedFetcher) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	feedURL := draft.Source
	etag := draft.Metadata["etag"].(string)
	lastModified := draft.Metadata["last_modified"].(string)

	req, _ := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("feed fetch failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		draft.Metadata["sync_result"] = "not_modified"
		return draft, nil
	case http.StatusGone:
		draft.Metadata["sync_result"] = "gone"
		return draft, fmt.Errorf("feed removed (410 Gone)")
	case http.StatusTooManyRequests:
		retryAfter := resp.Header.Get("Retry-After")
		draft.Metadata["retry_after"] = retryAfter
		return draft, fmt.Errorf("rate limited (429), retry after %s", retryAfter)
	case http.StatusOK:
		body, _ := io.ReadAll(resp.Body)
		draft.RawContent = string(body)
		draft.Metadata["etag"] = resp.Header.Get("ETag")
		draft.Metadata["last_modified"] = resp.Header.Get("Last-Modified")
		return draft, nil
	default:
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
}
```

```go
// FeedParser detects format (RSS 2.0, Atom 1.0, JSON Feed 1.1) and extracts items.
type FeedParser struct{}

func (s *FeedParser) Name() string { return "feed_parser" }

func (s *FeedParser) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	format := detectFeedFormat(draft.RawContent)
	draft.Metadata["feed_format"] = format

	var items []FeedItem
	switch format {
	case "rss2.0":
		items, _ = parseRSS(draft.RawContent)
	case "atom1.0":
		items, _ = parseAtom(draft.RawContent)
	case "json1.1":
		items, _ = parseJSONFeed(draft.RawContent)
	default:
		return nil, fmt.Errorf("unsupported feed format")
	}

	draft.Metadata["parsed_items"] = items
	return draft, nil
}
```

### Backend Processing

1. **Subscribe:** User provides feed URL. System fetches the feed, detects format, extracts feed-level metadata (title, description, site URL), and creates a row in the `feeds` table with status `active`.
2. **Scheduled sync:** A background scheduler triggers `feed.sync` for all active feeds at each feed's configured interval (default: 1 hour). Each sync creates a transient job.
3. **Conditional fetch:** FeedFetcher sends a GET request with `If-None-Match` (ETag) and `If-Modified-Since` headers. If the server returns 304 Not Modified, the sync completes with zero new items.
4. **Parse:** FeedParser detects the format and extracts a list of items. Each item has a GUID (or link as fallback), title, content/summary, published date, and author.
5. **Deduplicate:** ItemDeduplicator checks each item's GUID against a `feed_items` table. Only items not previously seen are passed through.
6. **Enqueue:** ItemEnqueuer creates one ingestion job per new item. The job's pipeline is `url.article` if the item has a link, or `text.long` if it contains inline content only. The KnowledgeObject for each item has `Type="feed_item"`, `Source=<item URL>`, and Metadata including `feed_url`, `feed_id`, `published_at`, and `author`.
7. **Update feed state:** The `feeds` row is updated with the new `etag`, `last_modified`, `last_sync` timestamp, and item count.
8. **Error handling:** If the feed URL is unreachable, the feed status is set to `error` with the reason. After a configurable number of consecutive failures (default: 5), the feed is automatically set to `suspended`. A 410 Gone response permanently marks the feed as `gone`.

### Database Schema

```sql
CREATE TABLE feeds (
    id          TEXT PRIMARY KEY,
    url         TEXT NOT NULL UNIQUE,
    title       TEXT,
    description TEXT,
    site_url    TEXT,
    format      TEXT,           -- 'rss2.0', 'atom1.0', 'json1.1'
    status      TEXT NOT NULL DEFAULT 'active',  -- 'active', 'paused', 'error', 'suspended', 'gone'
    sync_interval TEXT NOT NULL DEFAULT '1h',
    etag        TEXT,
    last_modified TEXT,
    last_sync   TIMESTAMP,
    error_count INTEGER NOT NULL DEFAULT 0,
    last_error  TEXT,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE feed_items (
    id          TEXT PRIMARY KEY,
    feed_id     TEXT NOT NULL REFERENCES feeds(id),
    guid        TEXT NOT NULL,
    link        TEXT,
    object_id   TEXT,           -- references the created KnowledgeObject
    ingested_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(feed_id, guid)
);
```

### Configuration
```yaml
# In configuration.yaml
feeds:
  enabled: true
  defaultSyncInterval: 1h       # How often to poll feeds
  maxItemsPerSync: 100           # Cap items per single sync to avoid floods
  retentionPolicy: all           # 'all' or integer (keep last N items)
  maxConsecutiveErrors: 5        # Suspend feed after this many failures
  requestTimeout: 30s            # Timeout for fetching a single feed
  userAgent: "ctxt/1.0 (+https://github.com/ideacrafterslabs/ctxt)"

  # Per-feed overrides are set via CLI flags or REST API
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt feed add <rss_url>` creates subscription and returns feed metadata
- [ ] CLI: `ctxt feed add <atom_url>` correctly detects Atom 1.0 format
- [ ] CLI: `ctxt feed add <json_feed_url>` correctly detects JSON Feed 1.1 format
- [ ] CLI: `ctxt feed list` displays all subscriptions with correct status and last-sync time
- [ ] CLI: `ctxt feed sync` triggers sync for all active feeds and reports new item counts
- [ ] CLI: `ctxt feed sync --url <url>` syncs only the specified feed
- [ ] CLI: `ctxt feed remove <url>` removes subscription; existing items remain searchable
- [ ] Sync: Conditional GET with ETag/Last-Modified avoids re-downloading unchanged feeds
- [ ] Dedup: Items already ingested (by GUID) are not re-enqueued on subsequent syncs
- [ ] Fanout: Each new feed item creates a separate ingestion job with correct pipeline
- [ ] KnowledgeObject: Created objects have Type="feed_item", correct Source, and feed metadata
- [ ] Error: Unreachable feed URL sets feed status to "error" with descriptive message
- [ ] Error: Feed returning 410 Gone permanently marks feed as "gone"
- [ ] Error: Feed returning 429 respects Retry-After header and delays next sync
- [ ] REST API: `POST /feeds` with valid URL returns 201 with feed details
- [ ] REST API: `GET /feeds` returns list of all subscriptions
- [ ] REST API: `POST /feeds/{id}/sync` triggers sync and returns 202 with sync job ID
- [ ] REST API: `DELETE /feeds/{id}` removes subscription and returns 204

---

## Related Stories

- [US-0001: Text Capture with Minimal Friction](./US-0001-text-capture-minimal-friction.md) -- Base capture functionality reused by feed item ingestion
- [US-0002: URL Capture and Extraction](./US-0002-url-capture-and-extraction.md) -- Feed items with links are dispatched to the `url.article` pipeline
- [US-0008: Batch Import from File](./US-0008-batch-import-from-file.md) -- OPML import creates feed subscriptions in bulk
- [US-0009: Extract Entities and Mentions](../enrichment/US-0009-extract-entities-and-mentions.md) -- Entities extracted from feed item content
- [US-0012: Generate Summaries and Sections](../enrichment/US-0012-generate-summaries-and-sections.md) -- Feed items are summarized during enrichment
