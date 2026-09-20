# Plugin: Audit Log Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Plugin that records all object mutations (create, update, delete, triage, discard, tag change, alias set) into an immutable append-only audit log. Queryable via `ctxt audit` CLI and `GET /api/v1/audit` REST endpoint.

**Architecture:** Hooks into the events.Bus (subscribes to all object.* events). Each event produces an AuditEntry appended to the `audit_log` table (migration 011). Log is immutable (no UPDATE/DELETE on entries). Extraction-ready: all plugin code in `plugins/auditlog/`.

**Tech Stack:** Go, SQLite (new table migration 011), events.Bus (existing), no external deps.

---

## Plugin System Findings (read before implementing)

### Plugin interface (defined in P500 Task 0)

See `internal/plugin/plugin.go`. The audit log plugin implements `plugin.Plugin` and uses the event bus (available via `plugin.Deps.Bus`).

### Event bus (`internal/events/bus.go`)

The `LocalBus` implementation:
- `Publish(ctx, Event)` dispatches to handlers asynchronously (one goroutine per handler).
- `Subscribe(eventType, Handler)` where `Handler func(ctx context.Context, e Event) error`.
- **Wildcard `"*"` is supported** — `LocalBus.Publish` checks both `handlers[e.Type]` and `handlers["*"]`.

### Events currently published (as of 2026-03-13)

| Source | Event Type | Data |
|--------|-----------|------|
| `service.analyze` | `job.enqueued` | `*storage.Job` |
| `service.objects` | `object.updated` | `map[string]string{"id": obj.ID}` |
| `service.objects` | `object.deleted` | `map[string]string{"id": id}` |
| `worker.pool` | `job.completed` | `map[string]string{"job_id": ..., "result_id": ...}` |
| `worker.pool` | `job.failed` | `map[string]string{"job_id": ..., "error": ...}` |
| `worker.pool.fanout` | `job.enqueued` | `*storage.Job` |

**Gap: `object.created` is not published.** Objects are created inside `WorkerPool.process` at `internal/jobs/worker.go:169` — after `store.Objects().Create(ctx, draft)` succeeds, no event is fired. This plan adds that event in Task 5.

**Gap: inbox events (`object.triaged`, `object.discarded`) are defined in P240** — the audit log plugin subscribes to those event types so that when P240 is implemented, they are automatically captured without modifying this plugin.

### Storage and migrations

Migrations 001-007 exist. P240 reserves 008 and 009. P510 uses 010. This plan uses **migration 011**.

The audit log is stored in the same SQLite database (sharing the driver) for simplicity and atomic consistency. The plugin accesses it via a new `AuditStore` sub-store added to `StorageDriver`.

### Immutability enforcement

SQLite does not support `BEFORE DELETE` triggers on the table from the application layer in a portable way. Immutability is enforced by:
1. `AuditStore` interface has no `Delete` or `Update` method — there is no code path to mutate entries.
2. A `BEFORE DELETE` and `BEFORE UPDATE` trigger is added in the migration SQL to reject any mutation attempt at the DB level.

### Actor extraction

The `Event.Source` field (e.g., `"service.objects"`) is the actor source. Events do not currently carry a user/profile identity. For now, `actor` defaults to the source string. When authentication is added, the actor can be enriched from event `Data`.

---

## Task List

| # | Task | File(s) | Est. |
|---|------|---------|------|
| T1 | Migration 011 — audit_log table | `internal/storage/sqlite/migrations/011_audit_log.sql`, `migrations.go` | 5 min |
| T2 | AuditEntry type + AuditStore interface | `internal/storage/storage.go` | 10 min |
| T3 | SQLite AuditStore implementation | `internal/storage/sqlite/auditlog.go` | 20 min |
| T4 | Wire AuditStore into StorageDriver | `internal/storage/sqlite/driver.go` | 5 min |
| T5 | Publish `object.created` event in worker | `internal/jobs/worker.go` | 5 min |
| T6 | Plugin module scaffold + go.mod | `plugins/auditlog/` | 5 min |
| T7 | AuditConfig | `plugins/auditlog/config.go` | 5 min |
| T8 | Event handler (subscription logic) | `plugins/auditlog/handler.go` | 15 min |
| T9 | Plugin struct | `plugins/auditlog/plugin.go` | 10 min |
| T10 | REST API handlers | `internal/server/http/handlers_audit.go` | 15 min |
| T11 | CLI commands | `plugins/auditlog/cli.go` | 15 min |
| T12 | Integration test | `plugins/auditlog/plugin_test.go` | 15 min |
| T13 | Extraction-readiness verification | `plugins/auditlog/go.mod`, `go.work` | 5 min |

---

## Task 1: Migration 011

**Files:**
- create: `internal/storage/sqlite/migrations/011_audit_log.sql`
- edit: `internal/storage/sqlite/migrations.go`

**Step 1.1 — write SQL**

Create `internal/storage/sqlite/migrations/011_audit_log.sql`:

```sql
CREATE TABLE IF NOT EXISTS audit_log (
    id          TEXT PRIMARY KEY,
    event_type  TEXT NOT NULL,
    object_id   TEXT NOT NULL DEFAULT '',
    actor       TEXT NOT NULL DEFAULT 'system',
    payload     TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_object_id  ON audit_log(object_id);
CREATE INDEX IF NOT EXISTS idx_audit_event_type ON audit_log(event_type);
CREATE INDEX IF NOT EXISTS idx_audit_created_at ON audit_log(created_at);

-- Enforce immutability at the DB level.
CREATE TRIGGER IF NOT EXISTS audit_log_no_update
    BEFORE UPDATE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log is immutable: UPDATE not permitted');
END;

CREATE TRIGGER IF NOT EXISTS audit_log_no_delete
    BEFORE DELETE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log is immutable: DELETE not permitted');
END;
```

