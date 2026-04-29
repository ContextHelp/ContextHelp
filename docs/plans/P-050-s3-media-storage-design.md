# S3-Compatible Blob Storage for Media Files

**Date:** 2026-02-18
**Status:** Approved

## Summary

Add a pluggable blob storage layer to dPKMS/ctxt for externalizing content that exceeds a configurable size threshold. Default backend is local filesystem (preserving local-first philosophy); S3-compatible backend is available for cloud deployments. A pipeline step handles the externalization decision transparently during ingestion.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Scope | Threshold-based (any content > N bytes) | Universal, type-agnostic |
| S3 compatibility | Generic (AWS, MinIO, R2, B2, DO Spaces, Garage) | Aligns with pluggable philosophy |
| Default backend | Local filesystem | Local-first; S3 is opt-in |
| Content handling | Replace RawContent with `blob://{hash}` | Keeps SQLite lean |
| Architecture | BlobStore sub-store + pipeline step (hybrid) | Clean abstraction + pipeline composability |
| Blob key | Content hash (SHA-256) | Natural dedup, consistent with existing ContentHash |
| Delete strategy | Reference-counted via ContentHash lookup | Prevents orphaned blobs |
| Text search | New TextContent field for extracted text | FTS5 indexes extracted text from binary blobs |
| Retry | Configurable retry with backoff for S3 | AWS SDK v2 built-in; local backend needs none |

## BlobStore Interface

```go
// Added to internal/storage/storage.go

type BlobStore interface {
    Put(ctx context.Context, key string, data io.Reader, meta BlobMeta) error
    Get(ctx context.Context, key string) (io.ReadCloser, BlobMeta, error)
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
    List(ctx context.Context, prefix string) ([]BlobInfo, error)
    URL(ctx context.Context, key string) (string, error)
}

type BlobMeta struct {
    ContentType string            // MIME type
    Size        int64             // bytes
    ContentHash string            // SHA-256
    Filename    string            // original filename if known
    Properties  map[string]string // extensible: dimensions, duration, etc.
}

type BlobInfo struct {
    Key       string
    Size      int64
    UpdatedAt time.Time
}
```

`StorageDriver` gains a `Blobs() BlobStore` method alongside existing sub-stores.

## Backend Implementations

### Local Filesystem (`internal/storage/blob/local/`)

- Path: `{data_dir}/blobs/{hash[0:2]}/{hash[2:4]}/{hash}`
- 2-level directory sharding prevents huge flat directories
- Metadata in sidecar `.meta.json` files
- `URL()` returns `file://{absolute_path}`
- `List()` walks directory tree

### S3-Compatible (`internal/storage/blob/s3/`)

- Uses AWS SDK v2 with custom endpoint support
- Key path: `{prefix}/{hash[0:2]}/{hash[2:4]}/{hash}`
- Metadata stored as S3 object metadata headers + `.meta.json` sidecar object
- `URL()` returns presigned GET URL (configurable expiry)
- `List()` uses `ListObjectsV2`
- Configurable retry with backoff (default 3 retries, SDK built-in)

### Stub (`internal/storage/blob/stub/`)

- No-op implementation for testing
- `Put()` succeeds silently, `Get()` returns not found
- Follows existing providers stub pattern

### Garage preset (`backend: garage`)

