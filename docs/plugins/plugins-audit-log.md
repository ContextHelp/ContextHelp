# Audit Log

> **Status:** shipped in core. Originally scoped as an extractable plugin; the
> implementation landed in the main module instead. There is no
> `plugins/auditlog/` module — see [Module](#module).

Audit logging records knowledge-object mutations into an immutable, append-only log.
Mutations produce an `AuditEntry` row that can never be modified or removed. The log is
queryable via the `ctxt audit` CLI subtree and the audit REST endpoint.

Entries are stored in a dedicated table (`audit_log`, migration 011) with immutability
enforced by both the Go interface and SQLite triggers.

---

## Overview

Goals:

- Tamper-evident record of every object mutation (per session or across sessions).
- Zero coupling to core logic — subscribe-only, no core pipeline involvement.
- Immutability enforced at two layers: Go interface (no `Delete`/`Update` on `AuditStore`) and
  SQLite `BEFORE UPDATE`/`BEFORE DELETE` triggers.
- Queryable by object ID, event type, actor, and time range.
- Exportable as JSON or CSV for compliance and forensic workflows.

Non-goals:

- Authentication / authorization (not in scope; actor defaults to the event source string).
- Real-time streaming (log is pull-based; push via webhooks is future work).

---

## Architecture

```
service.fanout      ──┐
service.duplicates  ──┼──append──►  AuditStore (SQLite / Postgres)
ctxt doctor         ──┘                        │
                                               │
                     REST handler  ◄──List()───┤
                     CLI commands  ◄──List()───┘
```

Components:

| Component | Location | Responsibility |
|-----------|----------|----------------|
| `AuditEntry` / `AuditStore` | `internal/storage/storage.go` | Type + interface definition |
| SQLite store | `internal/storage/sqlite/auditlog.go` | DB read/write |
| Postgres store | `internal/storage/postgres/auditlog.go` | DB read/write |
| Migration 011 | `internal/storage/sqlite/migrations/011_audit_log.sql` | Table + immutability triggers |
| Append call sites | `internal/service/fanout.go`, `internal/service/duplicates.go` | Emit `AuditEntry` on mutation |
| Export formatting | `internal/audit/format.go` | JSON / CSV rendering |
| REST handlers | `internal/server/http/handlers_audit_log.go` | HTTP endpoints |
| CLI commands | `cmd/ctxt/cmd/audit.go` | `ctxt audit` subtree |

---

## Storage Schema

Migration 011 creates the `audit_log` table:

```sql
CREATE TABLE IF NOT EXISTS audit_log (
    id          TEXT PRIMARY KEY,
    event_type  TEXT NOT NULL,
    object_id   TEXT NOT NULL DEFAULT '',
    actor       TEXT NOT NULL DEFAULT 'system',
    payload     TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL
);
```

Indexes: `object_id`, `event_type`, `created_at`.

Immutability triggers (applied at DB level):

```sql
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

- `object_id` is `TEXT DEFAULT ''` (no FK) — audit rows survive hard-deleted objects.
- `payload TEXT` stores JSON; SQLite JSON functions can query it ad-hoc.
- `created_at TEXT NOT NULL` in RFC3339 format; never updated.
- `actor` defaults to the `Event.Source` string (e.g. `"worker.pool"`); enriched when
  authentication is introduced.

---

## AuditEntry Type

```go
type AuditEntry struct {
    ID        string         `json:"id"`
    EventType string         `json:"event_type"`
    ObjectID  string         `json:"object_id"`
    Actor     string         `json:"actor"`
    Payload   map[string]any `json:"payload"`
    CreatedAt time.Time      `json:"created_at"`
}
```

---

## AuditStore Interface

```go
type AuditStore interface {
    // Append inserts a new entry. Never modifies existing entries.
    Append(ctx context.Context, entry *AuditEntry) error

    // List returns entries matching the filter, ordered by created_at ASC.
    List(ctx context.Context, filter AuditFilter) ([]*AuditEntry, int, error)

    // GetObjectHistory returns all entries for one object, created_at ASC.
    GetObjectHistory(ctx context.Context, objectID string) ([]*AuditEntry, error)
}
```

Note: no `Update` or `Delete` method — append-only by design.

`AuditFilter` fields: `ObjectID`, `EventType`, `Actor`, `After`, `Before`, `Limit`, `Offset`.

---

## Events Subscribed

The plugin subscribes explicitly to the following event types (no wildcard):

| Event Type | Source | Payload keys |
|------------|--------|--------------|
| `object.created` | `worker.pool` | `id`, `pipeline`, `source` |
| `object.updated` | `service.objects` | `id` |
| `object.deleted` | `service.objects` | `id` |
| `object.reinforced` | `worker.pool` | `id` |
| `object.triaged` | inbox service (P240) | `id` |
| `object.discarded` | inbox service (P240) | `id` |
| `job.completed` | `worker.pool` | `job_id`, `result_id` |
| `job.failed` | `worker.pool` | `job_id`, `error` |

Notes:

- `object.created` was added in P520 (Task 5) — previously not published by `worker.go`.
- `object.triaged` and `object.discarded` are defined in P240; the plugin subscribes now so
  they are captured automatically when P240 ships without any plugin change.
- `alias.set` (aliasing plugin) is planned for a future subscription extension.

Object ID extraction priority from event `Data`: `"id"` → `"object_id"` → `"result_id"`.

---

## Configuration

### Config block

```yaml
plugins:
  auditlog:
    enabled: true
    default_limit: 100
```

### Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `true` | Enable/disable the plugin at startup |
| `default_limit` | int | `100` | Default page size for `List` queries |

### Defaults

Audit settings live in the core config tree — see `internal/config/config.go`
(`AuditConfig` for SIEM export/forwarding, `fanout.audit_log` to toggle
changelog entries on fan-out mutations).

---

## REST API

All endpoints return `503 Service Unavailable` if the plugin is not configured.

### `GET /api/v1/audit`

List audit log entries with optional filters.

Query parameters:

| Parameter | Type | Description |
|-----------|------|-------------|
| `object_id` | string | Filter by object ID |
| `event_type` | string | Filter by event type (exact match) |
| `actor` | string | Filter by actor |
| `after` | RFC3339 | Entries created after this timestamp |
| `before` | RFC3339 | Entries created before this timestamp |
| `limit` | int | Max entries (default 100) |
| `offset` | int | Pagination offset |

Response:

```json
{
  "entries": [
    {
      "id": "...",
      "event_type": "object.created",
      "object_id": "obj_001",
      "actor": "worker.pool",
      "payload": { "pipeline": "text.short" },
      "created_at": "2026-03-13T10:00:00Z"
    }
  ],
  "total": 42,
  "limit": 100,
  "offset": 0
}
```

### `GET /api/v1/audit/objects/{id}/history`

Full immutable history for one object, chronological order.

Response: array of `AuditEntry` objects (same shape as above).

### `GET /api/v1/audit/export`

Export the full log.

Query parameters:

| Parameter | Values | Default |
|-----------|--------|---------|
| `format` | `json`, `csv` | `json` |

CSV columns: `id`, `event_type`, `object_id`, `actor`, `created_at`.

---

## CLI

The plugin contributes a `ctxt audit` command subtree.

### `ctxt audit list`

```
ctxt audit list [flags]

Flags:
  --object string   Filter by object ID
  --type string     Filter by event type
  --after string    Entries after date (RFC3339)
  --before string   Entries before date (RFC3339)
  --limit int       Max entries (default 50)
```

Example output:

```
[2026-03-13T10:00:00Z] object.created  obj=obj_001  actor=worker.pool
[2026-03-13T10:05:00Z] object.updated  obj=obj_001  actor=service.objects
total: 2
```

### `ctxt audit history <object-id>`

Full history for one object:

```
ctxt audit history obj_001
```

Output:

```
History for object obj_001 (2 events):
  1. [2026-03-13T10:00:00Z] object.created  actor=worker.pool
  2. [2026-03-13T10:05:00Z] object.updated  actor=service.objects
```

### `ctxt audit export`

Export to stdout:

```
ctxt audit export --format csv > audit.csv
ctxt audit export --format json > audit.json
```

---

## Integration

### Enabling the plugin

1. Add config block (see Configuration section above).
2. Register in `cmd/ctxt/cmd/root.go`:

```go
rootCmd.AddCommand(auditlog.NewCLICommands(serverBaseURL))
```

3. Wire plugin in the server setup:

```go
p := auditlog.New()
p.Init(ctx, rawConfig["auditlog"], plugin.Deps{Bus: bus, Store: driver})
```

### Querying from another plugin

Access `AuditStore` directly via `StorageDriver`:

```go
store := deps.Store.AuditLog()
history, err := store.GetObjectHistory(ctx, objectID)
```

### REST example (curl)

```sh
# List creates for a specific object
curl "http://localhost:8080/api/v1/audit?object_id=obj_001&event_type=object.created"

# Export full log as CSV
curl "http://localhost:8080/api/v1/audit/export?format=csv" -o audit.csv
```

---

## Immutability Guarantees

Two independent layers:

1. **Go interface layer** — `AuditStore` has no `Delete` or `Update` method. No code path can
   mutate an existing entry.
2. **SQLite trigger layer** — `BEFORE UPDATE` and `BEFORE DELETE` triggers abort any attempt to
   mutate the table, even from raw SQL tooling outside the application.

---

## Security & Privacy

- All entries stored locally; no network egress by default.
- Egress happens only when syslog or webhook forwarding is explicitly configured.
- `actor` field contains the event source string, not a user identity; enrichment deferred to
  when authentication is added.
- Entries live in the main SQLite/Postgres store alongside other core data.

---

## Plugin Manifest

```json
{
  "name": "auditlog",
  "version": "1.0.0",
  "description": "Immutable append-only audit log for all object mutations",
  "entrypoint": "code.so",
  "hooks": ["on_cli_start"],
  "permissions": {
    "network": false,
    "filesystem": false
  },
  "requires_plugin_api": ">=1.0,<2.0"
}
```

---

## Module

Audit logging ships **in core**, not as a separate Go module. There is no
`plugins/auditlog/` module and no extra `go.work` entry to add.

Surface lives in the main `github.com/ideacrafterslabs/ctxt` module:

| Concern | Location |
|---------|----------|
| Types + interface | `internal/storage/storage.go` |
| SQLite store | `internal/storage/sqlite/auditlog.go` |
| Postgres store | `internal/storage/postgres/auditlog.go` |
| Migration | `internal/storage/sqlite/migrations/011_audit_log.sql` |
| Export formatting | `internal/audit/format.go` |
| REST handler | `internal/server/http/handlers_audit_log.go` |
| CLI | `cmd/ctxt/cmd/audit.go` |

Entries are appended directly by the services that mutate objects
(`internal/service/fanout.go`, `internal/service/duplicates.go`) rather than via
an event-bus subscription.

---

## Related

- [plugins.md](plugins.md) — Plugin system overview
- [plugins-api.md](plugins-api.md) — Plugin API contract
- [plugins-aliasing.md](plugins-aliasing.md) — Aliasing plugin (emits `alias.set`)
- `internal/events/bus.go` — Event bus (wildcard and typed subscriptions)
- `internal/storage/storage.go` — `AuditEntry`, `AuditFilter`, `AuditStore`
- `internal/storage/sqlite/migrations/011_audit_log.sql` — Table DDL + triggers
- Plan: `docs/plans/2026-03-13-P520-plugin-audit-log.md`
