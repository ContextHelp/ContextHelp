# Skeleton 11 Goal: "Enterprise Scale"

## Package Focus

**Primary Package:** dPKMS (80%) + ctxt (20%)

Scale and enterprise features are almost entirely infrastructure concerns: multi-user storage backends, distributed workers, advanced ACL, audit trails, and the cloud platform layer. ctxt contributes SSO/SCIM integration and multi-tenant CLI configuration.

**Package Breakdown:**
- **dPKMS:** Postgres backend, rqlite backend, distributed workers, advanced ACL, audit trails, gRPC streaming
- **ctxt:** SSO/SCIM config, multi-tenant CLI, billing/marketplace surface

---

By the end of this skeleton:

1. **Multi-User Storage:** Teams can share a Postgres or rqlite backend with full data isolation and concurrent access.
2. **Distributed Workers:** Job execution scales horizontally using an external queue (e.g., NATS, Redis Streams) without modifying pipeline logic.
3. **Advanced ACL:** Per-object and per-namespace access control supports org-level sharing, read-only agents, and data-room patterns.
4. **Audit Trails:** Every mutation is logged with actor, timestamp, and change summary — satisfying compliance requirements.
5. **Cloud Platform (non-OSS track):** Multi-tenant admin, SSO/SCIM, billing/credits, and marketplace for paid registries and extensions.

---

## 1. dPKMS Storage Team (The Scaler)

Focus: Postgres and rqlite backends as production-grade alternatives to SQLite.
Why: Single-user SQLite cannot serve teams or horizontally scaled deployments.

### Task 1.1: Postgres Backend

- Implement all `ObjectStore`, `JobStore`, `EntityStore`, `AttachmentStore` interfaces against Postgres.
- Use `pgx/v5` — no ORM.
- Schema mirrors the SQLite schema; use `TEXT` UUIDs, `JSONB` for `mentions`/`metadata`, `TIMESTAMPTZ` everywhere.
- Full-text search: use `pg_trgm` + `GIN` index on `content` column as FTS5 equivalent.
- Vector search: integrate `pgvector` extension (`vector` column type, `<->` cosine operator).
- Migration runner: reuse the embedded migration system from Sprint 006 — separate SQL files under `migrations/postgres/`.
- Connection pool configuration:
  ```yaml
  storage:
    backend: postgres
    postgres:
      dsn: ${DATABASE_URL}
      max_open_conns: 20
      max_idle_conns: 5
      conn_max_lifetime: 5m
  ```
- Postgres stub in `internal/storage/postgres/` replaces the existing `ErrNotImplemented` stubs.

### Task 1.2: rqlite Backend

- Implement the same interface set against rqlite's HTTP API (`rqlite/gorqlite` client).
- rqlite = HA SQLite without Postgres; same SQL dialect, distributed via Raft consensus.
- No CGO required. Suitable for edge deployments needing HA without a full RDBMS.
- FTS5 works natively in rqlite; vector search uses the same `sqlite-vec` approach as the local backend.
- Configuration:
  ```yaml
  storage:
    backend: rqlite
    rqlite:
      endpoints:
        - http://node1:4001
        - http://node2:4001
      consistency: strong   # or: none, weak
  ```

### Task 1.3: gRPC Streaming for Ingestion and Retrieval

- Sprint 004 stubbed the gRPC server. This task completes it with streaming.
- Streaming RPCs to implement:
  - `StreamIngest(stream IngestRequest) → stream IngestProgress` — allows bulk ingestion with per-item progress.
  - `StreamSearch(SearchRequest) → stream SearchResult` — streams ranked results as they are computed.
  - `StreamJobEvents(JobID) → stream JobEvent` — replaces polling for job status.
- Backpressure: respect gRPC flow control; do not buffer entire result sets in memory.
- Auth: mTLS or JWT bearer (pluggable via interceptor).

---

## 2. dPKMS Worker Team (The Distributor)

Focus: Horizontal job execution via external queues.
Why: `dpkms serve` single-process polling cannot saturate multi-core or multi-node deployments.

### Task 2.1: External Queue Adapter Interface

- Define `QueueBackend` interface:
  ```go
  type QueueBackend interface {
      Enqueue(ctx context.Context, job Job) error
      Dequeue(ctx context.Context) (Job, AckFunc, error)
      Nack(ctx context.Context, job Job, err error) error
  }
  ```
- SQLite polling (existing) becomes `SqliteQueueBackend` — default, no config change required.
- New adapter: `RedisStreamsQueueBackend` (uses `redis/go-redis/v9`).
- New adapter: `NATSQueueBackend` (uses `nats-io/nats.go` with JetStream).
- Config:
  ```yaml
  workers:
    queue_backend: redis_streams   # or: sqlite (default), nats
    redis_streams:
      url: redis://localhost:6379
      stream: ctxt:jobs
      consumer_group: workers
    concurrency: 8
  ```