[Garage](https://garagehq.deuxfleurs.fr) is a Rust-built, distributed,
self-hosted S3-compatible object store. It is fully SigV4-compatible
and reuses the `s3` store implementation; the `garage` backend is a
thin preset on the factory that:

- Forces `use_path_style: true` (Garage's recommended addressing).
- Defaults `region: "garage"` (matches the upstream quick-start).
- Requires `s3.endpoint` explicitly (no sensible default).

Validated end-to-end by
[`test/integration/blob_garage_test.go`](../../test/integration/blob_garage_test.go),
which can auto-launch a single-node Garage container, init the
cluster layout, create a bucket and access key, and exercise the full
`BlobStore` round-trip.

### Factory (`internal/storage/blob/factory.go`)

```go
func New(cfg config.BlobConfig) (storage.BlobStore, error)
// cfg.Backend: "local" (default) | "s3" | "garage" | "stub"
```

## Configuration

```yaml
storage:
  type: "sqlite"
  path: "~/.local/share/contexthelp/db.sqlite"
  blob:
    backend: "local"              # local | s3 | garage | stub
    threshold: 65536              # bytes (64KB). 0 = never externalize
    local:
      path: "~/.local/share/contexthelp/blobs"
    s3:
      endpoint: ""                # custom endpoint for MinIO/R2/B2/etc.
      region: "us-east-1"
      bucket: ""
      prefix: ""                  # key prefix within bucket
      access_key: ""              # or AWS_ACCESS_KEY_ID env var
      secret_key: ""              # or AWS_SECRET_ACCESS_KEY env var
      use_path_style: false       # true for MinIO
      presign_expiry: "1h"
      max_retries: 3
```

Environment variable overrides:
- `CTXT_BLOB_BACKEND`, `CTXT_BLOB_THRESHOLD`
- `CTXT_BLOB_S3_ENDPOINT`, `CTXT_BLOB_S3_BUCKET`, `CTXT_BLOB_S3_REGION`, etc.
- AWS standard env vars (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`) also respected

## Pipeline Step: `externalize-content`

A built-in pipeline step that conditionally externalizes content to the blob store.

**When it runs:** After content extraction, before enrichment steps.

**Behavior:**

1. Check `len(obj.RawContent)` against configured threshold
2. If under threshold: pass through unchanged
3. If over threshold:
   - Compute content hash via `storageutil.ContentHash`
   - `BlobStore.Put(hash, content, meta)` (idempotent — deduped by hash key)
   - Replace `obj.RawContent` with `blob://{hash}`
   - Set `obj.Metadata["blob_key"]` = hash
   - Set `obj.Metadata["blob_original_size"]` = original byte count
   - Set `obj.Metadata["blob_content_type"]` = original content type

**Contract:** Declares capability `blob-externalize`. Requires `BlobStore` availability.

## Schema Changes

### KnowledgeObject

New field on `KnowledgeObject`:

- **`TextContent string`** — Searchable text extracted from binary blobs (PDF text, OCR output, audio transcription). When `RawContent` is a `blob://` reference, this field holds the extracted text. Empty for inline text objects.

### SQLite

- Add `text_content TEXT DEFAULT ''` column to `objects` table
- Update FTS5 virtual table to index `text_content` alongside `summaries` and `raw_content`

### RawContent Semantics

`RawContent` can now contain:
- Inline content (unchanged behavior for content under threshold)
- `blob://{hash}` reference (content lives in blob store)

## Blob Resolution

```go
// internal/storage/blob/resolve.go
func Resolve(ctx context.Context, store BlobStore, obj *KnowledgeObject) (io.ReadCloser, error)
```

Helper that checks if `RawContent` starts with `blob://` and fetches from the blob store. Returns the inline content as a reader if not externalized.

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Blob store unavailable on read | Return object with `blob://` reference intact. Callers show "content unavailable" or retry. Don't fail the object read. |
| Put failure during pipeline | Fail the pipeline step. No silent fallback to inline (would mask storage issues). User retries ingestion. |
| Content-hash collision | Astronomically unlikely with SHA-256. `Put` with existing key is a no-op (idempotent). |
| Threshold boundary | Content exactly at threshold stays inline ("above", not "at or above"). |
| S3 transient failures | AWS SDK v2 retry with configurable backoff (default 3 retries). |
| Delete with shared hash | Reference count via `GetByContentHash`. Only delete blob when no objects reference it. |

## File Layout

```
internal/storage/
├── storage.go              # Add BlobStore, BlobMeta, BlobInfo interfaces
├── types.go                # Add TextContent field to KnowledgeObject
├── blob/
│   ├── factory.go          # New(cfg) → BlobStore
│   ├── resolve.go          # Resolve() helper for blob:// references
│   ├── local/
│   │   └── store.go        # Local filesystem implementation
│   ├── s3/
│   │   └── store.go        # S3-compatible implementation
│   └── stub/
│       └── store.go        # No-op for testing
├── sqlite/
│   ├── driver.go           # Add Blobs() method
│   ├── migrations.go       # Add text_content column, update FTS
│   └── objects.go          # Handle TextContent in CRUD
internal/pipeline/builtins/
└── externalize.go          # externalize-content pipeline step
internal/config/
└── config.go               # Add BlobConfig struct
```

## Testing Strategy

- **Unit tests:** Each backend (local, s3, stub) tested against the BlobStore interface
- **S3 tests:** Use MinIO in Docker for integration tests, or mock the AWS SDK
- **Pipeline tests:** Verify externalization threshold logic, blob:// reference format, metadata population
- **Round-trip tests:** Ingest → externalize → read → resolve → verify content matches original
- **Delete tests:** Reference counting, orphan prevention

## Future Considerations (Not In Scope)

- **Blob GC job:** Periodic scan using `List()` cross-referenced against objects. Handles orphans from crashes.
- **Streaming ingestion:** For very large files, stream directly to blob store without loading into memory.
- **CDN integration:** Serve blob URLs through a CDN for public-facing deployments.
- **Encryption at rest:** Client-side encryption before blob storage.
