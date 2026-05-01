---
status: shipped
---

# US-0035: Migrate Storage Backend

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Operations](../../personas/operations.md), [Maintainers](../../personas/maintainers.md)

---

## User Goal

As an operator, I want to migrate the knowledge base from one storage backend to
another (e.g., SQLite → Postgres) without data loss, so I can scale or change
infrastructure without re-ingesting content.

---

## Context

dPKMS storage is configured via `storage.type` (`sqlite` | future backends) and
`storage.path`. The `dpkms serve` command initialises the configured backend on
startup using `storageutil.NewDriver`. Migration requires: (1) export data from
the source backend, (2) configure the destination backend, (3) import the data,
(4) verify record counts match. Until a dedicated `dpkms migrate` command exists,
the canonical migration path is: backup the SQLite file (US-0034), provision the
new backend, replay via `dpkms serve` on the new config. The `GET /health` and
`GET /api/v1/objects` endpoints are the verification gates.

---

## Acceptance Criteria

- [ ] `storage.type` and `storage.path` config fields control which backend is used
- [ ] `dpkms serve` initialises the configured backend and logs the storage type
- [ ] `GET /health` returns `{"status":"ok"}` after server starts on new backend
- [ ] Object count on destination matches source after migration
- [ ] `dpkms housekeeping vacuum` / `compact` prepare source for clean export
- [ ] Migrated objects retain all fields: id, type, pipeline, tags, mentions,
  created_at, updated_at
- [ ] Jobs table is also migrated (operators can see historical job records)
- [ ] Server refuses to start if `storage.type` is unrecognised

---

## Implementation Notes

### Configuration

```yaml
# Source (SQLite)
storage:
  type: sqlite
  path: ~/.local/share/contexthelp/db.sqlite

# Destination (future Postgres example)
storage:
  type: postgres
  path: postgres://user:pass@localhost:5432/dpkms
```

### Migration Steps (pseudocode)

```
1. dpkms serve --config source-config.yaml  # confirm running
   GET /health → {"status":"ok"}
   GET /api/v1/objects?limit=1 → {"total": N}  # record N

2. dpkms housekeeping compact --config source-config.yaml  # clean export

3. # provision destination; set destination config
   dpkms serve --config dest-config.yaml  # starts fresh; Init() runs migrations

4. # import objects from source (tooling TBD; currently manual SQLite copy)
   cp source.sqlite dest.sqlite  # for sqlite→sqlite clone

5. GET /health (on dest)  → {"status":"ok"}
   GET /api/v1/objects?limit=1 (on dest) → {"total": N}  # must match
```

### serve flag → config mapping

| CLI flag          | Config key         | Notes                          |
|-------------------|--------------------|--------------------------------|
| `--port`          | `server.port`      | forwarded to HTTP bind address |
| `--workers`       | `server.workers`   | pool size in worker log line   |
| `--public`        | `server.public`    | bind 0.0.0.0 vs 127.0.0.1     |
| `--profile`       | `profile.default`  | stored in server config        |
| `--steps-path`    | `steps.path`       | logged on startup              |
| (storage via cfg) | `storage.type`     | logged as "Storage initialized"|

---

## E2E Test Checklist

### CLI flags present in server startup
- [ ] `dpkms serve --port 9000` binds HTTP listener on port 9000 (verify via
  `GET http://localhost:9000/health`)
- [ ] `dpkms serve --workers 8` logs "Worker pool started (8 workers)"
- [ ] `dpkms serve --public` binds on `0.0.0.0` not `127.0.0.1` (verify listener)
- [ ] `dpkms serve --profile myprofile` sets `profile.default` = "myprofile"
  (verifiable via config inspect or startup log)
- [ ] `dpkms serve --steps-path /tmp/steps` logs the steps path on startup

### Storage initialisation validation
- [ ] `dpkms serve` with `storage.type: sqlite` logs "Storage initialized"
- [ ] `dpkms serve` with unknown `storage.type` returns error and exits non-zero
- [ ] `GET /health` returns HTTP 200 after server fully initialises with new config

### Object integrity post-migration
- [ ] `GET /api/v1/objects` on destination returns same `total` as source before migration
- [ ] Spot-check: `GET /api/v1/objects/{id}` for a known ID returns identical
  `type`, `pipeline`, `tags`, `mentions`, `created_at` as on source
- [ ] `GET /api/v1/jobs` on destination returns historical job records
- [ ] FTS search (`GET /api/v1/search?q=...`) returns results on destination

### Housekeeping as pre-migration gate
- [ ] `dpkms housekeeping compact` completes without error before migration
- [ ] SQLite `PRAGMA integrity_check` on compacted file returns `ok`

### Error paths
- [ ] `dpkms serve` with inaccessible `storage.path` returns error "init storage: ..."
- [ ] `dpkms serve` with `storage.type: postgres` (not yet implemented) returns
  explicit unsupported-backend error (not a nil-pointer panic)

---

## Related Stories

- [US-0034](./US-0034-export-and-backup-all-knowledge.md) — Export/backup before migration
- [US-0036](./US-0036-scale-worker-pool-for-load.md) — Tune workers post-migration
- [US-0027](../admin/US-0027-configure-ai-provider.md) — Reconfigure providers post-migration

---

## Personas

- [Operations](../../personas/operations.md)
- [Platform Engineer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/platform-engineer.md)

---

## E2E Tests

- `test/integration/us0035_storage_migration_test.go::TestUS0035_ObjectCountIdenticalAfterMigration`
- `test/integration/us0035_storage_migration_test.go::TestUS0035_ObjectContentIdenticalAfterMigration`
- `test/integration/us0035_storage_migration_test.go::TestUS0035_EdgeCountIdenticalAfterMigration`
- `test/integration/us0035_storage_migration_test.go::TestUS0035_MigrationWithSecondSQLiteDestination`
