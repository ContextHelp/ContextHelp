# Operations Guide

This chapter is for operators running dPKMS/`ctxt` in shared or production-like environments.

## Goal

Keep ingestion, retrieval, and composition paths reliable while controlling latency, failure recovery, and maintenance risk.

## Prerequisites

1. Runtime binaries are available:

```bash
ctxt version
dpkms version
```

2. Configuration is valid:

```bash
ctxt config validate
```

3. You can access server and logs in your environment.

## Current Implementation Path (Use This Today)

### 1. Start runtime with explicit settings

```bash
dpkms serve --port 8080 --grpc-port 9090 --workers 4
```

Run this in a dedicated terminal so operational checks can run from another shell.

If needed for remote access:

```bash
dpkms serve --public
```

Health check:

```bash
curl http://127.0.0.1:8080/health
```

### 2. Monitor queue health and flow

```bash
ctxt job list --state pending --limit 50
ctxt job list --state running --limit 50
ctxt job list --state failed --limit 50
```

Drill into failures:

```bash
ctxt job status <job_id>
ctxt job log <job_id>
ctxt job retry <job_id>
```

### 3. Validate search and composition path health

```bash
ctxt find "system health check query" --limit 5
ctxt make summary --since 2026-02-01 --output-file ops-health-summary.md
```

### 4. Maintain data and index health

```bash
dpkms housekeeping vacuum
dpkms housekeeping reindex
dpkms housekeeping compact
```

Prune old data with policy guardrails:

```bash
dpkms housekeeping prune --before 2025-01-01
```

### 5. Monitor registries and sync behavior

```bash
ctxt registry list
ctxt registry info <registry-name>
ctxt registry sync <registry-name>
```

### 6. Validate pipeline and step governance

```bash
dpkms pipeline list --include-archived
dpkms pipeline step registry list
```

## 30-Minute Operations Smoke Test

1. Validate config and start `dpkms serve`.
2. Hit `/health` successfully.
3. Confirm queue visibility with `ctxt job list`.
4. Retry at least one failed job (or simulate and retry).
5. Run `housekeeping reindex`.
6. Sync one configured registry.
7. Generate a small summary to verify end-to-end service health.

## Quality Checklist

- Health endpoint responds.
- Job states are observable and actionable.
- Failed jobs can be diagnosed and retried.
- Maintenance commands run without data corruption symptoms.
- Registry sync path remains functional.

## Common Pitfalls and Fixes

### Pending queue interpreted as outage

- Distinguish pending backlog from hard failures.
- Track trend and worker capacity before escalating.

### Maintenance run during peak load

- Schedule housekeeping in low-traffic windows.
- Validate query behavior after reindex.

### Registry-induced latency spikes

- Isolate affected registry with targeted sync checks.
- Separate local retrieval from federated retrieval for incident triage.

## Story Alignment

- Ops/admin: `US-0008`, `US-0015`, `US-0027`, `US-0029`, `US-0031` to `US-0036`, `US-0301` to `US-0317`
- Pipeline and registry governance: `US-0110`, `US-0111`, `US-0112`

## Related Manual Sections

- [`../operations/runbook.md`](../operations/runbook.md)
- [`../admin-extensibility/admin-configuration.md`](../admin-extensibility/admin-configuration.md)
- [`../admin-extensibility/pipeline-management.md`](../admin-extensibility/pipeline-management.md)
- [`../troubleshooting/faq.md`](../troubleshooting/faq.md)
