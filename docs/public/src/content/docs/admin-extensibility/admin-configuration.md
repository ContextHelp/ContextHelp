---
title: Admin Configuration
description: Establish a stable, auditable runtime configuration for providers, profiles, security controls, and service behavior.
---

## Goal

Establish a stable, auditable runtime configuration for providers, profiles, security controls, and service behavior.

## Scope

- AI/provider and runtime config management
- Encryption/secrets posture via config and environment
- Focus profile governance
- Validation and change safety

## Primary stories

- `US-0027` (configure AI provider)
- `US-0030` (focus profiles)
- `US-0031` (encryption and secrets)

## Baseline procedure

### 1. Inspect and validate config

```bash
ctxt config path
ctxt config show
ctxt config validate
```

Edit config:

```bash
ctxt config edit
```

### 2. Validate runtime startup profile

```bash
dpkms serve --profile founder --workers 4
```

Use separate shell for commands and verify:

```bash
curl http://127.0.0.1:8080/health
```

### 3. Manage focus profiles

```bash
ctxt profile list
ctxt profile show <name>
ctxt profile create <name> --config <file>
ctxt profile set-default <name>
```

### 4. Validate registry configuration

```bash
ctxt registry list
ctxt registry info <name>
ctxt registry sync <name>
```

## Security and secrets posture

Use config plus environment variables for sensitive values; avoid embedding secrets in shared docs or scripts.

Reference docs:

- [`../../configuration-structure.md`](../../configuration-structure.md)
- [`../../environment-variables/README.md`](../../environment-variables/README.md)
- [`../../security/security-model.md`](../../security/security-model.md)

## Change checklist

1. `ctxt config validate` passes.
2. Service starts with expected flags.
3. Health endpoint responds.
4. Profiles and registries load as expected.
5. Change record includes what/why/how to rollback.

## Common failure modes

### Config validates locally but fails in deployment

- Compare env vars and runtime flags between environments.
- Confirm profile names and registry names match exactly.

### Profile behavior seems inconsistent

- Verify default profile and command-level `--profile` overrides.
- Re-check profile definitions with `ctxt profile show`.

## Cross-links

- [`../reference/config-and-permissions.md`](../reference/config-and-permissions.md)
- [`../operations/runbook.md`](../operations/runbook.md)
