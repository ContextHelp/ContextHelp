# Runbook

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

Current CLI surface does not provide built-in full backup/export commands in `dpkms`. Use your storage backend snapshot/backup tooling and validate restore procedures externally.

## Cross-links

- [`../reference/api-cli-reference.md`](../reference/api-cli-reference.md)
- [`../reference/config-and-permissions.md`](../reference/config-and-permissions.md)
- [`../troubleshooting/faq.md`](../troubleshooting/faq.md)
- [`../admin-extensibility/pipeline-management.md`](../admin-extensibility/pipeline-management.md)
