# Persona: Operations / DevOps

**Primary Role:** Running dPKMS + `ctxt` in production or shared environments

---

## Goals

- Deploy and run the system reliably with minimal downtime
- Watch job and pipeline health
- Manage secrets securely (API keys, inbound auth tokens, federation tokens)
- Perform backups, upgrades, and recovery with confidence
- Keep storage and search fast

---

## Interaction Pattern

### Deployment

#### Local (Single User)
```bash
# SQLite backend (default); database at $CTXT_DATA_DIR/db.sqlite
export CTXT_DATA_DIR=~/.local/share/contexthelp

# Optional: hosted LLM for enrichment (read under its own name)
export OPENAI_API_KEY=sk-...

dpkms serve                  # REST on 127.0.0.1:8080, gRPC on 9090
dpkms serve --port 8080      # require that exact port
dpkms install --launchd      # or --systemd: run under the service manager
```

`dpkms serve` binds loopback only; `--public` (or `CH_PUBLIC`) allows remote
connections, and a non-private `server.access` requires inbound auth.

#### Containers

The supported container setup is the Docker Compose stack in the repo (dpkms
behind Caddy for TLS): [../deployment/docker.md](../deployment/docker.md). No
Kubernetes manifests or Helm chart ship with the project. For any orchestrator,
probe `GET /health` (`{"status":"ok"}`, 503 when the store is unhealthy).

### Configuration Management

#### Environment Variables (Container-Friendly)

Only an explicit list of variables maps to config keys; the full table is
in [../ctxt/cli-flag-binding.md](../ctxt/cli-flag-binding.md).

```bash
# Storage (the Postgres DSN itself is storage.path in the config file)
CH_STORAGE_TYPE=postgres              # sqlite, postgres
PGPASSWORD=...                        # read by the Postgres driver

# Listeners and workers
CH_SERVER_PORT=8080
CH_GRPC_PORT=9090
DPKMS_WORKERS=4                       # server.workers

# Jobs
DPKMS_MAX_RETRIES=3
DPKMS_STALE_TIMEOUT=30m

# AI provider keys (read under their own names)
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...

# Secrets backend
CTXT_SECRETS_BACKEND=keyring          # env, keyring, agefile, onepassword, ghsecrets
```

No env var sets the log level (pass `--verbose` for debug output), and
there are no metrics or tracing endpoints. Registry tokens are not env
vars: `ctxt registry login <name>` stores them in the OS keychain.

#### Configuration File (Complex Deployments)

`dpkms` reads `dpkms.yaml` (`/etc/contexthelp/dpkms.yaml`,
`$XDG_CONFIG_HOME/contexthelp/dpkms.yaml`, or the file `CTXT_CONFIG`
names). Values are literal: `${VAR}` is not expanded.

```yaml
# /etc/contexthelp/dpkms.yaml
storage:
  type: postgres
  path: postgres://ctxt@db.internal:5432/ctxt?sslmode=require

server:
  workers: 4
  access: protected                   # needs server.auth; see config reference

jobs:
  stale_timeout: 30m
  max_retries: 3
  drain_timeout: 30s

providers:
  llm:
    backend: openai                   # key from OPENAI_API_KEY
    model: gpt-4o

registries:
  - name: api1
    url: https://api1.example.com
    auth:
      type: bearer
    trust_level: untrusted

audit:
  syslog:
    enabled: true
    protocol: tcp
    address: siem.internal:514
```

