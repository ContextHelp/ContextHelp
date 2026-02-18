# Persona: Operations / DevOps

**Primary Role:** Running dPKMS + `ctxt` in production or shared environments

---

## Goals

- Deploy and scale the system reliably with minimal downtime
- Monitor jobs, queries, and enrichment pipeline health continuously
- Manage secrets securely (API keys, encryption keys, federation credentials)
- Perform backups, upgrades, and disaster recovery with confidence
- Optimize storage and query performance for user experience

---

## Interaction Pattern

### Deployment

#### Local Development (Single User)
```bash
# SQLite backend (default, zero-install)
export CH_STORAGE_TYPE=sqlite
export CH_STORAGE_PATH=./data/db.sqlite

# Optional: OpenAI for NLQ + enrichment
export CH_OPENAI_API_KEY=sk-...
export CH_OPENAI_MODEL=gpt-4

dpkms serve --listen localhost:8080
```

#### Containerized Production (Team/Organization)
```dockerfile
FROM golang:1.23-alpine
RUN apk add --no-cache sqlite postgresql-client
COPY dpkms /usr/local/bin/
EXPOSE 8080
CMD ["dpkms", "serve"]
```

```bash
docker run -e CH_STORAGE_TYPE=postgres \
  -e CH_STORAGE_URL=postgres://user:pass@db:5432/ctxt \
  -e CH_OPENAI_API_KEY=${OPENAI_KEY} \
  -e CH_ENCRYPTION_KEY_PROVIDER=vault \
  -e CH_VAULT_ADDR=http://vault:8200 \
  -p 8080:8080 \
  dpkms:latest
```

#### Kubernetes Production
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dpkms
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: dpkms
        image: dpkms:v0.1.0
        env:
        - name: CH_STORAGE_TYPE
          value: postgres
        - name: CH_STORAGE_URL
          valueFrom:
            secretKeyRef:
              name: db-credentials
              key: postgres-url
        - name: CH_OPENAI_API_KEY
          valueFrom:
            secretKeyRef:
              name: ai-credentials
              key: openai-key
        ports:
        - containerPort: 8080
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
```

### Configuration Management

#### Environment Variables (Container-Friendly)
```bash
# Storage
CH_STORAGE_TYPE=postgres              # sqlite, postgres
CH_STORAGE_URL=postgres://...         # for postgres

# AI Provider
CH_OPENAI_API_KEY=sk-...
CH_OPENAI_MODEL=gpt-4o

# Encryption
CH_ENCRYPTION_KEY_PROVIDER=vault      # vault, kms, file
CH_VAULT_ADDR=http://vault:8200

# Registry Federation
CH_REGISTRY_URLS=https://api1.example.com,https://api2.example.com
CH_REGISTRY_TOKENS=token1,token2      # one per registry

# Observability
CH_LOG_LEVEL=info                     # debug, info, warn, error
CH_METRICS_ENABLED=true
CH_METRICS_PORT=9090
CH_JAEGER_ENDPOINT=http://jaeger:14268/api/traces

# Performance
CH_JOB_WORKERS=4                      # parallel job executors
CH_JOB_TIMEOUT=30m                    # per-job timeout
CH_CACHE_SIZE_MB=512                  # in-memory query result cache
```

#### Configuration File (Complex Deployments)
```yaml
# config.yaml
storage:
  type: postgres
  url: ${CH_STORAGE_URL}
  pool:
    minConnections: 5
    maxConnections: 20
  migrations:
    autoRun: true
    logAll: true

aiProvider:
  type: openai
  model: gpt-4o
  apiKey: ${CH_OPENAI_API_KEY}
  rateLimit: 100/min
  timeout: 30s

encryption:
  provider: vault
  vaultAddr: ${CH_VAULT_ADDR}
  keyRotation: 90d

registries:
  - name: api1
    url: https://api1.example.com
    token: ${REGISTRY_TOKEN_1}
    timeout: 10s
    weight: 0.6

observability:
  logging:
    level: info
    format: json
  metrics:
    enabled: true
    port: 9090
  tracing:
    jaegerEndpoint: ${CH_JAEGER_ENDPOINT}
    sampleRate: 0.1

plugins:
  - path: ./plugins/code-metrics.so
    permissions:
      subprocess: true

performance:
  jobWorkers: 4
  jobTimeout: 30m
  cacheSize: 512MB
  ftsIndexing: parallel
```

### Monitoring & Observability

#### Health Checks
```bash
# Liveness (is process running?)
curl http://localhost:8080/health
# → {"status":"ok","uptime":"2h30m"}

