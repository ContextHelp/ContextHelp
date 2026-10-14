---
title: Runbook
description: Keep ingestion, retrieval, and composition stable under load while minimizing recovery time for incidents.
---

## Goal

Keep ingestion, retrieval, and composition stable under load while minimizing recovery time for incidents.

## Scope

- Queue health and failure triage
- Service readiness checks
- Registry connectivity and sync health
- Pipeline/step governance checks
- Database maintenance and index health

## Primary stories

- `US-0032` to `US-0036`
- `US-0008`, `US-0015`
- `US-0300` to `US-0317`
- `US-0027`, `US-0029`, `US-0031`

## Baseline startup checks

Validate runtime/config:

```bash
ctxt version
dpkms version
ctxt config validate
```

Run server in dedicated terminal:

```bash
dpkms serve --port 8080 --grpc-port 9090 --workers 4
```

Ports and exit behaviour:

- Set a port with its flag (`--port`, `--grpc-port`, `--cookie-bridge-port`), env var (`CH_SERVER_PORT`, `CH_GRPC_PORT`, `CH_COOKIE_BRIDGE_PORT`) or config key (`server.port`, `server.grpc_port`, `server.cookie_bridge_port`). The flag wins over the env var, the env var over the config file.
- A port you set any of these ways is bound exactly. If it is in use, serve exits non-zero; it never moves to another port. `0` binds a port the OS picks; `dpkms ps --format json` shows it.
- With none of them set, a busy default port (8080, 9090, cookie bridge 9377) falls back to a free port. Serve prints a `note:` line with the port it bound; `dpkms ps` lists it.
- If any component fails (HTTP, gRPC, cookie bridge, workers), serve logs `error: <component>: ...`, stops every component and exits non-zero. launchd `KeepAlive` or systemd `Restart=always` then restarts it.
- SIGTERM or SIGINT stops serve within `jobs.drain_timeout` (default 30s). A second signal exits at once.

Health probe:

```bash
curl http://127.0.0.1:8080/health
```

## Daily checks

### 1. Queue health

```bash
ctxt job list --state pending --limit 50
ctxt job list --state running --limit 50
ctxt job list --state failed --limit 50
```

If failed jobs exist, drill in:

```bash
ctxt job status <job_id>
ctxt job log <job_id>
```

Retry if safe:

```bash
ctxt job retry <job_id>
```

### 2. Read-path smoke checks

```bash
ctxt find "daily health retrieval check" --limit 5
ctxt list --after 2026-02-01 --limit 10
```

### 3. Composition path smoke checks

```bash
ctxt make summary --since 2026-02-01 --output-file ops-daily-summary.md
```

### 4. Registry checks

```bash
ctxt registry list
ctxt registry sync <registry-name>
```

## Weekly checks

### 1. Maintenance operations

```bash
dpkms housekeeping vacuum
dpkms housekeeping reindex
dpkms housekeeping compact
```

Optional retention prune (policy-controlled):

```bash
dpkms housekeeping prune --before 2025-01-01
```

Prune deletes each object the way `DELETE /api/v1/objects/{id}` does: its edges, then the object, then the blob its content is stored in (content above `storage.blob.threshold`), unless another object still refers to that blob. A blob it can't delete is listed as an orphan blob; its object is deleted either way. `--dry-run` lists the objects and the blobs it would delete. `dpkms housekeeping run` purges blobs no object refers to once they are older than `--blob-grace` (default `1h`), orphans included.

### 2. Pipeline and step governance

```bash
dpkms pipeline list --include-archived
dpkms pipeline step list
dpkms pipeline step registry list
```

### 3. Registry update policy review

```bash
dpkms pipeline step registry autoupdate <registry-url> --enable
# or
dpkms pipeline step registry autoupdate <registry-url> --disable
```

## After upgrading dpkms

### Schema migrations snapshot the database first

The first open by a new binary (`dpkms serve`, or any `ctxt`/`dpkms` command that opens the store) applies pending schema migrations. On SQLite, before applying any, it copies the database to `backups/` beside the database file (`<data dir>/backups/` with the default `storage.path`):

```text
backups/pre-migrate-v40-v41-20261014T091500Z.sqlite
```

