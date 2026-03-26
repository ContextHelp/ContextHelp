# US-0034: Export and Backup All Knowledge

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Operations](../../personas/operations.md), [Maintainers](../../personas/maintainers.md)

---

## User Goal

As an operator, I want to export the full knowledge base to a portable format
(JSON/SQLite dump) and verify the backup is consistent, so I can restore it on
another instance or keep offline copies.

---

## Context

dPKMS uses SQLite as the default storage backend (`storage.type: sqlite`,
`storage.path`). The housekeeping subsystem (`dpkms housekeeping`) handles
maintenance operations directly on the database file. For backup, the operator
can: (a) run `dpkms housekeeping vacuum` + copy the SQLite file, or (b) use
`dpkms housekeeping prune` to trim stale data before archiving. A future
dedicated export API will offer streaming JSON export; until then the canonical
backup path is a consistent SQLite copy after a vacuum. The `GET /health`
endpoint confirms the server is in a clean state before taking a backup.

---

## Acceptance Criteria

- [ ] `dpkms housekeeping vacuum` reclaims unused space in the SQLite file
- [ ] `dpkms housekeeping compact` runs `PRAGMA optimize` + `VACUUM` in sequence
- [ ] `dpkms housekeeping prune --before <YYYY-MM-DD>` removes objects before date
- [ ] `--before` flag value is forwarded to the storage layer as the cutoff timestamp
- [ ] Prune shows count of objects found before prompting for confirmation
- [ ] After vacuum/compact, the SQLite file is self-consistent (passes PRAGMA integrity_check)
- [ ] `GET /health` returns `{"status":"ok"}` after backup operations complete
- [ ] Operator can copy the SQLite file while the server is stopped and restore it
- [ ] Restored backup contains all objects present at backup time

---

## Implementation Notes

### CLI Commands (dpkms)

```
# Step 1: confirm server health before backup
curl http://localhost:8080/health

# Step 2: compact database (optimize + vacuum)
dpkms housekeeping compact

# Step 3 (optional): prune objects older than a cutoff
dpkms housekeeping prune --before 2025-01-01

# Step 4: copy the SQLite file while dpkms is stopped
cp ~/.local/share/contexthelp/db.sqlite ~/backups/db-$(date +%Y%m%d).sqlite
```

### REST API (health gate)

```
GET /health
→ 200 OK
{"status": "ok"}

→ 503 Service Unavailable
{"error": {"code": "UNHEALTHY", "message": "storage: disk full"}}
```

### Housekeeping SQL Internals

```
vacuum:   VACUUM
compact:  PRAGMA optimize
          VACUUM
prune:    SELECT * FROM objects WHERE created_at < $before  (confirm)
          DELETE FROM objects WHERE id = $id  (per row)
```

### Flag → Storage Mapping

| CLI flag    | Storage parameter       | Notes                              |
|-------------|-------------------------|------------------------------------|
| `--before`  | `ObjectFilter.Before`   | ISO date `YYYY-MM-DD`; required    |

---

## E2E Test Checklist

### CLI → Storage payload
- [ ] `dpkms housekeeping vacuum` executes `VACUUM` on the configured database path
  (path taken from `storage.path` config, not hardcoded)
- [ ] `dpkms housekeeping compact` executes both `PRAGMA optimize` and `VACUUM`
  against the same database path
- [ ] `dpkms housekeeping prune --before 2025-01-01` sends cutoff `2025-01-01`
  as the `Before` filter to the storage layer
- [ ] Prune without `--before` flag returns an error (flag is required)
- [ ] Invalid date format for `--before` (e.g., `01/01/2025`) returns parse error

### Server-side receipt and storage validation
- [ ] After `vacuum`: SQLite `PRAGMA integrity_check` on the file returns `ok`
- [ ] After `compact`: file size is equal or smaller than before compaction
- [ ] `prune --before 2025-01-01` output shows the count of matching objects
  before deletion (e.g., "Found 42 objects before 2025-01-01")
- [ ] After confirming prune, `GET /api/v1/objects?before=2025-01-01` returns
  zero results for objects created before that date
- [ ] Objects created after the cutoff are unaffected by prune

### Health gate
- [ ] `GET /health` returns `{"status":"ok"}` with HTTP 200 after each operation
- [ ] `GET /health` returns HTTP 503 when database file is missing or corrupt

### Restore verification
- [ ] Copying the SQLite file after `compact` and restarting `dpkms serve`
  with that file path starts cleanly (no migration errors)
- [ ] `GET /api/v1/objects` on the restored instance returns the expected object count

### Error paths
- [ ] `dpkms housekeeping vacuum` with non-sqlite storage type returns explicit error
  ("housekeeping only supports sqlite storage")
- [ ] `dpkms housekeeping prune --before 2025-01-01` with empty storage path
  returns "storage path not configured"

---

## Related Stories

- [US-0035](./US-0035-migrate-storage-backend.md) — Migrate to a different backend after export
- [US-0032](./US-0032-monitor-job-queue-health.md) — Confirm queue is idle before backup
- [US-0027](../admin/US-0027-configure-ai-provider.md) — Config location for storage path

---

## Personas

- [Operations](../../personas/operations.md)
- [Platform Engineer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/platform-engineer.md)

---

## E2E Tests

> Not yet implemented.
