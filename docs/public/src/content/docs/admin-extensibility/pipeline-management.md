---
title: Pipeline Management
description: Manage pipelines and step registries safely across create, rollout, maintenance, and retirement phases.
---

## Goal

Manage pipelines and step registries safely across create, rollout, maintenance, and retirement phases.

## Scope

- Create, list, show, archive, unarchive, remove pipelines
- Enqueue payloads to test pipeline behavior
- Discover/install/uninstall steps
- Manage step registries and auto-update policy

## Primary stories

- `US-0101` to `US-0113`
- `US-0028` (register custom pipeline)

## Lifecycle playbook

### 1. Inventory and inspection

```bash
dpkms pipeline list
dpkms pipeline list --include-archived
dpkms pipeline show <name>
dpkms pipeline show <name> --raw
```

### 2. Create and validate

```bash
dpkms pipeline create ./pipelines/custom.yaml
dpkms pipeline show <name>
```

Smoke enqueue:

```bash
dpkms pipeline enqueue --pipeline <name> --type text "pipeline smoke test"
```

### 3. Operate and monitor

Check job outcomes with `ctxt`:

```bash
ctxt job list --limit 20
ctxt job status <job_id>
ctxt job log <job_id>
```

### 4. Step management

```bash
dpkms pipeline step list
dpkms pipeline step list --source registry
dpkms pipeline step install <step-name> --registry <registry-url>
dpkms pipeline step uninstall <step-name>
```

### 5. Registry management

```bash
dpkms pipeline step registry list
dpkms pipeline step registry add <name> <url>
dpkms pipeline step registry update <url>
dpkms pipeline step registry autoupdate <url> --enable
# or
dpkms pipeline step registry autoupdate <url> --disable
```

### 6. Archive/retire

```bash
dpkms pipeline archive <name>
dpkms pipeline unarchive <name>
dpkms pipeline remove <name>
```

## Rollout safety checklist

1. Create/update pipeline in controlled environment.
2. Run enqueue smoke tests.
3. Verify job outcomes and object quality.
4. Roll to broader use.
5. Keep rollback command ready (`archive` or restore prior config).

## Common failure modes

### Step installation fails

- Verify registry URL availability.
- Update manifest and retry.
- Confirm step name matches registry listing.

### Pipeline regressions after registry updates

- Disable autoupdate during high-risk windows.
- Re-enable after compatibility verification.

### Orphaned or stale pipelines

- Use archived listing regularly.
- Archive unused pipelines before deletion.

## Cross-links

- [`../operations/runbook.md`](../operations/runbook.md)
- [`../reference/config-and-permissions.md`](../reference/config-and-permissions.md)
- [`../troubleshooting/faq.md`](../troubleshooting/faq.md)
