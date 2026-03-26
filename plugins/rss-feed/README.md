# ctxt-plugin-rss-feed

Reference ingestion plugin — polls RSS 2.0 / Atom 1.0 feeds on a schedule
and emits each new item as a `KnowledgeObject` via the event bus.

Demonstrates: scheduled ingestion, network permission, deduplication.

## Config

```yaml
plugins:
  rss-feed:
    feeds:
      - https://example.com/feed.xml
      - https://news.ycombinator.com/rss
    interval: 15m          # poll interval (default 15m)
    max_items: 50          # cap per feed per cycle (0 = unlimited)
    user_agent: "my-reader/1.0"
    timeout_seconds: 30
```

## Events emitted

| Type | When |
|------|------|
| `ctxt.plugin.rss-feed.item` | New item detected (not previously seen this session) |
| `ctxt.plugin.rss-feed.error` | Fetch or parse error for a feed URL |

## KnowledgeObject shape

- `type`: `url`, `subtype`: `rss-item`
- `raw_content`: item link URL
- `text_content`: title + body/summary joined
- `metadata.feed_url`: source feed URL
- `metadata.guid`: item GUID
- `metadata.published`: RFC3339 publish date
- `metadata.author`: author name (when present)
- `content_hash`: SHA-256 of text content

## Build

```
CGO_ENABLED=1 go build -tags fts5 ./...
CGO_ENABLED=1 go test -tags fts5 ./...
```