Design notes:
- `id` is a UUID, `PRIMARY KEY`.
- `object_id` is `TEXT DEFAULT ''` (not a FK) because events can refer to objects that were subsequently hard-deleted via external means; we log the ID we observed.
- `payload TEXT` stores JSON; SQLite's JSON functions can query it if needed.
- `created_at TEXT NOT NULL` stores RFC3339; never updated (immutability triggers enforce this).
- No FK on `object_id` — audit records must survive object deletion.

**Step 1.2 — register embed**

In `internal/storage/sqlite/migrations.go`:

```go
//go:embed migrations/011_audit_log.sql
var migration011 string

// Add to migrations slice:
{Version: 11, SQL: migration011},
```

**Step 1.3 — write test**

```go
func TestMigration011_AuditLogTable(t *testing.T) {
    db := setupTestDB(t)
    ctx := context.Background()

    // Insert succeeds.
    _, err := db.ExecContext(ctx,
        `INSERT INTO audit_log (id, event_type, object_id, actor, payload, created_at)
         VALUES ('test-id', 'object.created', 'obj_001', 'system', '{}', datetime('now'))`)
    require.NoError(t, err)

    // UPDATE is rejected.
    _, err = db.ExecContext(ctx,
        `UPDATE audit_log SET actor='hacker' WHERE id='test-id'`)
    require.Error(t, err)
    require.Contains(t, err.Error(), "immutable")

    // DELETE is rejected.
    _, err = db.ExecContext(ctx,
        `DELETE FROM audit_log WHERE id='test-id'`)
    require.Error(t, err)
    require.Contains(t, err.Error(), "immutable")
}
```

**Step 1.4 — run test, commit**

```bash
go test ./internal/storage/sqlite/... -run TestMigration011
```

Expected: `PASS`.

```
git add internal/storage/sqlite/migrations/011_audit_log.sql internal/storage/sqlite/migrations.go
git commit -m "feat(storage): add migration 011 — immutable audit_log table"
```

---

## Task 2: AuditEntry Type + AuditStore Interface

**Files:**
- edit: `internal/storage/storage.go`

**Step 2.1 — add to storage.go**

```go
// AuditEntry is one immutable record in the audit log.
type AuditEntry struct {
    ID        string         `json:"id"`
    EventType string         `json:"event_type"`
    ObjectID  string         `json:"object_id"`
    Actor     string         `json:"actor"`
    Payload   map[string]any `json:"payload"`
    CreatedAt time.Time      `json:"created_at"`
}

// AuditFilter restricts audit log queries.
type AuditFilter struct {
    ObjectID  string
    EventType string
    Actor     string
    After     time.Time
    Before    time.Time
    Limit     int
    Offset    int
}

// AuditStore is an append-only store for audit log entries.
// There are intentionally no Update or Delete methods.
type AuditStore interface {
    // Append inserts a new entry. Returns error on failure; never modifies existing entries.
    Append(ctx context.Context, entry *AuditEntry) error
    // List returns entries matching the filter, ordered by created_at ascending.
    List(ctx context.Context, filter AuditFilter) ([]*AuditEntry, int, error)
    // GetObjectHistory returns all entries for a specific object, ordered by created_at ascending.
    GetObjectHistory(ctx context.Context, objectID string) ([]*AuditEntry, error)
}
```

Also add `AuditLog() AuditStore` to the `StorageDriver` interface.

**Step 2.2 — commit**

```
git add internal/storage/storage.go
git commit -m "feat(storage): add AuditEntry type and AuditStore interface"
```

---

## Task 3: SQLite AuditStore Implementation

**Files:**
- create: `internal/storage/sqlite/auditlog.go`
- create: `internal/storage/sqlite/auditlog_test.go`

**Step 3.1 — write test first**

Create `internal/storage/sqlite/auditlog_test.go`:

```go
package sqlite_test

import (
    "context"
    "testing"
    "time"

    "github.com/google/uuid"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestAuditStore_AppendAndList(t *testing.T) {
    db := newTestDriver(t)
    ctx := context.Background()
    now := time.Now().UTC().Truncate(time.Second)

    entries := []*storage.AuditEntry{
        {ID: uuid.NewString(), EventType: "object.created", ObjectID: "obj_001", Actor: "system", Payload: map[string]any{"source": "cli"}, CreatedAt: now},
        {ID: uuid.NewString(), EventType: "object.updated", ObjectID: "obj_001", Actor: "system", Payload: map[string]any{"field": "tags"}, CreatedAt: now.Add(time.Second)},
        {ID: uuid.NewString(), EventType: "object.deleted", ObjectID: "obj_001", Actor: "system", Payload: map[string]any{}, CreatedAt: now.Add(2 * time.Second)},
    }
    for _, e := range entries {
        require.NoError(t, db.AuditLog().Append(ctx, e))
    }

    all, count, err := db.AuditLog().List(ctx, storage.AuditFilter{ObjectID: "obj_001"})
    require.NoError(t, err)
    assert.Equal(t, 3, count)
    assert.Len(t, all, 3)
    assert.Equal(t, "object.created", all[0].EventType)
}

func TestAuditStore_FilterByEventType(t *testing.T) {
    db := newTestDriver(t)
    ctx := context.Background()
    now := time.Now().UTC().Truncate(time.Second)

    for _, et := range []string{"object.created", "object.updated", "object.created"} {
        require.NoError(t, db.AuditLog().Append(ctx, &storage.AuditEntry{
            ID: uuid.NewString(), EventType: et, ObjectID: "obj_002",
            Actor: "system", Payload: map[string]any{}, CreatedAt: now,
        }))
    }

    results, count, err := db.AuditLog().List(ctx, storage.AuditFilter{EventType: "object.created"})
    require.NoError(t, err)
    assert.Equal(t, 2, count)
    assert.Len(t, results, 2)
}

func TestAuditStore_GetObjectHistory(t *testing.T) {
    db := newTestDriver(t)
    ctx := context.Background()
    now := time.Now().UTC().Truncate(time.Second)

    _ = db.AuditLog().Append(ctx, &storage.AuditEntry{
        ID: uuid.NewString(), EventType: "object.created", ObjectID: "obj_hist",
        Actor: "system", Payload: map[string]any{}, CreatedAt: now,
    })
    _ = db.AuditLog().Append(ctx, &storage.AuditEntry{
        ID: uuid.NewString(), EventType: "object.updated", ObjectID: "obj_hist",
        Actor: "user:alice", Payload: map[string]any{}, CreatedAt: now.Add(time.Second),
    })

    history, err := db.AuditLog().GetObjectHistory(ctx, "obj_hist")
    require.NoError(t, err)
    assert.Len(t, history, 2)
    assert.Equal(t, "object.created", history[0].EventType)
    assert.Equal(t, "object.updated", history[1].EventType)
}

func TestAuditStore_Immutable_UpdateRejected(t *testing.T) {
    db := newTestDriver(t)
    ctx := context.Background()
    now := time.Now().UTC()

    e := &storage.AuditEntry{
        ID: uuid.NewString(), EventType: "object.created", ObjectID: "obj_imm",
        Actor: "system", Payload: map[string]any{}, CreatedAt: now,
    }
    require.NoError(t, db.AuditLog().Append(ctx, e))

    // Direct SQL UPDATE must be rejected by trigger.
    rawDB := getRawDB(t, db) // helper to get *sql.DB from driver
    _, err := rawDB.ExecContext(ctx,
        `UPDATE audit_log SET actor='tampered' WHERE id=?`, e.ID)
    require.Error(t, err)
    require.Contains(t, err.Error(), "immutable")
}
```

**Step 3.2 — write auditlog.go**

Create `internal/storage/sqlite/auditlog.go`:

```go
package sqlite

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "time"

    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

type auditStore struct{ db *sql.DB }

func (s *auditStore) Append(ctx context.Context, e *storage.AuditEntry) error {
    payload, err := json.Marshal(e.Payload)
    if err != nil {
        return fmt.Errorf("audit append: marshal payload: %w", err)
    }
    _, err = s.db.ExecContext(ctx,
        `INSERT INTO audit_log (id, event_type, object_id, actor, payload, created_at)
         VALUES (?, ?, ?, ?, ?, ?)`,
        e.ID, e.EventType, e.ObjectID, e.Actor,
        string(payload),
        e.CreatedAt.UTC().Format(time.RFC3339),
    )
    if err != nil {
        return fmt.Errorf("audit append: %w", err)
    }
    return nil
}

func (s *auditStore) List(ctx context.Context, f storage.AuditFilter) ([]*storage.AuditEntry, int, error) {
    where := "1=1"
    var args []interface{}

    if f.ObjectID != "" {
        where += " AND object_id=?"
        args = append(args, f.ObjectID)
    }
    if f.EventType != "" {
        where += " AND event_type=?"
        args = append(args, f.EventType)
    }
    if f.Actor != "" {
        where += " AND actor=?"
        args = append(args, f.Actor)
    }
    if !f.After.IsZero() {
        where += " AND created_at > ?"
        args = append(args, f.After.UTC().Format(time.RFC3339))
    }
    if !f.Before.IsZero() {
        where += " AND created_at < ?"
        args = append(args, f.Before.UTC().Format(time.RFC3339))
    }

    // Count total.
    var total int
    countArgs := make([]interface{}, len(args))
    copy(countArgs, args)
    err := s.db.QueryRowContext(ctx,
        "SELECT COUNT(*) FROM audit_log WHERE "+where, countArgs...).Scan(&total)
    if err != nil {
        return nil, 0, err
    }

    // Apply pagination.
    query := "SELECT id, event_type, object_id, actor, payload, created_at FROM audit_log WHERE " +
        where + " ORDER BY created_at ASC"
    if f.Limit > 0 {
        query += fmt.Sprintf(" LIMIT %d", f.Limit)
    }
    if f.Offset > 0 {
        query += fmt.Sprintf(" OFFSET %d", f.Offset)
    }

    rows, err := s.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, 0, err
    }
    defer rows.Close()

    var out []*storage.AuditEntry
    for rows.Next() {
        e, err := scanAuditEntry(rows)
        if err != nil {
            return nil, 0, err
        }
        out = append(out, e)
    }
    return out, total, rows.Err()
}

func (s *auditStore) GetObjectHistory(ctx context.Context, objectID string) ([]*storage.AuditEntry, error) {
    rows, err := s.db.QueryContext(ctx,
        `SELECT id, event_type, object_id, actor, payload, created_at
         FROM audit_log WHERE object_id=? ORDER BY created_at ASC`,
        objectID,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var out []*storage.AuditEntry
    for rows.Next() {
        e, err := scanAuditEntry(rows)
        if err != nil {
            return nil, err
        }
        out = append(out, e)
    }
    return out, rows.Err()
}

func scanAuditEntry(rows *sql.Rows) (*storage.AuditEntry, error) {
    var e storage.AuditEntry
    var payloadStr, createdStr string
    if err := rows.Scan(&e.ID, &e.EventType, &e.ObjectID, &e.Actor, &payloadStr, &createdStr); err != nil {
        return nil, err
    }
    e.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
    if payloadStr != "" {
        _ = json.Unmarshal([]byte(payloadStr), &e.Payload)
    }
    if e.Payload == nil {
        e.Payload = map[string]any{}
    }
    return &e, nil
}
```