- **Name**: schema version before (`v40`) and after (`v41`), UTC time. Written with SQLite's online backup (`VACUUM INTO`), fsynced, owner-only permissions.
- **When**: only when migrations are pending on an existing database. No snapshot when the schema is current or the database is new.
- **No snapshot, no migration**: if the copy cannot be written (disk full, `backups/` not writable), the open fails with the reason and the database stays at its old version. Free space or fix permissions and start again.
- **Retention**: the newest 3 `pre-migrate-*` files per `backups/` directory are kept; older ones are deleted after a new snapshot succeeds. Other files in `backups/` are never touched.
- **Disk**: each snapshot is about the size of the database, so allow up to 3x the database size in `backups/`.
- **Blobs are not copied.** They are content-addressed and migrations never change them; the blob store stays as it is.
- **Turn it off**: `storage.migrate_snapshot: false` (or `-c storage.migrate_snapshot=false`) migrates without a copy.
- **Postgres** gets no snapshot. `dpkms` logs a warning naming both versions when it migrates an existing Postgres database: take a `pg_dump` before installing a new binary.

Roll back a migration that went wrong:

1. Stop every `dpkms serve` and `ctxt` process using the database.
2. Move `db.sqlite`, `db.sqlite-wal` and `db.sqlite-shm` aside.
3. Copy the snapshot to `db.sqlite`.
4. Reinstall the previous binary and start it. The snapshot is at the old schema; the new binary would migrate it again.

Objects written after the snapshot are not in it. Their blobs stay in the blob store, unreferenced.

### Index changes re-apply automatically

A release that changes how objects are indexed for full-text search is re-applied automatically: on its first start, `dpkms serve` prints

```text
warning: FTS signature mismatch: old=… new=… — reindex_auto: re-projecting stored objects
FTS re-projection to projection@v2: job <job-id> (follow with: ctxt upgrade status)
```

and re-projects every stored object in the background. Search keeps serving throughout; each object switches to its new index text as it is reached. Follow it:

```bash
ctxt upgrade status --watch   # bucket reindex_auto, target projection@vN
```

- **Stopped part-way** (shutdown, crash, `dpkms job cancel <job-id>`): the next start resumes with the objects not yet done. Nothing finished is redone.
- **`failed` with a `failed` count**: those objects could not be re-projected and keep their old index text. The next start retries them; `last_error` names the first one.
- **Embeddings are not refreshed** by this: stored vectors keep the text they were embedded from. See [Operate embedding models](./embeddings.md).
- `dpkms housekeeping reindex` rebuilds the SQLite FTS table from the stored text; it does not re-project.

## Incident response playbook

### Severity model

- `SEV-1`: data-loss risk, write-path outage, health endpoint failure.
- `SEV-2`: widespread ingestion/search degradation.
- `SEV-3`: isolated pipeline/step/registry issue with workaround.

### Triage sequence

1. Confirm health endpoint:

```bash
curl http://127.0.0.1:8080/health
```

2. Determine queue impact:

```bash
ctxt job list --state failed --limit 100
ctxt job list --state pending --limit 100
```

3. Sample failed job details:

```bash
ctxt job status <job_id>
ctxt job log <job_id>
```

4. Verify read path:

```bash
ctxt find "incident validation query" --limit 5
```

5. Verify registry blast radius:

```bash
ctxt registry list
ctxt registry info <registry-name>
ctxt registry sync <registry-name>
```

6. Apply scoped mitigation (retry, isolate registry changes, adjust workers, roll back pipeline changes).

### Post-incident checks

1. Re-run health, queue, and search smoke checks.
2. Confirm failed jobs are drained/retried or explicitly cancelled.
3. Record root cause, timeline, and follow-up actions.

## Capacity and scaling guidance

Increase worker capacity for sustained backlog:

```bash
dpkms serve --workers 8
```

Use backlog trend, not single snapshots, for scaling decisions.

## Change-management checklist

Before major config/pipeline changes:

1. `ctxt config validate`
2. Capture current pipeline/registry state (`dpkms pipeline list --include-archived`, `dpkms pipeline step registry list`)
3. Apply one change at a time.
4. Run smoke ingestion and search checks.
5. Log change + verification result.

## Data safety note

`dpkms backup --output-dir <dir>` writes a full SQLite archive (database, optionally blobs and config); `dpkms restore <archive>` reads it back. Schema migrations also snapshot the database on their own (see [Schema migrations snapshot the database first](#schema-migrations-snapshot-the-database-first)). For Postgres use `pg_dump` and validate restores externally.

## Cross-links

- [`../reference/api-cli-reference.md`](../reference/api-cli-reference.md)
- [`../reference/config-and-permissions.md`](../reference/config-and-permissions.md)
- [`../troubleshooting/faq.md`](../troubleshooting/faq.md)
- [`../admin-extensibility/pipeline-management.md`](../admin-extensibility/pipeline-management.md)
