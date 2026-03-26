# ctxt-plugin-content-monitor

Reference monitoring plugin — fetches one or more URLs on a schedule,
detects content changes via SHA-256 hashing, and emits a `KnowledgeObject`
diff event when content changes are detected.

Demonstrates: scheduled network fetch, stateful change detection, diff generation.

## Config

```yaml
plugins:
  content-monitor:
    # Shorthand — URL is also used as label.
    urls:
      - https://example.com/pricing

    # Verbose — custom label per target.
    targets:
      - url: https://example.com/docs
        label: Docs Page

    interval: 30m          # poll interval (default 30m)
    user_agent: "my-monitor/1.0"
    timeout_seconds: 30
    max_body_bytes: 524288  # 512 KiB cap per URL
```

## Events emitted

| Type | When |
|------|------|
| `ctxt.plugin.content-monitor.seen` | First successful fetch — baseline recorded |
| `ctxt.plugin.content-monitor.change` | Content differs from previous fetch |
| `ctxt.plugin.content-monitor.error` | Fetch error or non-200 HTTP status |

## KnowledgeObject shape (change event)

- `type`: `url`, `subtype`: `content-monitor-change`
- `raw_content`: target URL
- `text_content`: new page content (up to `max_body_bytes`)
- `metadata.url`: target URL
- `metadata.label`: human label
- `metadata.change_summary`: human-readable diff summary
- `sections[0].title`: "Change Summary"
- `sections[1].title`: "Diff" (line-level +/- diff)
- `content_hash`: SHA-256 of new content

## Build

```
CGO_ENABLED=1 go build -tags fts5 ./...
CGO_ENABLED=1 go test -tags fts5 ./...
```