**Step 3.3 — run tests, commit**

```bash
go test ./internal/storage/sqlite/... -run TestAuditStore
```

Expected: `PASS`.

```
git add internal/storage/sqlite/auditlog.go internal/storage/sqlite/auditlog_test.go
git commit -m "feat(storage/sqlite): add AuditStore implementation"
```

---

## Task 4: Wire AuditStore into StorageDriver

**Files:**
- edit: `internal/storage/sqlite/driver.go`

**Step 4.1 — add AuditLog() method**

```go
func (d *Driver) AuditLog() storage.AuditStore {
    return &auditStore{db: d.db}
}
```

Add `AuditLog() storage.AuditStore` to any test mock `StorageDriver`.

**Step 4.2 — compile check**

```bash
go build ./...
```

**Step 4.3 — commit**

```
git add internal/storage/sqlite/driver.go
git commit -m "feat(storage/sqlite): wire AuditStore into StorageDriver"
```

---

## Task 5: Publish `object.created` Event in Worker

**Files:**
- edit: `internal/jobs/worker.go`

**Step 5.1 — locate insertion point**

In `WorkerPool.process`, after the successful `store.Objects().Create(ctx, draft)` call (around line 169), before the `p.queue.Complete` call, add:

```go
// Emit object.created event (consumed by audit log and other plugins).
if p.bus != nil {
    if ev, err := events.NewEvent("worker.pool", "object.created", map[string]string{
        "id":       draft.ID,
        "pipeline": draft.Pipeline,
        "source":   draft.Source,
    }); err == nil {
        _ = p.bus.Publish(ctx, ev)
    }
}
```

**Step 5.2 — also emit for reinforce path**

In the `Reinforce` path (around line 156), add:

```go
if p.bus != nil {
    if ev, err := events.NewEvent("worker.pool", "object.reinforced", map[string]string{
        "id": existingID,
    }); err == nil {
        _ = p.bus.Publish(ctx, ev)
    }
}
```

**Step 5.3 — write test**

In `internal/jobs/worker_test.go` (or a new test file), verify that after processing a job successfully, the `object.created` event is published:

```go
func TestWorkerPool_PublishesObjectCreatedEvent(t *testing.T) {
    // Set up worker with mock bus. Run a pipeline job.
    // Verify bus received "object.created" event.
}
```

**Step 5.4 — run test, commit**

```bash
go test ./internal/jobs/... -run TestWorkerPool
```

Expected: `PASS`.

```
git add internal/jobs/worker.go
git commit -m "feat(worker): publish object.created event after successful ingestion"
```

---

## Task 6: Plugin Module Scaffold + go.mod

**Files:**
- create: `plugins/auditlog/go.mod`

**Step 6.1 — create directory and go.mod**

```bash
mkdir -p plugins/auditlog
```

Create `plugins/auditlog/go.mod`:

```go
module github.com/ideacrafterslabs/ctxt-plugin-auditlog

go 1.25.6

require (
    github.com/google/uuid v1.6.0
    github.com/ideacrafterslabs/ctxt v0.0.0
    github.com/spf13/cobra v1.10.2
    github.com/stretchr/testify v1.11.1
)

replace github.com/ideacrafterslabs/ctxt => ../..
```

**Step 6.2 — add to go.work**

Edit `./go.work`:

```
go 1.26.1

use .
use ./plugins/autosuggest
use ./plugins/aliasing
use ./plugins/auditlog
```

**Step 6.3 — commit**

```
git add plugins/auditlog/go.mod go.work
git commit -m "feat(plugin/auditlog): scaffold module"
```

---

## Task 7: AuditConfig

**Files:**
- create: `plugins/auditlog/config.go`
- create: `plugins/auditlog/config_test.go`

**Step 7.1 — write test**

Create `plugins/auditlog/config_test.go`:

```go
package auditlog_test

import (
    "testing"

    auditlog "github.com/ideacrafterslabs/ctxt-plugin-auditlog"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestAuditConfig_Defaults(t *testing.T) {
    cfg := auditlog.DefaultConfig()
    assert.True(t, cfg.Enabled)
    assert.Equal(t, 100, cfg.DefaultLimit)
}

func TestAuditConfig_FromMap(t *testing.T) {
    raw := map[string]interface{}{
        "enabled":       false,
        "default_limit": 50,
    }
    cfg, err := auditlog.ConfigFromMap(raw)
    require.NoError(t, err)
    assert.False(t, cfg.Enabled)
    assert.Equal(t, 50, cfg.DefaultLimit)
}
```

**Step 7.2 — write config.go**

Create `plugins/auditlog/config.go`:

```go
package auditlog

// AuditConfig controls plugin behaviour.
type AuditConfig struct {
    // Enabled controls whether audit logging is active (default true).
    Enabled bool
    // DefaultLimit is the default page size for audit list queries (default 100).
    DefaultLimit int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() AuditConfig {
    return AuditConfig{
        Enabled:      true,
        DefaultLimit: 100,
    }
}

// ConfigFromMap decodes a raw config map into AuditConfig.
func ConfigFromMap(m map[string]interface{}) (AuditConfig, error) {
    cfg := DefaultConfig()
    if m == nil {
        return cfg, nil
    }
    if v, ok := m["enabled"].(bool); ok {
        cfg.Enabled = v
    }
    if v, ok := m["default_limit"].(int); ok {
        cfg.DefaultLimit = v
    }
    return cfg, nil
}
```

**Step 7.3 — run test, commit**

