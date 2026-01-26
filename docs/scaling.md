# Scaling & Deployment

Scale boundaries and deployment patterns for **dPKMS + `ctxt`**.

---

## Scaling Philosophy

**dPKMS + `ctxt` is designed for:**
- **Local-first by default** — Single-user, sovereign operation
- **Selective scaling** — Scale only what you need
- **Horizontal growth** — Add workers, not bigger machines
- **Graceful degradation** — Works offline, syncs later

**Not designed for:**
- Massive multi-tenancy (thousands of concurrent users)
- Real-time collaboration at scale
- Global distribution with strong consistency

---

## Deployment Topologies

### 1. Personal (Local-First)

**Best for:** Individual users, sovereign operation

```
┌─────────────────────────────┐
│   Single Machine            │
│  ┌──────────┐  ┌──────────┐│
│  │  ctxt    │  │  dpkms   ││
│  │  (CLI)   │  │ (worker) ││
│  └──────────┘  └──────────┘│
│         │           │       │
│  ┌──────────────────────┐  │
│  │   SQLite Database    │  │
│  └──────────────────────┘  │
└─────────────────────────────┘
```

**Configuration:**
```yaml
storage:
  type: sqlite
  path: ~/.local/share/contexthelp/data.db

queue:
  backend: sqlite
  workers: 4
```

**Capacity:**
- **Objects:** 1M+ knowledge objects
- **Storage:** 10-50GB typical
- **Workers:** 2-8 background workers
- **Throughput:** 100-500 ingestions/hour

---

### 2. Team (Shared Database)

**Best for:** Small teams (5-20 users), shared knowledge

```
┌────────────┐  ┌────────────┐  ┌────────────┐
│ User 1     │  │ User 2     │  │ User 3     │
│ ctxt CLI   │  │ ctxt CLI   │  │ ctxt CLI   │
└─────┬──────┘  └─────┬──────┘  └─────┬──────┘
      │               │               │
      └───────────────┼───────────────┘
                      │
              ┌───────▼────────┐
              │  dPKMS Server  │
              │   (API + gRPC) │
              └───────┬────────┘
                      │
        ┌─────────────┼─────────────┐
        │             │             │
    ┌───▼────┐  ┌────▼───┐  ┌──────▼──┐
    │Postgres│  │ Redis  │  │ Qdrant  │
    │(Storage)│  │(Queue) │  │(Vectors)│
    └────────┘  └────────┘  └─────────┘
```

**Configuration:**
```yaml
storage:
  type: postgres
  host: db.internal
  max_connections: 50

queue:
  backend: redis
  workers: 8

server:
  rest_api: 0.0.0.0:8080
  grpc_api: 0.0.0.0:9090
```

**Capacity:**
- **Users:** 5-20 concurrent
- **Objects:** 10M+ knowledge objects
- **Storage:** 100GB-1TB
- **Workers:** 8-16 background workers
- **Throughput:** 1K-5K ingestions/hour

---

### 3. Organization (Multi-Instance)

**Best for:** Organizations, multiple teams, departments

```
                 ┌──────────────┐
                 │ Load Balancer│
                 └──────┬───────┘
                        │
        ┌───────────────┼───────────────┐
        │               │               │
   ┌────▼────┐    ┌────▼────┐    ┌────▼────┐
   │ dPKMS 1 │    │ dPKMS 2 │    │ dPKMS 3 │
   │(Workers)│    │(Workers)│    │(Workers)│
   └────┬────┘    └────┬────┘    └────┬────┘
        │               │               │
        └───────────────┼───────────────┘
                        │
        ┌───────────────┼───────────────┐
        │               │               │
    ┌───▼────┐    ┌────▼───┐    ┌──────▼──┐
    │Postgres│    │ Redis  │    │ Qdrant  │
    │(Primary)│    │Cluster │    │ Cluster │
    └────┬───┘    └────────┘    └─────────┘
         │
    ┌────▼────┐
    │Postgres │
    │(Replica)│
    └─────────┘
```

**Configuration:**
```yaml
storage:
  type: postgres
  host: db-primary.internal
  replicas:
    - db-replica-1.internal
    - db-replica-2.internal
  max_connections: 100
  read_write_split: true

queue:
  backend: redis
  cluster: true
  workers_per_instance: 16

server:
  instances: 3
  rest_api: 0.0.0.0:8080
  grpc_api: 0.0.0.0:9090
```

