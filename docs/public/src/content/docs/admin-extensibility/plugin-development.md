---
title: Plugin Development
description: Design and deploy extension capabilities without destabilizing core ingestion/search/composition workflows.
---

## Goal

Design and deploy extension capabilities without destabilizing core ingestion/search/composition workflows.

## Scope

- Plugin-style extensions for enrichment, providers, registry adapters, and ranking
- Capability/permission boundaries and isolation
- Packaging, compatibility, rollout, and rollback strategy

## Primary stories

- `US-0029`
- `US-0042` to `US-0045`
- `US-0013`, `US-0014`, `US-0041`, `US-0050`

## Development lifecycle

### 1. Define extension contract

Before coding:

1. Choose extension type (enrichment/provider/registry/ranking).
2. Define expected inputs/outputs and failure behavior.
3. Define compatibility target (runtime version, pipeline dependencies).

### 2. Implement with isolation constraints

Reference implementation contracts:

- [`../../plugins/plugins-api.md`](../../plugins/plugins-api.md)
- [`../../plugins/plugin-isolation.md`](../../plugins/plugin-isolation.md)

Guideline:

- request minimum required capabilities
- keep behavior deterministic where possible
- fail with actionable error messages

### 3. Register/distribute through step-registry flow where applicable

```bash
dpkms pipeline step registry list
dpkms pipeline step install <step-name> --registry <registry-url>
```

Validate with test enqueue:

```bash
dpkms pipeline enqueue --pipeline <test-pipeline> --type text "plugin validation payload"
ctxt job list --state failed --limit 20
```

### 4. Rollout strategy

1. Deploy in staging path first.
2. Run ingestion/search/composition smoke tests.
3. Roll out to production scope.
4. Monitor failure rates and latency.

### 5. Rollback strategy

1. Disable or uninstall failing step:

```bash
dpkms pipeline step uninstall <step-name>
```

2. Archive affected pipeline if needed:

```bash
dpkms pipeline archive <pipeline-name>
```

3. Restore known-good pipeline/step version.

## Compatibility checklist

- Extension tested against current runtime command/API surfaces.
- Required registry sources documented.
- Failure and retry behavior validated.
- Rollback commands documented and tested.

## Common failure modes

### Runtime capability mismatch

- Re-check declared capabilities and isolation assumptions.
- Validate behavior under minimal privilege.

### Version drift

- Pin target runtime version in extension docs.
- Re-test after core upgrades.

### Ranking or retrieval regressions

- Compare baseline retrieval output before/after extension.
- Roll back quickly if relevance quality drops.

## Cross-links

- [`../reference/config-and-permissions.md`](../reference/config-and-permissions.md)
- [`../operations/runbook.md`](../operations/runbook.md)
- [`../troubleshooting/faq.md`](../troubleshooting/faq.md)
