# Workers & Jobs Environment Variables

Job queue and background worker configuration.

---

## Worker Pool

### DPKMS_WORKERS
**Type:** integer\
**Default:** `4`\
**Config key:** `server.workers`\
**Flag:** `dpkms serve --workers`

Number of job workers `dpkms serve` runs. `--workers` wins over it.

```bash
DPKMS_WORKERS=8
```

**Scaling guidelines:**
- **CPU-bound tasks:** Workers ≈ CPU cores
- **I/O-bound tasks:** Workers = 2-4× CPU cores
- **Mixed workload:** Start with 2× CPU cores

### DPKMS_POLL_INTERVAL
**Type:** duration\
**Default:** `500ms`\
**Minimum:** `50ms`\
**Config key:** `jobs.poll_interval`

How often idle workers poll for new jobs.

```bash
DPKMS_POLL_INTERVAL=250ms
```

---

## Job Configuration

### DPKMS_MAX_RETRIES
**Type:** integer\
**Default:** `3`\
**Config key:** `jobs.max_retries`

Default retry limit for fan-out jobs.

```bash
DPKMS_MAX_RETRIES=5
```

### DPKMS_STALE_TIMEOUT
**Type:** duration\
**Default:** `30m`\
**Config key:** `jobs.stale_timeout`

How long a running job may stay silent before it counts as stale and
becomes eligible for recovery. Jobs have no execution timeout.

```bash
DPKMS_STALE_TIMEOUT=1h
```

### DPKMS_MAX_HOPS
**Type:** integer\
**Default:** `5`\
**Config key:** `jobs.max_hops`

Maximum number of pipeline hops one job can take.

```bash
DPKMS_MAX_HOPS=8
```

### Config-file only

`jobs.drain_timeout` (default `30s`): how long `dpkms serve` waits for
in-flight jobs on graceful shutdown. No env var.

---

## Examples

### Development (Low Concurrency)
```bash
DPKMS_WORKERS=2
DPKMS_POLL_INTERVAL=500ms
DPKMS_MAX_RETRIES=3
```

### Production (High Throughput)
```bash
DPKMS_WORKERS=16
DPKMS_POLL_INTERVAL=250ms
DPKMS_MAX_RETRIES=5
DPKMS_STALE_TIMEOUT=1h
```

---

## Worker Monitoring

List jobs and their state:
```bash
dpkms job list
```

Instance status:
```bash
dpkms status
```

---

## Related

- [../dpkms/jobs-and-ingestion.md](../dpkms/jobs-and-ingestion.md) — Job system architecture
- [../dpkms/queue.md](../dpkms/queue.md) — Queue implementation
- [../scaling.md](../scaling.md#worker-scaling) — Worker scaling strategies
