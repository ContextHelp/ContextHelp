# Scaling

What the dpkms server actually offers for sizing a deployment. There is no
load-balancer, cache tier, metrics exporter or multi-region mode in the code;
this page covers the knobs that exist.

---

## Topology

A deployment is one `dpkms serve` process per instance (HTTP API, gRPC API and
job workers in the same process) and any number of `ctxt` clients talking to it
over HTTP. Both binaries read their own config file (`dpkms.yaml`, `ctxt.yaml`);
see [ctxt/configuration.md](ctxt/configuration.md).

| Listener | Default | Config key | Env var | Flag |
|----------|---------|------------|---------|------|
| HTTP (`/api/v1`, `/healthz`, `/ui`) | 8080 | `server.port` | `CH_SERVER_PORT` | `--port` |
| gRPC | 9090 | `server.grpc_port` | `CH_GRPC_PORT` | `--grpc-port` |
| Browser-extension cookie bridge (loopback only) | 9377 | `server.cookie_bridge_port` | `CH_COOKIE_BRIDGE_PORT` | `--cookie-bridge-port` |

Remote clients need `server.access: protected` or `public` plus static bearer
tokens. See [api/api-rest.md](api/api-rest.md#authentication) and
[deployment/docker.md](deployment/docker.md) for a TLS-fronted setup.

---

## Storage Scaling

Two storage drivers exist (`internal/storageutil/factory.go`). Choose one with
`storage.type` or `CH_STORAGE_TYPE`; `storage.path` holds the SQLite file or the
PostgreSQL connection string.

| `storage.type` | Notes |
|----------------|-------|
| `sqlite` (default) | Single file at `<data dir>/db.sqlite`. Opened in WAL mode with a 5 s busy timeout. FTS5 for text search, `sqlite-vec` for vectors. |
| `postgres` | Connection pool capped at 25 open / 5 idle connections. Requires the `pgvector` extension (`CREATE EXTENSION vector`); the driver fails with an explicit error when it is missing. |

```yaml
storage:
  type: postgres
  path: "postgres://ctxt:<password>@db.internal:5432/ctxt?sslmode=require"
```

Large content goes to the blob store, not the database: `storage.blob.backend`
is `local` or `s3`, with `storage.blob.threshold` setting the size cutoff. See
[environment-variables/storage.md](environment-variables/storage.md) and
[dpkms/storage.md](dpkms/storage.md).

There is no read-replica, sharding or partitioning support.

---

## Worker Scaling

The job queue lives in the storage backend itself (a `jobs` table), not in a
separate broker. Workers run inside `dpkms serve`.

| Setting | Default | Config key | Env var / flag |
|---------|---------|------------|----------------|
| Worker count | 4 | `server.workers` | `DPKMS_WORKERS`, `--workers` |
| Idle poll interval | 500ms | `jobs.poll_interval` (min 50ms) | `DPKMS_POLL_INTERVAL` |
| Stale-job timeout | 30m | `jobs.stale_timeout` | `DPKMS_STALE_TIMEOUT` |
| Fan-out retry limit | 3 | `jobs.max_retries` | `DPKMS_MAX_RETRIES` |
| Max pipeline hops | 5 | `jobs.max_hops` | `DPKMS_MAX_HOPS` |
| Shutdown drain timeout | 30s | `jobs.drain_timeout` | - |

On PostgreSQL, job claims use `SELECT ... FOR UPDATE SKIP LOCKED`, so
concurrent workers never claim the same job. On SQLite all writes go through a
single writer, so adding workers mainly helps I/O-bound pipelines (network
fetches, remote LLM calls).

Inspect the queue with `dpkms job list` / `dpkms job status <id>`. See [environment-variables/workers.md](environment-variables/workers.md)
and [dpkms/jobs-and-ingestion.md](dpkms/jobs-and-ingestion.md).

---

## Network & API Scaling

dpkms has no built-in global rate limiter. On non-private instances,
`server.quotas` sets per-principal caps on metered events (`entity_resolve`,
`content_pull`, `taxonomy_sync`); an exhausted quota returns HTTP 429
`QUOTA_EXHAUSTED`. Put a reverse proxy (the Docker setup ships Caddy) in front
for TLS and any request-rate limiting you need.

---

## Backup

`dpkms backup` writes a `.tar.gz` with a SQLite snapshot (plus blobs with
`--include-blobs`, configs and keys with `--include-configs`); `dpkms restore`
reads it back. It supports SQLite only and refuses other storage types. For
PostgreSQL, use the database's own tooling (`pg_dump`, WAL archiving).

---

## Related

- [environment-variables/](environment-variables/) - env var reference
- [api/](api/) - REST and gRPC API
- [deployment/docker.md](deployment/docker.md) - self-hosted Docker deployment
- [architecture.md](architecture.md) - component overview
