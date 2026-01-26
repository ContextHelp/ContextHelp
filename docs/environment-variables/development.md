# Development Environment Variables

Profiling, debugging, metrics, and testing configuration.

---

## Profiling

### ENABLE_PROFILING
**Type:** boolean  
**Default:** `false`

Enable pprof profiling endpoints.

```bash
ENABLE_PROFILING=true
```

Access profiler:
```bash
# CPU profile
go tool pprof http://localhost:6060/debug/pprof/profile

# Memory profile
go tool pprof http://localhost:6060/debug/pprof/heap
```

### PPROF_PORT
**Type:** integer  
**Default:** `6060`

Profiling server port.

```bash
PPROF_PORT=6060
```

---

## Metrics

### ENABLE_METRICS
**Type:** boolean  
**Default:** `true`

Enable Prometheus metrics export.

```bash
ENABLE_METRICS=true
```

### METRICS_PORT
**Type:** integer  
**Default:** `9091`

Metrics server port.

```bash
METRICS_PORT=9091
```

Access metrics:
```bash
curl http://localhost:9091/metrics
```

---

## Debug Logging

### DEBUG_SQL
**Type:** boolean  
**Default:** `false`

Log all SQL queries.

```bash
DEBUG_SQL=true
```

**Output:**
```
2024-01-26 10:30:45 [SQL] SELECT * FROM objects WHERE id = ?
2024-01-26 10:30:45 [SQL] INSERT INTO jobs (id, type, payload) VALUES (?, ?, ?)
```

### DEBUG_JOBS
**Type:** boolean  
**Default:** `false`

Verbose job execution logging.

```bash
DEBUG_JOBS=true
```

**Output:**
```
[JOB] Starting job abc123 (pipeline: text.long)
[JOB] Step 1/5: Parse input
[JOB] Step 2/5: Extract entities
[JOB] Completed job abc123 in 2.3s
```

### DEBUG_PIPELINES
**Type:** boolean  
**Default:** `false`

Verbose pipeline execution logging.

```bash
DEBUG_PIPELINES=true
```

**Output:**
```
[PIPELINE] text.long: Starting
[PIPELINE] text.long: Step parse_input (0.1s)
[PIPELINE] text.long: Step extract_entities (1.2s)
[PIPELINE] text.long: Completed in 2.3s
```

---

## Testing

### TEST_DB_PATH
**Type:** string  
**Default:** `./data/sqlite/contexthelp_test.db`

Path to test database.

```bash
TEST_DB_PATH=/tmp/contexthelp_test.db
```

### TEST_CLEANUP
**Type:** boolean  
**Default:** `true`

Clean up test data after runs.

```bash
TEST_CLEANUP=false  # Keep test data for debugging
```

### TEST_FIXTURES
**Type:** string  
**Default:** `./test/fixtures`

Directory containing test fixtures.

```bash
TEST_FIXTURES=./test/data
```

---

## Examples

### Development (Full Debug)
```bash
ENABLE_PROFILING=true
PPROF_PORT=6060

ENABLE_METRICS=true
METRICS_PORT=9091

DEBUG_SQL=true
DEBUG_JOBS=true
DEBUG_PIPELINES=true

TEST_CLEANUP=false
```

### Performance Testing
```bash
ENABLE_PROFILING=true
PPROF_PORT=6060

ENABLE_METRICS=true
METRICS_PORT=9091

DEBUG_SQL=false
DEBUG_JOBS=false
DEBUG_PIPELINES=false
```

### CI/CD Testing
```bash
ENABLE_PROFILING=false
ENABLE_METRICS=false

DEBUG_SQL=false
DEBUG_JOBS=false
DEBUG_PIPELINES=false

TEST_DB_PATH=/tmp/test_$(date +%s).db
TEST_CLEANUP=true
TEST_FIXTURES=./test/fixtures
```

---

## Performance Analysis

### CPU Profiling
```bash
# Enable profiling
export ENABLE_PROFILING=true
export PPROF_PORT=6060

# Start server
./bin/dpkms serve &

# Generate load
./bin/ctxt analyze "..." &

# Capture profile (30 seconds)
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/profile?seconds=30
```

### Memory Profiling
```bash
# Capture heap profile
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/heap

# Capture allocs profile
go tool pprof http://localhost:6060/debug/pprof/allocs
```

### Goroutine Analysis
```bash
# View goroutines
curl http://localhost:6060/debug/pprof/goroutine?debug=1
```

---

## Metrics Collection

### Prometheus Integration

**prometheus.yml:**
```yaml
scrape_configs:
  - job_name: 'contexthelp'
    static_configs:
      - targets: ['localhost:9091']
```

**Key metrics:**
```
contexthelp_job_queue_depth{priority="high"}
contexthelp_job_duration_seconds{pipeline="text.long",quantile="0.95"}
contexthelp_api_request_duration_seconds{endpoint="/search",quantile="0.95"}
```

---

## Related

- [../development.md](../development.md#debugging) — Development guide
- [../scaling.md](../scaling.md#monitoring--observability) — Production monitoring