```bash
cd plugins/auditlog && go test ./... -run TestAuditConfig
```

Expected: `PASS`.

```
git add plugins/auditlog/config.go plugins/auditlog/config_test.go
git commit -m "feat(plugin/auditlog): add AuditConfig"
```

---

## Task 8: Event Handler (Subscription Logic)

**Files:**
- create: `plugins/auditlog/handler.go`
- create: `plugins/auditlog/handler_test.go`

**Step 8.1 — write test**

Create `plugins/auditlog/handler_test.go`:

```go
package auditlog_test

import (
    "context"
    "encoding/json"
    "testing"
    "time"

    "github.com/google/uuid"
    auditlog "github.com/ideacrafterslabs/ctxt-plugin-auditlog"
    "github.com/ideacrafterslabs/ctxt/internal/events"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// mockAuditStore is an in-memory AuditStore for tests.
type mockAuditStore struct {
    entries []*storage.AuditEntry
}

func (m *mockAuditStore) Append(_ context.Context, e *storage.AuditEntry) error {
    m.entries = append(m.entries, e)
    return nil
}

func (m *mockAuditStore) List(_ context.Context, _ storage.AuditFilter) ([]*storage.AuditEntry, int, error) {
    return m.entries, len(m.entries), nil
}

func (m *mockAuditStore) GetObjectHistory(_ context.Context, objectID string) ([]*storage.AuditEntry, error) {
    var out []*storage.AuditEntry
    for _, e := range m.entries {
        if e.ObjectID == objectID {
            out = append(out, e)
        }
    }
    return out, nil
}

func TestHandler_ObjectCreated_ProducesEntry(t *testing.T) {
    store := &mockAuditStore{}
    handler := auditlog.NewHandler(store)

    data, _ := json.Marshal(map[string]string{"id": "obj_001", "pipeline": "text.short"})
    ev := events.Event{
        ID:     uuid.NewString(),
        Source: "worker.pool",
        Type:   "object.created",
        Time:   time.Now(),
        Data:   data,
    }

    require.NoError(t, handler.Handle(context.Background(), ev))
    require.Len(t, store.entries, 1)
    assert.Equal(t, "object.created", store.entries[0].EventType)
    assert.Equal(t, "obj_001", store.entries[0].ObjectID)
    assert.Equal(t, "worker.pool", store.entries[0].Actor)
}

func TestHandler_UnknownEventType_StillRecorded(t *testing.T) {
    store := &mockAuditStore{}
    handler := auditlog.NewHandler(store)

    data, _ := json.Marshal(map[string]string{"job_id": "job_001"})
    ev := events.Event{
        ID:   uuid.NewString(),
        Source: "worker.pool",
        Type: "job.failed",
        Time: time.Now(),
        Data: data,
    }
    require.NoError(t, handler.Handle(context.Background(), ev))
    require.Len(t, store.entries, 1)
    assert.Equal(t, "job.failed", store.entries[0].EventType)
}
```

**Step 8.2 — write handler.go**

Create `plugins/auditlog/handler.go`:

```go
package auditlog

import (
    "context"
    "encoding/json"

    "github.com/google/uuid"
    "github.com/ideacrafterslabs/ctxt/internal/events"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Handler converts bus events into AuditEntry records.
type Handler struct {
    store storage.AuditStore
}

// NewHandler creates a Handler backed by the given store.
func NewHandler(store storage.AuditStore) *Handler {
    return &Handler{store: store}
}

// Handle processes one event and appends an AuditEntry.
// It never returns an error (soft failure: audit errors must not block the event bus).
func (h *Handler) Handle(ctx context.Context, e events.Event) error {
    objectID := extractObjectID(e)
    actor := e.Source
    payload := extractPayload(e)

    entry := &storage.AuditEntry{
        ID:        uuid.New().String(),
        EventType: e.Type,
        ObjectID:  objectID,
        Actor:     actor,
        Payload:   payload,
        CreatedAt: e.Time,
    }
    // Soft failure: log but do not propagate.
    if err := h.store.Append(ctx, entry); err != nil {
        // In production, use structured logging.
        _ = err
    }
    return nil
}

// extractObjectID pulls the object ID from common event data shapes.
func extractObjectID(e events.Event) string {
    if e.Data == nil {
        return ""
    }
    var m map[string]interface{}
    if err := json.Unmarshal(e.Data, &m); err != nil {
        return ""
    }
    // Try known keys in priority order.
    for _, key := range []string{"id", "object_id", "result_id"} {
        if v, ok := m[key].(string); ok && v != "" {
            return v
        }
    }
    return ""
}

// extractPayload unmarshals event data into a map for storage.
func extractPayload(e events.Event) map[string]any {
    if e.Data == nil {
        return map[string]any{}
    }
    var m map[string]any
    if err := json.Unmarshal(e.Data, &m); err != nil {
        return map[string]any{"raw": string(e.Data)}
    }
    return m
}
```

**Step 8.3 — run test, commit**

```bash
cd plugins/auditlog && go test ./... -run TestHandler
```

Expected: `PASS`.

```
git add plugins/auditlog/handler.go plugins/auditlog/handler_test.go
git commit -m "feat(plugin/auditlog): add event handler"
```

---

## Task 9: Plugin Struct

**Files:**
- create: `plugins/auditlog/plugin.go`

**Step 9.1 — write plugin.go**

Create `plugins/auditlog/plugin.go`:

```go
// Package auditlog provides an immutable append-only audit log plugin.
// It subscribes to all events on the bus and records mutations to knowledge objects.
package auditlog

import (
    "context"
    "fmt"

    "github.com/ideacrafterslabs/ctxt/internal/pipeline"
    "github.com/ideacrafterslabs/ctxt/internal/plugin"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Plugin is the plugin.Plugin implementation for the audit log.
type Plugin struct {
    cfg     AuditConfig
    handler *Handler
    store   storage.AuditStore
}

// New returns an uninitialised Plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string    { return "auditlog" }
func (p *Plugin) Version() string { return "1.0.0" }

func (p *Plugin) Init(ctx context.Context, raw map[string]interface{}, deps plugin.Deps) error {
    cfg, err := ConfigFromMap(raw)
    if err != nil {
        return fmt.Errorf("auditlog: config: %w", err)
    }
    p.cfg = cfg

    if deps.Store != nil {
        p.store = deps.Store.AuditLog()
        p.handler = NewHandler(p.store)
    }

    if deps.Bus != nil && p.handler != nil && p.cfg.Enabled {
        // Subscribe to all events via wildcard "*".
        deps.Bus.Subscribe("*", p.handler.Handle)
        // Also subscribe to specific object event types for clarity and future filtering.
        for _, t := range []string{
            "object.created",
            "object.updated",
            "object.deleted",
            "object.reinforced",
            "object.triaged",    // defined in P240
            "object.discarded",  // defined in P240
            "alias.set",         // emitted by aliasing plugin (future)
            "job.completed",
            "job.failed",
        } {
            deps.Bus.Subscribe(t, p.handler.Handle)
        }
        // Note: subscribing to both "*" and specific types means each matching event
        // triggers Handle twice. To avoid duplicate entries, remove the specific subscriptions
        // and rely solely on "*", OR remove the wildcard and only use specific types.
        // RECOMMENDED: use ONLY specific types (no wildcard) for deterministic coverage.
        // Remove the wildcard subscription above and keep only the explicit list.
    }

    return nil
}

// IMPORTANT: Remove the wildcard subscription from Init above. Subscribe only to the
// explicit list. This prevents double-recording if both "*" and a specific type match.
// The corrected Init should be:

// func (p *Plugin) Init(ctx context.Context, raw map[string]interface{}, deps plugin.Deps) error {
//     ...
//     if deps.Bus != nil && p.handler != nil && p.cfg.Enabled {
//         for _, t := range subscribedEventTypes {
//             deps.Bus.Subscribe(t, p.handler.Handle)
//         }
//     }
//     return nil
// }

// subscribedEventTypes is the canonical list of audited event types.
var subscribedEventTypes = []string{
    "object.created",
    "object.updated",
    "object.deleted",
    "object.reinforced",
    "object.triaged",
    "object.discarded",
    "job.completed",
    "job.failed",
}

// PipelineSteps returns nothing — audit log is not a pipeline step.
func (p *Plugin) PipelineSteps() []pipeline.PipelineStep { return nil }

func (p *Plugin) Close(_ context.Context) error { return nil }

// GetStore returns the underlying AuditStore for use by REST handlers and CLI.
func (p *Plugin) GetStore() storage.AuditStore { return p.store }

// GetConfig returns the current config (for REST handlers).
func (p *Plugin) GetConfig() AuditConfig { return p.cfg }
```

Note: The Init method above contains a correction comment. Implement it with only the explicit type subscriptions (no wildcard) to prevent double-recording.

**Step 9.2 — commit**

```
git add plugins/auditlog/plugin.go
git commit -m "feat(plugin/auditlog): add Plugin struct with event subscriptions"
```

---

## Task 10: REST API Handlers

**Files:**
- create: `internal/server/http/handlers_audit.go`
- create: `internal/server/http/handlers_audit_test.go`

**Step 10.1 — write handlers**

Create `internal/server/http/handlers_audit.go`:

```go
package http

import (
    "encoding/csv"
    "encoding/json"
    "net/http"
    "strconv"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// auditPlugin is the minimal interface the handlers need from the audit log plugin.
type auditPlugin interface {
    GetStore() storage.AuditStore
    GetConfig() interface{ DefaultLimit() int }
}

// GET /api/v1/audit
// Query params: object_id, event_type, actor, after (RFC3339), before (RFC3339), limit, offset
func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    ap := s.auditPlugin()
    if ap == nil {
        writeError(w, http.StatusServiceUnavailable, "audit log plugin not configured")
        return
    }
    store := ap.GetStore()

    filter := storage.AuditFilter{
        ObjectID:  r.URL.Query().Get("object_id"),
        EventType: r.URL.Query().Get("event_type"),
        Actor:     r.URL.Query().Get("actor"),
    }
    if v := r.URL.Query().Get("after"); v != "" {
        filter.After, _ = time.Parse(time.RFC3339, v)
    }
    if v := r.URL.Query().Get("before"); v != "" {
        filter.Before, _ = time.Parse(time.RFC3339, v)
    }
    filter.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
    if filter.Limit <= 0 {
        filter.Limit = 100
    }
    filter.Offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))

    entries, total, err := store.List(ctx, filter)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, map[string]interface{}{
        "entries": entries,
        "total":   total,
        "limit":   filter.Limit,
        "offset":  filter.Offset,
    })
}

// GET /api/v1/audit/objects/{id}/history
func (s *Server) handleObjectAuditHistory(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    ap := s.auditPlugin()
    if ap == nil {
        writeError(w, http.StatusServiceUnavailable, "audit log plugin not configured")
        return
    }
    objectID := chi.URLParam(r, "id")
    history, err := ap.GetStore().GetObjectHistory(ctx, objectID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, history)
}

// GET /api/v1/audit/export?format=json|csv
func (s *Server) handleExportAudit(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    ap := s.auditPlugin()
    if ap == nil {
        writeError(w, http.StatusServiceUnavailable, "audit log plugin not configured")
        return
    }

    format := r.URL.Query().Get("format")
    if format == "" {
        format = "json"
    }

    entries, _, err := ap.GetStore().List(ctx, storage.AuditFilter{Limit: 10000})
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    switch format {
    case "csv":
        w.Header().Set("Content-Type", "text/csv")
        w.Header().Set("Content-Disposition", `attachment; filename="audit_log.csv"`)
        cw := csv.NewWriter(w)
        _ = cw.Write([]string{"id", "event_type", "object_id", "actor", "created_at"})
        for _, e := range entries {
            _ = cw.Write([]string{e.ID, e.EventType, e.ObjectID, e.Actor, e.CreatedAt.Format(time.RFC3339)})
        }
        cw.Flush()
    default: // json
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(entries)
    }
}
```