Keys and structure: [../configuration-structure.md](../configuration-structure.md).
Inbound auth for `protected` / `public`:
[../manual/reference/config-and-permissions.md](../manual/reference/config-and-permissions.md#inbound-auth-roles-and-scopes).
Keep the `server.auth` tokens out of this file: write them literally in a
gitignored secrets file loaded as a second `--config`
([../deployment/docker.md#secrets](../deployment/docker.md#secrets),
`docker/dpkms-auth.example.yaml`).

### Monitoring & Observability

#### Health Checks
```bash
# Bare liveness: 200 {"status":"ok"}, 503 when the store is unhealthy
curl http://localhost:8080/health

# Verbose envelope: health, version, uptime_seconds, checks
# (process, rest_api, grpc_api, db, queue, watchers), upgrade progress.
# 503 only when health is "failed". Public instances return the bare
# verdict to unauthenticated callers.
curl http://localhost:8080/healthz

# Same envelope as a table; exit 0 healthy/degraded, 1 otherwise
dpkms healthcheck
dpkms healthcheck --watch --interval 5
```

`ctxt status` shows the same health from the client side.

#### Metrics, Logs, Tracing

- No metrics endpoint (no Prometheus exporter) and no tracing.
- Logs are human-readable text on stderr, Warn level by default; `-V` raises to
  debug, `-VV` to trace. Under `dpkms install` they land in
  `~/Library/Logs/dpkms/{out,err}.log` (macOS) or
  `~/.local/share/dpkms/dpkms.{out,err}.log` (Linux).
- Audit events can be forwarded to syslog or a webhook (`audit:` config); see
  [../operations/siem-integration.md](../operations/siem-integration.md).
- Job state: `dpkms job list`, `dpkms job status <id>`, `dpkms job log <id>`.

### Maintenance & Upgrades

#### Database Migrations

Schema migrations run automatically when dpkms opens the database (SQLite and
Postgres). There is no migrate command and no rollback; take a backup before
upgrading. Self-upgrades: `dpkms upgrade --help`.

#### Backup & Recovery
```bash
# Timestamped .tar.gz: SQLite snapshot, optionally blobs and config files + keys
dpkms backup --output-dir /var/backups/ctxt --include-blobs --include-configs
dpkms backup --async                    # enqueue as a background job

# Restore an archive or extracted directory (overwrites existing files)
dpkms restore ctxt-backup-2026-04-25T13-51-54Z.tar.gz
dpkms restore backup.tar.gz --data-dir /new/location
dpkms restore backup.tar.gz --skip-configs
```

- `dpkms backup` supports SQLite storage only; back up Postgres with its own tools
  (`pg_dump`).
- The output directory defaults to `backup.dir`, then the current directory; it
  must be empty.
- `--include-configs` takes the dpkms config file (`--config`, then `CTXT_CONFIG`,
  then `$XDG_CONFIG_HOME/contexthelp/dpkms.yaml`). That file carries the
  `federations:` targets, so restoring it restores the push topology.
- Client config backups are separate: `ctxt config backup` exports a signed
  bundle of the ctxt config, `ctxt config restore <bundle.zip>` restores it.

#### Federation Watermarks

Each push target has a `federation_watermarks` row (`federation_name`,
`last_change_seq`): the source's `objects.change_seq` of the last object the
target acknowledged. Every object insert and update takes the next
`change_seq`. For a local SQLite target the row lives in the target database;
for an HTTP target it lives in the source instance's database. A missing row
counts as 0, so a new or emptied target receives a full push on the next tick.

```sql
-- objects not yet pushed, per HTTP target (run against the source database)
SELECT w.federation_name,
       (SELECT COUNT(*) FROM objects o WHERE o.change_seq > w.last_change_seq) AS behind
FROM   federation_watermarks w
ORDER  BY behind DESC;
```

For a local target, read `last_change_seq` from the target database and count
the source's objects above it the same way.

- Expected: `behind` back to 0 within the target's `interval`
- Stale watermark: check target reachability and the dpkms log for push errors

#### Reindex and Housekeeping
```bash
dpkms housekeeping reindex      # rebuild FTS and vector indexes
dpkms housekeeping vacuum       # reclaim space
dpkms housekeeping compact      # optimize the database file
dpkms housekeeping prune --before 2024-01-01
dpkms housekeeping run          # WAL checkpoint, vacuum, orphaned blobs,
                                # embeddings compaction, job history >30d,
                                # zombie job detection, stats
```

#### Multi-Instance Lifecycle

Several dpkms instances can run on one machine. Give each its own port and, for
separate data, its own data directory (and config file via `CTXT_CONFIG` or
`--config`).

```bash
dpkms serve --name work     --port 8080 --data-dir ~/.local/share/contexthelp-work
dpkms serve --name personal --port 8081 --data-dir ~/.local/share/contexthelp-personal

dpkms ps                         # running instances from <data dir>/run/ pidfiles
dpkms shutdown --port 8081       # SIGTERM: drain in-flight jobs, exit
dpkms reboot --port 8081         # SIGHUP: drain and exit; a supervisor relaunches
```

- There is no `dpkms start` or per-profile instance selection; `serve` starts an
  instance and `--name` labels it (defaults to the database basename).
- `dpkms reboot` does not relaunch on its own: run under `dpkms install` (or
  another supervisor) so the process comes back.
- Pidfiles: `<data dir>/run/<port>.pid` (default data dir
  `$XDG_DATA_HOME/contexthelp`). `dpkms ps` removes stale entries.
- Clients pick an instance with `--instance`, `CTXT_INSTANCE` or
  `ctxt instance use`.

#### Plugins and Steps

There is no plugin install/load command; Go plugins are compiled in. External
pipeline steps are managed with `dpkms pipeline step list|install|uninstall` and
`dpkms pipeline step registry ...`.

### Scaling

#### Single Machine
- Increase `server.workers` / `DPKMS_WORKERS` / `--workers` (parallel jobs)
- Move from SQLite to Postgres (`storage.type: postgres`, DSN in `storage.path`)

#### Shared Postgres
- On Postgres, job claims use `FOR UPDATE SKIP LOCKED`, so workers do not pick
  the same job. Running several dpkms processes against one database is not a
  documented topology beyond that: each process also runs its own federation
  workers and reminders.

#### Registries
- Use `sync_mode: thin` on large registries (registry config)

---

## Key Pain Points

- **Storage Backend Choice:** SQLite for dev, Postgres for prod; `dpkms backup` covers SQLite only
- **AI Provider Dependency:** Hosted LLM rate limits, API downtime, cost spikes
- **Registry Availability:** Slow or unreachable registries slow sync
- **Job Queue Overload:** Too many pending jobs → worker backlog → user-facing latency
- **Schema Migration Risk:** Migrations run on open with no rollback
- **Observability Gaps:** No metrics or tracing; debugging relies on logs, `/healthz` and job logs

---

## System Leverage

### Transactional Job Queue
- Stale running jobs reset to pending after `jobs.stale_timeout` (`DPKMS_STALE_TIMEOUT`)
- Failed jobs retry up to `jobs.max_retries`; `dpkms job retry` reruns one by hand
- Job history with per-job logs (`dpkms job log`)
- Graceful drain on shutdown (`jobs.drain_timeout`)

### Storage Backends
- SQLite (default, zero install, FTS5)
- Postgres for shared deployments

### Registries
- Per-registry `trust_level` (`trusted`, `untrusted`, `sandboxed`) bounds what a registry may change
- `ctxt registry sync --reconcile --merge-strategy last-write-wins|trust-score` across registries

---

## User Stories

Operations teams interact with the system through these key stories:

### Configuration & Setup
- [US-0027](../stories/admin/US-0027-configure-ai-provider.md) — Configure AI Provider (hot-reload, cost tracking, health checks)
- [US-0029](../stories/admin/US-0029-install-and-enable-plugin.md) — Install and Enable Plugin (permission management)
- [US-0031](../stories/admin/US-0031-configure-encryption-and-secrets.md) — Configure Encryption and Secrets (key management, HSM)

### Monitoring & Watch
- [US-0208](../stories/capture/US-0208-temporal-watch.md) — Temporal Watch (change detection and alerting)

### Batch Processing
- [US-0008](../stories/ingestion/US-0008-batch-import-from-file.md) — Batch Import from File (bulk ingestion)
- [US-0015](../stories/enrichment/US-0015-batch-enrichment-with-progress.md) — Batch Enrichment with Progress (track large jobs)

### Monitoring & Observability
- [US-0032](../stories/operations/US-0032-monitor-job-queue-health.md) — Monitor Job Queue Health (queue metrics, latency)
- [US-0033](../stories/operations/US-0033-debug-failed-enrichment-job.md) — Debug Failed Enrichment Job (troubleshooting)

### Multi-Instance & Federation
- US-0321 — Multi-instance lifecycle (`serve --name`, `ps`, `shutdown`, `reboot`)
- US-0322 — Backup + federation rebuild (`dpkms backup --include-configs`; watermarks)

### Backup & Migration
- [US-0034](../stories/operations/US-0034-export-and-backup-all-knowledge.md) — Export and Backup All Knowledge (disaster recovery)
- [US-0035](../stories/operations/US-0035-migrate-storage-backend.md) — Migrate Storage Backend (SQLite → Postgres)

### Scaling & Performance
- [US-0036](../stories/operations/US-0036-scale-worker-pool-for-load.md) — Scale Worker Pool for Load (parallel processing)

### Import Operations
- [US-0301](../stories/ingestion/US-0301-import-chrome-bookmarks.md) — Import Chrome Bookmarks
- [US-0302](../stories/ingestion/US-0302-import-edge-bookmarks.md) — Import Edge Bookmarks
- [US-0303](../stories/ingestion/US-0303-import-firefox-bookmarks.md) — Import Firefox Bookmarks
- [US-0304](../stories/ingestion/US-0304-import-safari-bookmarks.md) — Import Safari Bookmarks
- [US-0305](../stories/ingestion/US-0305-import-google-drive.md) — Import Google Drive
- [US-0306](../stories/ingestion/US-0306-import-onedrive.md) — Import OneDrive
- [US-0307](../stories/ingestion/US-0307-import-notion.md) — Import Notion
- [US-0308](../stories/ingestion/US-0308-import-dropbox.md) — Import Dropbox
- [US-0309](../stories/ingestion/US-0309-import-slack.md) — Import Slack
- [US-0310](../stories/ingestion/US-0310-import-discord.md) — Import Discord
- [US-0311](../stories/ingestion/US-0311-import-obsidian-vault.md) — Import Obsidian Vault
- [US-0312](../stories/ingestion/US-0312-import-logseq-graph.md) — Import Logseq Graph
- [US-0313](../stories/ingestion/US-0313-import-evernote-enex.md) — Import Evernote ENEX
- [US-0314](../stories/ingestion/US-0314-import-pinboard-bookmarks.md) — Import Pinboard Bookmarks
- [US-0315](../stories/ingestion/US-0315-import-raindrop-bookmarks.md) — Import Raindrop Bookmarks
- [US-0316](../stories/ingestion/US-0316-import-twitter-archive.md) — Import Twitter Archive
- [US-0317](../stories/ingestion/US-0317-import-linkedin-export.md) — Import LinkedIn Export

---

## Success Metrics

- **Availability:** 99.9% (target)
- **Query latency:** P99 < 500ms (local), P99 < 2s (with registries)
- **Enrichment latency:** 95th percentile job completion < 5 minutes
- **Storage efficiency:** Cost per GB of knowledge stored
- **MTTR (Mean Time To Recovery):** < 5 minutes for common failure modes

---

## Collaboration with Other Personas

- **Maintainers:** Operations provide reliability feedback, suggest config improvements, report scaling bottlenecks
- **Agents/LLMs:** Operations ensure query latency SLOs, monitor AI provider costs
- **Knowledge Workers:** Operations monitor enrichment job completion rates, ensure fast search
- **Integrators:** Operations need clear deployment docs for pipelines and step registries, versioning strategy, safe rollback procedures
