# Workers & Jobs Environment Variables

Job queue and background worker configuration.

---

## Worker Pool

### WORKER_COUNT
**Type:** integer  
**Default:** `4`

Number of background workers.

```bash
WORKER_COUNT=8
```

**Scaling guidelines:**
- **CPU-bound tasks:** Workers ≈ CPU cores
- **I/O-bound tasks:** Workers = 2-4× CPU cores
- **Mixed workload:** Start with 2× CPU cores

### WORKER_POLL_INTERVAL
**Type:** duration  
**Default:** `1s`

How often workers poll for new jobs.

```bash
WORKER_POLL_INTERVAL=500ms
```

---

## Job Configuration

### JOB_RETRY_LIMIT
**Type:** integer  
**Default:** `3`

Maximum retry attempts for failed jobs.

```bash
JOB_RETRY_LIMIT=5
```

### JOB_TIMEOUT
**Type:** duration  
**Default:** `5m`

Default job execution timeout.

```bash
JOB_TIMEOUT=10m
```

### JOB_CLEANUP_INTERVAL
**Type:** duration  
**Default:** `1h`

How often to clean up old completed jobs.

```bash
JOB_CLEANUP_INTERVAL=24h
```

---

## Examples

### Development (Low Concurrency)
```bash
WORKER_COUNT=2
WORKER_POLL_INTERVAL=1s
JOB_RETRY_LIMIT=3
JOB_TIMEOUT=5m
JOB_CLEANUP_INTERVAL=1h
```

### Production (High Throughput)
```bash
WORKER_COUNT=16
WORKER_POLL_INTERVAL=500ms
JOB_RETRY_LIMIT=5
JOB_TIMEOUT=10m
JOB_CLEANUP_INTERVAL=6h
```

---

## Worker Monitoring

Check worker status:
```bash
./bin/dpkms workers stats
```

Check queue depth:
```bash
./bin/dpkms queue stats
```

---

## Related

- [../dpkms/jobs-and-ingestion.md](../dpkms/jobs-and-ingestion.md) — Job system architecture
- [../dpkms/queue.md](../dpkms/queue.md) — Queue implementation
- [../scaling.md](../scaling.md#worker-scaling) — Worker scaling strategies