### Task 2.2: Worker Scaling and Health

- Workers register in a `worker_nodes` table (for rqlite/Postgres deployments) or a Redis SET (for Redis deployments).
- `ctxt workers` CLI command — lists active worker nodes, their queue depth, and last heartbeat.
- Dead worker detection: if heartbeat is stale > `2 × poll_interval`, jobs owned by that worker are re-queued.

---

## 3. dPKMS Security Team (The Enforcer)

Focus: Advanced ACL and audit trails for compliance environments.

### Task 3.1: Advanced ACL Model

- Extend the existing trust/permission model with per-resource access rules:
  ```yaml
  acl:
    default_policy: owner_only   # or: org_read, public_read
    rules:
      - resource: namespace:stripe.*
        allow: [agent:analyst, user:alice]
        deny: [user:bob]
  ```
- ACL checks are enforced at the storage layer — `ObjectStore.List()` and `ObjectStore.Get()` filter by caller identity.
- Identity propagation: gRPC context carries `principal` claim (JWT sub or mTLS CN).
- Read-only agent mode: principals tagged `role:agent` cannot call mutating methods unless explicitly granted.

### Task 3.2: Audit Trail

- Log every state-changing operation to an `audit_log` table:
  ```sql
  CREATE TABLE audit_log (
    id           TEXT PRIMARY KEY,
    principal    TEXT NOT NULL,
    action       TEXT NOT NULL,    -- "create", "update", "delete", "export", "sync"
    resource     TEXT NOT NULL,    -- "knowledge_object:<id>", "entity:<slug>", etc.
    changes      TEXT,             -- JSON diff
    ip_address   TEXT,
    occurred_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
  );
  ```
- Audit log is append-only — no UPDATE or DELETE permitted via application code.
- `ctxt audit` CLI: paginated read of audit log with filters (`--principal`, `--action`, `--since`).
- Retention policy: configurable age-based pruning via `dpkms housekeeping`.

---

## 4. Cloud Platform Team (The Host) — Non-OSS Track

Focus: context.help cloud — multi-tenant administration, identity lifecycle, and billing.
Why: OSS users run locally; cloud users need org management, SSO, and metered access.

### Task 4.1: Multi-Tenant Admin Interface

- Org-level isolation: each org has its own `tenant_id` prefix on all storage keys.
- Admin UI (React + Vite) for:
  - Managing org members, roles, and invitations.
  - Viewing per-org usage metrics (storage, API calls, job counts).
  - Managing node (dPKMS instance) registrations per org.
- REST API routes prefixed `/admin/orgs/<org_id>/`.

### Task 4.2: SSO / SCIM Onboarding

- SSO: OIDC provider integration (Google Workspace, Azure AD, Okta).
  - `ctxt login --sso` redirects to browser-based OIDC flow; stores access + refresh tokens in keychain.
  - JWT from OIDC provider used as gRPC bearer token.
- SCIM 2.0 endpoint (`/scim/v2/`) for automated user provisioning/deprovisioning from identity providers.
- Group sync: SCIM groups map to dPKMS ACL roles.

### Task 4.3: Billing, Credits, and Marketplace

- Credit system: org admins allocate credits to teams; credits are debited on paid registry access and cloud AI enrichment calls.
- Marketplace: UI surface for discovering and subscribing to paid registries and plugins.
  - Registry listing: name, namespaces, price tier, trial availability.
  - Plugin listing: type, permissions required, version, author.
- Billing webhooks: Stripe integration for subscription management.
- CLI: `ctxt marketplace search <topic>` — queries the marketplace API and displays results.

---

## 5. dPKMS Security Operations Team (The Compliance Officer)

Focus: Operational security monitoring, incident response infrastructure, and compliance documentation.
Why: Enterprise deployments require security event visibility, a practised incident response process, and documented compliance posture. These cannot be retrofitted after an incident.

### Task 5.1: Security Event Monitoring (SIEM-Ready Audit Log Export)

- Extend the `audit_log` table (Task 3.2) with structured JSON output suitable for ingestion by SIEM tools.
- Add `ctxt audit export --format json|cef|syslog` for batch export.
- Add syslog forwarding support:
  ```yaml
  audit:
    syslog:
      enabled: true
      protocol: udp         # or: tcp, tls
      address: siem.corp.example.com:514
      facility: local0
  ```
- For cloud deployments: add webhook export (POST JSON to configurable URL on each audit event).
- Document integration patterns for common SIEMs (Splunk, Elastic/ELK, Datadog) in `docs/operations/siem-integration.md`.

### Task 5.2: Security Alerting Hooks