# Readiness (can handle requests?)
curl http://localhost:8080/ready
# → {"status":"ready","storageHealthy":true,"registriesHealthy":true}
```

#### Metrics (Prometheus)
```
# Job queue
ch_jobs_pending{env="prod"}        # how many jobs waiting
ch_jobs_running{env="prod"}         # how many jobs executing
ch_jobs_completed_total{env="prod"} # cumulative completed
ch_jobs_failed_total{env="prod"}    # cumulative failed
ch_job_duration_seconds{env="prod"} # histogram of execution times

# Query performance
ch_search_duration_seconds{strategy="metadata",env="prod"}   # SQL query time
ch_search_duration_seconds{strategy="fts",env="prod"}        # FTS query time
ch_search_duration_seconds{strategy="vector",env="prod"}     # Vector search time
ch_search_duration_seconds{strategy="graph",env="prod"}      # Graph traversal time
ch_search_results_merged{env="prod"}                         # results before dedup/reranking

# Storage
ch_storage_objects{env="prod"}      # total knowledge objects
ch_storage_entities{env="prod"}     # total entities
ch_storage_edges{env="prod"}        # total graph edges
ch_storage_size_bytes{env="prod"}   # database size

# AI Provider
ch_ai_calls_total{provider="openai",env="prod"}
ch_ai_errors_total{provider="openai",env="prod"}
ch_ai_tokens_used{provider="openai",model="gpt-4o",env="prod"}
ch_ai_cost_usd{provider="openai",env="prod"}

# Registry
ch_registry_queries_total{registry="api1",env="prod"}
ch_registry_errors_total{registry="api1",env="prod"}
ch_registry_latency{registry="api1",env="prod"}
```

#### Logging
```
# Structured JSON logs
{
  "timestamp":"2025-01-18T10:30:45Z",
  "level":"info",
  "component":"job.executor",
  "job_id":"j-abc123",
  "pipeline":"text.short",
  "status":"completed",
  "duration_ms":1234,
  "steps_executed":5,
  "objects_written":1
}

{
  "timestamp":"2025-01-18T10:31:02Z",
  "level":"info",
  "component":"search.executor",
  "query":"type==decision",
  "strategies":["metadata","graph"],
  "total_results":42,
  "merged_results":38,
  "duration_ms":567
}

{
  "timestamp":"2025-01-18T10:32:15Z",
  "level":"error",
  "component":"ai.provider",
  "provider":"openai",
  "error":"rate_limit_exceeded",
  "retry_after_seconds":60
}
```

#### Distributed Tracing (Jaeger)
- Trace ingestion job: capture → pipeline selection → step execution → storage write
- Trace search query: query parsing → strategy selection → parallel execution → merging → reranking
- Trace composition: object retrieval → graph traversal → template application → rendering

### Maintenance & Upgrades

#### Database Migrations
```bash
# List available migrations
dpkms migrate --list

# Run all pending migrations (idempotent)
dpkms migrate --up

# Rollback to specific version
dpkms migrate --rollback-to v0.0.5

# Check current schema version
dpkms migrate --status
```

#### Backup & Recovery
```bash
# Export all data (portable format)
dpkms export --format ndjson > backup-2025-01-18.ndjson

# Import from backup
dpkms import < backup-2025-01-18.ndjson

# Incremental snapshot (for large deployments)
dpkms export-delta --since 2025-01-17T00:00:00Z > delta.ndjson
```

#### Reindex (After Schema Changes)
```bash
# Rebuild FTS index (offline safe)
dpkms reindex --fts

# Rebuild vector index (requires embeddings)
dpkms reindex --vector

# Rebuild graph index
dpkms reindex --graph
```

#### Plugin Management
```bash
# List installed plugins
dpkms plugins list

# Load new plugin
dpkms plugins install ./plugins/code-metrics.so

# Unload plugin (does not remove)
dpkms plugins unload code-metrics

