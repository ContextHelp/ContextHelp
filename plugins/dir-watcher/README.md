# ctxt-plugin-dir-watcher

Reference ingestion plugin — polls a directory for new files and emits each
as a `KnowledgeObject` via the event bus.

Demonstrates: filesystem permission, polling-based file detection, extension filtering.

## Config

```yaml
plugins:
  dir-watcher:
    dir: ~/Documents/drop       # required — directory to watch
    interval: 10s               # poll interval (default 10s)
    extensions: [".txt", ".md"] # allowlist; omit for all files
    max_file_size_bytes: 1048576  # cap per file (default 1 MiB)
    delete_after_ingest: false  # move ingested files to .ctxt-ingested/
```

## Events emitted

| Type | When |
|------|------|
| `ctxt.plugin.dir-watcher.file` | New file detected in watched directory |
| `ctxt.plugin.dir-watcher.error` | Directory read or file read error |

## KnowledgeObject shape

- `type`: `file`, `subtype`: file extension (e.g. `md`, `txt`)
- `raw_content`: absolute file path
- `text_content`: file contents (up to `max_file_size_bytes`)
- `content_type`: MIME type (from extension or content sniffing)
- `metadata.filename`: base filename
- `metadata.path`: absolute path
- `metadata.size_bytes`: file size
- `metadata.modified`: last-modified timestamp (RFC3339)
- `content_hash`: SHA-256 of file content

## Build

```
CGO_ENABLED=1 go build -tags fts5 ./...
CGO_ENABLED=1 go test -tags fts5 ./...
```