- Define a `SecurityEventEmitter` interface that fires on:
  - Authentication failures (>3 in 60s from same principal → rate-limit alert)
  - ACL denials (>10 in 60s → anomaly alert)
  - Quota exhaustion events (from Sprint 010)
  - Signing verification failures (from Sprint 008)
  - Worker crash/restart loops
- Default handler: log to `audit_log` with `event_class=security`.
- Optional handlers (plugin-provided or config-driven):
  - Webhook POST to `security.alert_webhook_url`
  - Email via SMTP (for self-hosted deployments)
- Configuration:
  ```yaml
  security:
    alerts:
      auth_failure_threshold: 3
      acl_denial_threshold: 10
      webhook_url: ${CTXT_SECURITY_WEBHOOK}
  ```

### Task 5.3: Incident Response Runbook

- Create `docs/operations/incident-response.md` covering:
  - **Compromised secret:** revoke → rotate → assess exposure → document (with CLI commands for each step).
  - **Secret found in git history:** `git filter-repo` procedure + force-push coordination + team notification template.
  - **Unauthorized API access detected:** audit log query → ACL lockdown → key rotation sequence.
  - **Plugin behaving maliciously:** disable plugin → review audit log → graph integrity check → restore from signed backup.
- Each scenario includes: detection signals, immediate actions (< 1 hour), follow-up actions (< 24 hours), and post-incident review checklist.
- Runbook is linked from SECURITY.md and the production deployment checklist.

### Task 5.4: Compliance Documentation (GDPR / SOC 2 Stubs)

- Create `docs/operations/compliance.md` covering:
  - **Data residency:** all user data stays local by default (local-first principle); cloud deployments document data boundaries per region.
  - **PII handling:** knowledge objects may contain PII; document what is stored, retention defaults, and how to purge (`ctxt object delete --all --before <date>`).
  - **Right to erasure:** document the purge path for GDPR article 17 requests.
  - **SOC 2 control mapping:** map existing features (audit log, ACL, encryption, signed bundles) to SOC 2 Trust Service Criteria as a readiness assessment stub.
  - **Third-party dependency assessment:** document process for evaluating new dependencies (govulncheck + nancy + manual review checklist).
- This is documentation, not code. No new features required.
- Link from SECURITY.md under "Compliance & Regulatory Requirements".

---

## The Integration Check (The Demo)

### Scenario: "Multi-User Team Deployment"

1. Org admin deploys dPKMS with Postgres backend and Redis queue:
   ```
   docker compose up -d
   ```
   - Migrations run automatically.
   - Two worker replicas start consuming from Redis Streams.

2. Alice and Bob join via SSO (Google Workspace):
   ```
   ctxt login --sso --org acme
   ```

3. Alice ingests 500 documents via `StreamIngest` gRPC:
   - Workers process in parallel.
   - All mutations appear in `audit_log` with `principal=alice@acme.com`.

4. Bob searches with mention filter:
   - ACL enforces `namespace:stripe.*` is read-only for Bob's role.
   - Bob sees results but cannot enrich or delete.

5. Admin checks audit trail:
   ```
   ctxt audit --since 24h --action delete
   ```
   - Zero deletions — integrity confirmed.

6. Quota usage checked:
   ```
   ctxt registry usage --org acme
   ```
   - Metered paid registry calls visible across all org members.

---

## Risks to Watch For

- **Postgres schema drift:** Migrations must be tested against both SQLite and Postgres in CI from this point forward.
- **rqlite consistency model:** `strong` consistency adds latency; document tradeoffs clearly.
- **gRPC streaming backpressure:** Unbounded result sets can OOM workers — enforce server-side page limits.
- **External queue at-least-once delivery:** Pipeline steps must be idempotent; duplicate job delivery must not create duplicate knowledge objects.
- **SCIM provisioning race:** Group sync must be atomic — partial sync must not leave users in inconsistent ACL state.
- **Audit log growth:** High-throughput deployments may generate millions of rows; partition by month in Postgres.

---

## See Also

**Package Boundaries:**
- [CROSS-PACKAGE-CONTRACTS.md](CROSS-PACKAGE-CONTRACTS.md) - dPKMS ↔ ctxt integration points
- [../branding.md](../branding.md) - Naming conventions (dPKMS vs ctxt vs ContextHelp)
- [../dpkms-or-ctxt.md](../dpkms-or-ctxt.md) - Package placement guide

**Configuration:**
- [CONFIGURATION-STRUCTURE.md](CONFIGURATION-STRUCTURE.md) - Config file organization
- [../ctxt/configuration.md](../ctxt/configuration.md) - Focus profiles & preferences

**Architecture:**
- [../architecture.md](../architecture.md) - System architecture overview
- [../../ROADMAP.md](../../ROADMAP.md) - Living skeleton roadmap
- [README.md](README.md) - Sprint documentation index