# Verify plugin safety (sandboxing, permissions)
dpkms plugins verify ./plugins/code-metrics.so
```

### Scaling Strategies

#### Vertical Scaling (Single Machine)
- Increase `CH_JOB_WORKERS` (parallel enrichment)
- Increase `CH_CACHE_SIZE_MB` (query result cache)
- Upgrade storage backend from SQLite to Postgres
- Tune database parameters (connection pooling, query cache)

#### Horizontal Scaling (Multiple Machines)
- Deploy multiple `dpkms` instances behind load balancer
- Share Postgres database (all instances read/write)
- Distribute jobs across worker pool (transactional outbox handles distribution)
- Scale registries independently (each registry behind its own load balancer)

#### Federation Optimization
- Add registry weight filters: `CH_REGISTRY_WEIGHT_THRESHOLD=0.5`
- Configure per-registry timeouts: `CH_REGISTRY_TIMEOUT=5s`
- Batch registry queries when possible
- Cache registry results aggressively

---

## Key Pain Points

- **Storage Backend Choice:** SQLite for dev, Postgres for prod; migration path is tedious
- **Encryption Key Management:** Key rotation without downtime, HSM integration
- **AI Provider Dependency:** OpenAI rate limits, API downtime, cost spikes
- **Registry Availability:** Federated queries slow if registries are slow; cascading failures
- **Job Queue Overload:** Too many pending jobs → worker backlog → user-facing latency
- **Schema Migration Risk:** Upgrading schema while serving traffic (zero-downtime migration hard)
- **Plugin Compatibility:** Plugins may break across core versions; no automated dependency tracking
- **Observability Gaps:** Hard to debug slow queries or failed enrichments without full tracing

---

## System Leverage

### Transactional Job Queue
- Automatic recovery from worker crashes (stale jobs reset to pending)
- Idempotent job execution (safe to retry)
- Full job history with error logs
- No data loss (outbox pattern ensures writes before acknowledgment)

### Pluggable Storage Backends
- Start with SQLite (zero install, WAL mode, FTS5)
- Switch to Postgres when scaling (shared database, replication, connection pooling)
- Support for other backends (MySQL, DynamoDB) via plugin interface

### Deterministic Replay
- Same query + same config → same results
- Facilitates debugging, auditing, capacity planning
- Enables result caching (query as cache key)

### Federated Registries
- Distribute load across multiple registry sources
- Conflict resolution rules prevent data corruption
- Registry weighting guides resource allocation

### Distributed Tracing
- End-to-end visibility: ingestion → enrichment → storage → search → ranking
- Debug slow operations (which strategy is slow?)
- Capacity planning (which components are bottlenecks?)

---

## User Stories

Operations teams interact with the system through these key stories:

### Configuration & Setup
- [US-0027](../stories/admin/US-0027-configure-ai-provider.md) — Configure AI Provider (hot-reload, cost tracking, health checks)
- [US-0029](../stories/admin/US-0029-install-and-enable-plugin.md) — Install and Enable Plugin (permission management)
- [US-0031](../stories/admin/US-0031-configure-encryption-and-secrets.md) — Configure Encryption and Secrets (key management, HSM)

### Batch Processing
- [US-0008](../stories/ingestion/US-0008-batch-import-from-file.md) — Batch Import from File (bulk ingestion)
- [US-0015](../stories/enrichment/US-0015-batch-enrichment-with-progress.md) — Batch Enrichment with Progress (track large jobs)

### Monitoring & Observability
- [US-0032](../stories/operations/US-0032-monitor-job-queue-health.md) — Monitor Job Queue Health (queue metrics, latency)
- [US-0033](../stories/operations/US-0033-debug-failed-enrichment-job.md) — Debug Failed Enrichment Job (troubleshooting)

### Backup & Migration
- [US-0034](../stories/operations/US-0034-export-and-backup-all-knowledge.md) — Export and Backup All Knowledge (disaster recovery)
- [US-0035](../stories/operations/US-0035-migrate-storage-backend.md) — Migrate Storage Backend (SQLite → Postgres)

### Scaling & Performance
- [US-0036](../stories/operations/US-0036-scale-worker-pool-for-load.md) — Scale Worker Pool for Load (parallel processing)

---

## Success Metrics

- **Availability:** 99.9% (target)
- **Query latency:** P99 < 500ms (local), P99 < 2s (with registries)
- **Enrichment latency:** 95th percentile job completion < 5 minutes
- **Storage efficiency:** Cost per GB of knowledge stored
- **Plugin reliability:** Zero critical plugin crashes per week
- **MTTR (Mean Time To Recovery):** < 5 minutes for common failure modes

---

## Collaboration with Other Personas

- **Maintainers:** Operations provide reliability feedback, suggest config improvements, report scaling bottlenecks
- **Agents/LLMs:** Operations ensure query latency SLOs, monitor AI provider costs
- **Knowledge Workers:** Operations monitor enrichment job completion rates, ensure fast search
- **Integrators:** Operations need clear plugin deployment docs, versioning strategy, safe rollback procedures
