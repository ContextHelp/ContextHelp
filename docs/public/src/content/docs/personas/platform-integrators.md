---
title: Platform Integrators Guide
description: This chapter is for teams extending dPKMS/`ctxt` via custom pipelines, step registries, and plugin-style integrations.
---

This chapter is for teams extending dPKMS/`ctxt` via custom pipelines, step registries, and plugin-style integrations.

## Goal

Ship extensibility safely: reproducible pipeline changes, controlled registry updates, and constrained execution boundaries.

## Prerequisites

1. Local runtime is healthy:

```bash
ctxt version
dpkms version
```

Run server in a dedicated terminal:

```bash
dpkms serve
```

2. Config validates:

```bash
ctxt config validate
```

3. You have access to pipeline definitions and any external step registries you plan to use.

## Current Implementation Path (Use This Today)

### 1. Inspect pipeline inventory

```bash
dpkms pipeline list
dpkms pipeline list --include-archived
dpkms pipeline show <pipeline-name>
```

Use `--raw` to inspect full pipeline payload:

```bash
dpkms pipeline show <pipeline-name> --raw
```

### 2. Create and validate a custom pipeline

```bash
dpkms pipeline create ./pipelines/custom-text-long.yaml
dpkms pipeline list --name custom-text-long
dpkms pipeline show custom-text-long
```

Exercise it with a test payload:

```bash
dpkms pipeline enqueue --pipeline custom-text-long --type text "Pipeline integration smoke test"
```

### 3. Manage lifecycle: archive, unarchive, remove

```bash
dpkms pipeline archive custom-text-long
dpkms pipeline unarchive custom-text-long
dpkms pipeline remove custom-text-long
```

### 4. Discover and install pipeline steps

```bash
dpkms pipeline step list
dpkms pipeline step list --source registry
```

Install/uninstall registry step:

```bash
dpkms pipeline step install <step-name> --registry <registry-url>
dpkms pipeline step uninstall <step-name>
```

### 5. Manage step registries

```bash
dpkms pipeline step registry list
dpkms pipeline step registry add <registry-name> <registry-url>
dpkms pipeline step registry update <registry-url>
dpkms pipeline step registry autoupdate <registry-url> --enable
```

Disable auto-update for controlled rollout windows:

```bash
dpkms pipeline step registry autoupdate <registry-url> --disable
```

## 30-Minute Integrator Smoke Test

1. Validate config and runtime.
2. List and inspect one existing pipeline.
3. Create one custom pipeline from file.
4. Enqueue one test payload through it.
5. Add one step registry and list available steps.
6. Install one step, then uninstall it.
7. Archive and unarchive the custom pipeline.

## Quality Checklist

- Pipeline definitions are versioned and reviewable.
- Step installs are reproducible from declared registry URLs.
- Registry auto-update policy is explicit (`--enable` or `--disable`).
- Lifecycle operations (create/archive/remove) behave predictably.
- Test payloads produce expected job outcomes.

## Common Pitfalls and Fixes

### Pipeline works in staging but not production

- Compare config and step registry definitions between environments.
- Verify the same step versions are installed.

### Registry updates break execution

- Disable autoupdate before high-risk periods.
- Update manually and validate with test payloads first.

### Step install ambiguity

- Use explicit registry URLs.
- Document installed step source and version in change records.

## Story Alignment

- Core integrator/admin: `US-0013`, `US-0014`, `US-0028`, `US-0029`, `US-0041`, `US-0042` to `US-0045`, `US-0050`
- Pipeline lifecycle: `US-0101` to `US-0113`

## Related Manual Sections

- [`../admin-extensibility/pipeline-management.md`](../admin-extensibility/pipeline-management.md)
- [`../admin-extensibility/plugin-development.md`](../admin-extensibility/plugin-development.md)
- [`../reference/config-and-permissions.md`](../reference/config-and-permissions.md)
- [`../operations/runbook.md`](../operations/runbook.md)