**Capacity:**
- **Users:** 50-200 concurrent
- **Objects:** 100M+ knowledge objects
- **Storage:** 1TB-10TB
- **Workers:** 48+ total (16 per instance)
- **Throughput:** 10K+ ingestions/hour

---

## Scale Dimensions

### Horizontal Scaling

**What scales horizontally:**
- ✅ Background workers (add more)
- ✅ API servers (stateless)
- ✅ Job processing (queue-based)
- ✅ Read replicas (PostgreSQL)

**What doesn't scale horizontally:**
- ❌ SQLite (single-writer limitation)
- ❌ In-memory queue (process-local)
- ❌ File-based storage

**How to scale:**
```yaml
# Add more workers
queue:
  workers: 32  # Increase from 4

# Add more API instances
# Run multiple dpkms instances behind load balancer

# Add read replicas
storage:
  replicas: [replica-1, replica-2, replica-3]
```

---

### Vertical Scaling

**When to scale vertically:**
- Large embedding models (16GB+ RAM)
- Heavy pipeline processing
- Large batch operations

**Resource recommendations:**

| Tier | vCPU | RAM | Storage | Use Case |
|------|------|-----|---------|----------|
| Small | 2 | 4GB | 20GB | Personal, <100K objects |
| Medium | 4 | 8GB | 100GB | Team, <1M objects |
| Large | 8 | 16GB | 500GB | Department, <10M objects |
| XLarge | 16 | 32GB | 2TB | Organization, 10M+ objects |

---

## Storage Scaling

### SQLite Limits

**Practical limits:**
- **Objects:** 1M-5M (with good indexing)
- **Database size:** 50GB-100GB
- **Concurrent writers:** 1 (WAL mode)
- **Concurrent readers:** Unlimited (WAL mode)

**When to migrate to PostgreSQL:**
- >1M objects with heavy writes
- Multiple concurrent users
- Need for horizontal scaling
- Advanced query features

### PostgreSQL Scaling

**Configuration for scale:**
```sql
-- Increase connection limits
max_connections = 200
shared_buffers = 4GB
effective_cache_size = 12GB
work_mem = 64MB

-- Optimize for writes
wal_buffers = 16MB
checkpoint_completion_target = 0.9

-- Partitioning for large tables
CREATE TABLE objects (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP
) PARTITION BY RANGE (created_at);

CREATE TABLE objects_2024_q1 PARTITION OF objects
    FOR VALUES FROM ('2024-01-01') TO ('2024-04-01');
```

**Sharding strategy** (future):
- Shard by user ID
- Shard by workspace
- Shard by time range

---

## Worker Scaling

### Job Queue Scaling

**Worker pool configuration:**
```yaml
queue:
  workers:
    high: 8     # High priority
    normal: 16  # Normal priority
    low: 4      # Low priority
```

**Scaling guidelines:**
- **CPU-bound tasks** — Workers ≈ CPU cores
- **I/O-bound tasks** — Workers = 2-4× CPU cores
- **Mixed workload** — Start with 2× CPU cores

**Monitoring:**
```bash
# Check queue depth
dpkms queue stats

# Check worker utilization
dpkms workers stats
```

---

## Network & API Scaling

### Load Balancing

**nginx configuration:**
```nginx
upstream dpkms {
    least_conn;
    server dpkms-1:8080 max_fails=3 fail_timeout=30s;
    server dpkms-2:8080 max_fails=3 fail_timeout=30s;
    server dpkms-3:8080 max_fails=3 fail_timeout=30s;
}

server {
    listen 80;
    location / {
        proxy_pass http://dpkms;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

### Rate Limiting

**Application-level:**
```yaml
api:
  rate_limits:
    analyze: 100/minute
    search: 1000/minute
    global: 10000/hour
```

**nginx-level:**
```nginx
limit_req_zone $binary_remote_addr zone=api:10m rate=10r/s;

location /api/ {
    limit_req zone=api burst=20;
}
```

---

## Caching Strategy

### Multi-Layer Caching

```
┌────────────────┐
│  Application   │
│  (in-memory)   │  ← L1: Frequently accessed
└───────┬────────┘
        │