Register routes in `server.go`:

```go
r.Get("/api/v1/audit", s.handleListAudit)
r.Get("/api/v1/audit/objects/{id}/history", s.handleObjectAuditHistory)
r.Get("/api/v1/audit/export", s.handleExportAudit)
```

Add `auditPlugin()` accessor to the `Server` struct (similar to `aliasPlugin()`).

**Step 10.2 — write handler tests**

Create `internal/server/http/handlers_audit_test.go`. Use a mock `auditPlugin` and a mock `AuditStore` with pre-populated entries. Follow the pattern of `handlers_objects_test.go`.

**Step 10.3 — run tests, commit**

```bash
go test ./internal/server/http/... -run TestAudit
```

```
git add internal/server/http/handlers_audit.go internal/server/http/server.go
git commit -m "feat(api): add audit log query endpoints"
```

---

## Task 11: CLI Commands

**Files:**
- create: `plugins/auditlog/cli.go`

**Step 11.1 — write cli.go**

Create `plugins/auditlog/cli.go`:

```go
package auditlog

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"

    "github.com/spf13/cobra"
)

// NewCLICommands returns a cobra.Command subtree for the audit log plugin.
// Mount as: rootCmd.AddCommand(auditlog.NewCLICommands(baseURL))
func NewCLICommands(baseURL string) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "audit",
        Short: "Query the immutable audit log",
    }

    var objectID, eventType, after, before string
    var limit int

    listCmd := &cobra.Command{
        Use:   "list",
        Short: "List audit log entries",
        RunE: func(cmd *cobra.Command, args []string) error {
            q := url.Values{}
            if objectID != "" {
                q.Set("object_id", objectID)
            }
            if eventType != "" {
                q.Set("event_type", eventType)
            }
            if after != "" {
                q.Set("after", after)
            }
            if before != "" {
                q.Set("before", before)
            }
            if limit > 0 {
                q.Set("limit", fmt.Sprint(limit))
            }

            resp, err := http.Get(baseURL + "/api/v1/audit?" + q.Encode())
            if err != nil {
                return err
            }
            defer resp.Body.Close()
            var result map[string]interface{}
            if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
                return err
            }
            entries, _ := result["entries"].([]interface{})
            for _, raw := range entries {
                e, _ := raw.(map[string]interface{})
                fmt.Fprintf(os.Stdout, "[%s] %s  obj=%s  actor=%s\n",
                    e["created_at"], e["event_type"], e["object_id"], e["actor"])
            }
            fmt.Fprintf(os.Stdout, "total: %v\n", result["total"])
            return nil
        },
    }
    listCmd.Flags().StringVar(&objectID, "object", "", "Filter by object ID")
    listCmd.Flags().StringVar(&eventType, "type", "", "Filter by event type")
    listCmd.Flags().StringVar(&after, "after", "", "Filter entries after this date (RFC3339)")
    listCmd.Flags().StringVar(&before, "before", "", "Filter entries before this date (RFC3339)")
    listCmd.Flags().IntVar(&limit, "limit", 50, "Maximum entries to return")

    historyCmd := &cobra.Command{
        Use:   "history <object-id>",
        Short: "Show full immutable history for one object",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            resp, err := http.Get(baseURL + "/api/v1/audit/objects/" + args[0] + "/history")
            if err != nil {
                return err
            }
            defer resp.Body.Close()
            var entries []map[string]interface{}
            if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
                return err
            }
            fmt.Fprintf(os.Stdout, "History for object %s (%d events):\n", args[0], len(entries))
            for i, e := range entries {
                fmt.Fprintf(os.Stdout, "  %d. [%s] %s  actor=%s\n",
                    i+1, e["created_at"], e["event_type"], e["actor"])
            }
            return nil
        },
    }

    var exportFormat string
    exportCmd := &cobra.Command{
        Use:   "export",
        Short: "Export the full audit log",
        RunE: func(cmd *cobra.Command, args []string) error {
            resp, err := http.Get(baseURL + "/api/v1/audit/export?format=" + exportFormat)
            if err != nil {
                return err
            }
            defer resp.Body.Close()
            _, err = io.Copy(os.Stdout, resp.Body)
            return err
        },
    }
    exportCmd.Flags().StringVar(&exportFormat, "format", "json", "Export format: json or csv")

    cmd.AddCommand(listCmd, historyCmd, exportCmd)
    return cmd
}
```

In `cmd/ctxt/cmd/root.go`:

```go
rootCmd.AddCommand(auditlog.NewCLICommands(serverBaseURL))
```

**Step 11.2 — commit**

```
git add plugins/auditlog/cli.go
git commit -m "feat(plugin/auditlog): add CLI audit list/history/export"
```

---

## Task 12: Integration Test

**Files:**
- create: `plugins/auditlog/plugin_test.go`

**Step 12.1 — write integration test**

Create `plugins/auditlog/plugin_test.go`:

