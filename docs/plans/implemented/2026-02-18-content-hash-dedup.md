# Content-Hash Dedup + Reinforcement Counting

## Status: Implemented

## Overview

Content-based deduplication with reinforcement tracking. Prevents duplicate knowledge objects while measuring content importance through frequency of encounter.

## Design Decisions

| Decision | Choice |
|----------|--------|
| Hash algorithm | SHA-256 |
| Hash scope | `RawContent` + `Source` (null-byte separated) |
| Normalization | Trim, lowercase, collapse whitespace |
| Uniqueness | Partial unique index (`WHERE content_hash != ''`) |
| Reinforcement triggers | Re-ingestion only |
| Merge behavior | Append/merge new Tags and Mentions (dedup by label/string) |
| Merge conflict | First-seen wins (existing tag/mention preserved) |
| Atomicity | `Reinforce` wrapped in `BeginTx`/`Commit` |
| Race handling | Unique constraint catch on `Create` → fallback to `Reinforce` |
| Empty-hash guard | `GetByContentHash("")` returns `nil, nil` |
| Max reinforcement | Unlimited (configurable via `max_reinforcement` setting) |

## Schema Changes

### Migration: `003_content_hash_reinforcement.sql`

```sql
ALTER TABLE objects ADD COLUMN content_hash TEXT DEFAULT '';
ALTER TABLE objects ADD COLUMN reinforcement_count INTEGER DEFAULT 1;
ALTER TABLE objects ADD COLUMN last_reinforced_at TEXT;

-- Partial unique index: excludes pre-migration empty hashes
CREATE UNIQUE INDEX IF NOT EXISTS idx_objects_content_hash
  ON objects(content_hash) WHERE content_hash != '';
```

## Type Changes

### `internal/storage/types.go`

```go
type KnowledgeObject struct {
    // ... existing fields ...
    ContentHash        string     `json:"content_hash,omitempty"`
    ReinforcementCount int        `json:"reinforcement_count,omitempty"`
    LastReinforcedAt   *time.Time `json:"last_reinforced_at,omitempty"`
}
```

## Storage Interface

### `internal/storage/storage.go`

```go
type ObjectStore interface {
    // ... existing methods ...
    GetByContentHash(ctx context.Context, hash string) (*KnowledgeObject, error)
    Reinforce(ctx context.Context, hash string, mergeData *KnowledgeObject) (string, error)
}
```

`GetByContentHash` returns the existing object ID on reinforcement, enabling the worker to complete the job with the correct ID.

## Implementation Components

### 1. Content Hash Utility

**File:** `internal/storageutil/content_hash.go`

- `ContentHash(rawContent, source) string` — SHA-256 of `normalize(rawContent) + "\x00" + source`
- `normalizeContent(s)` — TrimSpace, ToLower, collapse `\s+` to single space
- Whitespace regex is precompiled at package level (`var whitespaceRe`)

### 2. SQLite ObjectStore

**File:** `internal/storage/sqlite/objects.go`

**`GetByContentHash`:**
- Returns `nil, nil` for empty hash (guards against matching pre-migration rows)
- Uses `LIMIT 1` for defense-in-depth
- Full object scan via shared `scanObject`

**`Reinforce`:**
- Rejects empty hash with error
- Wraps read + update in `BeginTx`/`Commit` to prevent concurrent merge races
- Reads only `id`, `tags`, `mentions` (minimal read within tx)
- Merges tags (dedup by label, first-seen wins) and mentions (dedup by string)
- Atomically increments `reinforcement_count` and sets `last_reinforced_at`
- Returns existing object ID

**`Create` / `Update`:**
- Use shared `marshalObjectFields` helper that propagates `json.Marshal` errors
- Include `content_hash`, `reinforcement_count`, `last_reinforced_at` in all column lists

**`mergeTags` / `mergeStrings`:**
- Pre-allocate result slice with combined capacity
- Dedup via `seen` map; existing entries take priority

### 3. Worker Flow

**File:** `internal/jobs/worker.go`

After pipeline execution:

1. Compute `ContentHash(draft.RawContent, draft.Source)`
2. Check `GetByContentHash` — if match found, call `Reinforce` and complete
3. Set `ReinforcementCount = 1`, call `Create`
4. If `Create` fails with `UNIQUE constraint failed` (concurrent race), fall back to `Reinforce`
5. On success, write mention edges (ADR-049) and complete

## Files Created/Modified

| Action | File |
|--------|------|
| Create | `internal/storage/sqlite/migrations/003_content_hash_reinforcement.sql` |
| Create | `internal/storageutil/content_hash.go` |
| Create | `internal/storageutil/content_hash_test.go` |
| Modify | `internal/storage/types.go` |
| Modify | `internal/storage/storage.go` |
| Modify | `internal/storage/storage_test.go` (mock interface) |
| Modify | `internal/storage/sqlite/objects.go` |
| Modify | `internal/storage/sqlite/objects_test.go` |
| Modify | `internal/storage/sqlite/migrations.go` |
| Modify | `internal/jobs/worker.go` |

## Test Coverage

### `internal/storageutil/content_hash_test.go`
- Determinism (same input = same hash)
- Content divergence (different content = different hash)
- Source divergence (different source = different hash)
- Whitespace normalization
- Case insensitivity
- Empty source handling

### `internal/storage/sqlite/objects_test.go`
- `TestMergeTags` — basic merge, dedup by label (first wins), empty existing, empty incoming, both empty
- `TestMergeStrings` — basic merge, dedup, all duplicates, empty existing, empty incoming, both empty
- `TestGetByContentHash` — found, not found, empty hash returns nil
- `TestReinforce` — increments count and merges tags/mentions, empty hash errors

## Configuration

```yaml
deduplication:
  max_reinforcement: 0  # 0 = unlimited
```

## Rollout

1. Run migration on existing databases
2. Backfill `content_hash` for existing objects (batch job)
3. Deploy worker changes