┌───────▼────────┐
│     Redis      │  ← L2: Shared cache
└───────┬────────┘
        │
┌───────▼────────┐
│   PostgreSQL   │  ← L3: Persistent storage
└────────────────┘
```

**Configuration:**
```yaml
cache:
  l1:
    enabled: true
    max_size: 1000
    ttl: 5m
  l2:
    enabled: true
    backend: redis
    ttl: 1h
```

---

## Monitoring & Observability

### Key Metrics

**System metrics:**
- CPU usage per component
- Memory usage and leaks
- Disk I/O and space
- Network throughput

**Application metrics:**
- Job queue depth
- Job processing time (p50, p95, p99)
- API latency (p50, p95, p99)
- Error rates
- Cache hit rates

**Business metrics:**
- Objects ingested per hour
- Active users
- Search queries per minute
- Pipeline execution success rate

### Prometheus Exporter

**Enable metrics:**
```yaml
metrics:
  enabled: true
  port: 9091
  path: /metrics
```

**Example metrics:**
```
# Job queue depth
contexthelp_job_queue_depth{priority="high"} 42
contexthelp_job_queue_depth{priority="normal"} 128

# Job processing time
contexthelp_job_duration_seconds{pipeline="text.long",quantile="0.5"} 2.3
contexthelp_job_duration_seconds{pipeline="text.long",quantile="0.95"} 8.1

# API latency
contexthelp_api_request_duration_seconds{endpoint="/search",quantile="0.95"} 0.15
```

---

## Backup & Disaster Recovery

### SQLite Backup

```bash
# Online backup (no downtime)
sqlite3 ~/.local/share/contexthelp/data.db ".backup backup.db"

# Incremental backup (WAL checkpointing)
sqlite3 data.db "PRAGMA wal_checkpoint(FULL);"
```

### PostgreSQL Backup

```bash
# Full backup
pg_dump contexthelp > backup_$(date +%Y%m%d).sql

# Continuous archiving
# Enable WAL archiving in postgresql.conf
archive_mode = on
archive_command = 'cp %p /backup/wal/%f'

# Point-in-time recovery
pg_restore -d contexthelp backup_20240126.sql
```

### Backup Strategy

- **Daily:** Full backup
- **Hourly:** Incremental (WAL)
- **Retention:** 30 days daily, 12 months weekly
- **Storage:** Off-site S3/GCS/Azure Blob

---

## Cost Optimization

### Personal Tier (Free)

```
Local SQLite:        $0
Self-hosted:         $0
Total:               $0/month
```

### Team Tier (~$50/month)

```
DigitalOcean Droplet (4 vCPU, 8GB): $48/month
Or
AWS t3.large:                        $60/month
S3 storage (100GB):                  $2/month
Total:                               ~$50-62/month
```

### Organization Tier (~$300/month)

```
3× AWS t3.xlarge:                    $300/month
RDS PostgreSQL (db.t3.large):        $136/month
ElastiCache Redis:                   $50/month
S3 storage (1TB):                    $23/month
Load balancer:                       $25/month
Total:                               ~$534/month
```

---

## Deployment Checklist

### Pre-Production

- [ ] Load testing completed
- [ ] Security audit passed
- [ ] Backup strategy tested
- [ ] Monitoring configured
- [ ] Alerting rules defined
- [ ] Runbooks created
- [ ] Disaster recovery plan documented

### Production Launch

- [ ] DNS configured
- [ ] SSL certificates installed
- [ ] Firewall rules applied
- [ ] Rate limiting enabled
- [ ] Logging aggregation setup
- [ ] Health checks configured
- [ ] Auto-scaling policies set

### Post-Production

- [ ] Monitor metrics dashboard
- [ ] Review logs daily
- [ ] Test backups weekly
- [ ] Review capacity monthly
- [ ] Security patches applied
- [ ] Performance optimization ongoing

---

## Related Documentation

- [environment-variables.md](environment-variables.md) — Configuration reference
- [security/](security/) — Security documentation
- [development.md](development.md) — Development workflow

---

For deployment patterns and infrastructure, see [development-infrastructure.md](development-infrastructure.md).