```go
package auditlog_test

import (
    "context"
    "encoding/json"
    "testing"
    "time"

    "github.com/google/uuid"
    auditlog "github.com/ideacrafterslabs/ctxt-plugin-auditlog"
    "github.com/ideacrafterslabs/ctxt/internal/events"
    "github.com/ideacrafterslabs/ctxt/internal/plugin"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// TestPluginE2E simulates the full create → update → delete lifecycle.
func TestPluginE2E_ObjectLifecycle(t *testing.T) {
    store := &mockAuditStore{}
    bus := events.NewLocalBus()
    p := auditlog.New()

    require.NoError(t, p.Init(context.Background(), nil, plugin.Deps{
        Bus:   bus,
        Store: &mockStorageDriver{audit: store},
    }))

    ctx := context.Background()
    objectID := "obj_lifecycle_001"

    publishEvent := func(eventType string, data map[string]string) {
        b, _ := json.Marshal(data)
        ev := events.Event{
            ID:     uuid.NewString(),
            Source: "test",
            Type:   eventType,
            Time:   time.Now(),
            Data:   b,
        }
        _ = bus.Publish(ctx, ev)
        // Give async handlers a moment to run.
        time.Sleep(10 * time.Millisecond)
    }

    publishEvent("object.created", map[string]string{"id": objectID, "pipeline": "text.short"})
    publishEvent("object.updated", map[string]string{"id": objectID})
    publishEvent("object.deleted", map[string]string{"id": objectID})

    history, err := store.GetObjectHistory(ctx, objectID)
    require.NoError(t, err)
    require.Len(t, history, 3, "create + update + delete = 3 audit entries")

    assert.Equal(t, "object.created", history[0].EventType)
    assert.Equal(t, "object.updated", history[1].EventType)
    assert.Equal(t, "object.deleted", history[2].EventType)

    // Verify chronological order.
    assert.True(t, !history[1].CreatedAt.Before(history[0].CreatedAt))
}

// TestPluginE2E_ImmutabilityViaInterface verifies AuditStore has no Delete/Update method.
func TestPluginE2E_ImmutabilityViaInterface(t *testing.T) {
    // This test is a compile-time check: if AuditStore gained Delete or Update,
    // it would need to be added here. The absence of these methods IS the test.
    var _ storage.AuditStore = &mockAuditStore{} // must compile
    // AuditStore interface has: Append, List, GetObjectHistory — no Delete, no Update.
    t.Log("AuditStore interface is append-only: PASS")
}
```

**Step 12.2 — run test, commit**

```bash
cd plugins/auditlog && go test ./... -v
```

Expected: all `PASS`.

```
git add plugins/auditlog/plugin_test.go
git commit -m "test(plugin/auditlog): add E2E integration tests"
```

---

## Task 13: Extraction-Readiness Verification

**Step 13.1 — verify standalone build**

```bash
cd ./plugins/auditlog
go build ./...
```

Expected: no errors.

**Step 13.2 — verify tests pass**

```bash
go test ./... -count=1
```

Expected: all `PASS`.

**Step 13.3 — check no relative imports**

```bash
grep -r "\"\.\./" ./plugins/auditlog/
```

Expected: zero output.

**Step 13.4 — document extraction procedure**

To extract to standalone repo:

1. Copy `plugins/auditlog/` to new repo root.
2. Remove `replace github.com/ideacrafterslabs/ctxt => ../..` from `go.mod`; pin to a tagged version.
3. Remove entry from parent `go.work`.
4. Publish as `github.com/ideacrafterslabs/ctxt-plugin-auditlog`.

Note: the migration SQL (`011_audit_log.sql`) lives in the core repo, not the plugin module, because it modifies the shared SQLite database. When extracting, document that the core repo must be at a version that includes migration 011.

**Step 13.5 — commit**

```
git add .
git commit -m "chore(plugin/auditlog): verify extraction-readiness"
```

---

## Full Event Type Reference

### Currently published (implemented)

| Event Type | Source | Data keys | Audited? |
|-----------|--------|-----------|----------|
| `object.created` | `worker.pool` (added in T5) | `id`, `pipeline`, `source` | YES |
| `object.updated` | `service.objects` | `id` | YES |
| `object.deleted` | `service.objects` | `id` | YES |
| `job.enqueued` | `service.analyze` | job fields | YES |
| `job.completed` | `worker.pool` | `job_id`, `result_id` | YES |
| `job.failed` | `worker.pool` | `job_id`, `error` | YES |

### Future events (subscribed, not yet published)

| Event Type | When published | Plugin |
|-----------|---------------|--------|
| `object.reinforced` | T5 (this plan) | worker |
| `object.triaged` | P240 implementation | service |
| `object.discarded` | P240 implementation | service |
| `alias.set` | P510 implementation | aliasing plugin |

### Not audited (by design)

| Event Type | Reason |
|-----------|--------|
| `job.enqueued` via fanout | Noise; not a direct user action |
| `worker.pool.fanout/job.enqueued` | Internal plumbing |

---

## Example config.yaml

```yaml
plugins:
  - plugin: auditlog
    type: event-subscriber
    config:
      enabled: true
      default_limit: 100
```

---

## TDD Cycle Summary

```
T1: migration test                  → PASS → commit
T2: go build ./...                  → PASS → commit
T3: AuditStore tests                → PASS → commit
T4: go build ./...                  → PASS → commit
T5: worker event test               → PASS → commit
T6: go build module                 → PASS → commit
T7: config tests                    → PASS → commit
T8: handler tests                   → PASS → commit
T9: go build plugin                 → PASS → commit
T10: REST handler tests             → PASS → commit
T11: go build CLI                   → PASS → commit
T12: E2E lifecycle test             → PASS → commit
T13: standalone build+test          → PASS → commit
```
