# Aliasing Plugin (Bundled)

The **Aliasing Plugin** lets you assign human-readable names to knowledge objects so you can refer to them by alias instead of opaque object IDs. Aliases are resolved transparently — any command that accepts an object ID (`ctxt open`, `ctxt edit`, `ctxt delete`, etc.) will accept an alias without any extra flags.

---

## Overview

Object IDs in ContextHelp are stable but unreadable (`obj_abc123`). The aliasing plugin lets you bind a short, memorable name to any object:

```bash
ctxt alias set project-charter obj_abc123
ctxt open project-charter   # works exactly like: ctxt open obj_abc123
```

Aliases can be **global** (available in all contexts) or **profile-scoped** (active only when a specific focus profile is selected). A profile alias silently shadows a global alias of the same name, so different roles can point the same alias at different objects.

---

## Enabling the Plugin

Add the following to your `config.yaml`:

```yaml
plugins:
  - name: aliasing
    config:
      enabled: true          # default: true
      default_scope: global  # global | profile
```

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `true` | Set to `false` to disable alias resolution entirely. Existing aliases are preserved in the database. |
| `default_scope` | `global` | Scope used when `--scope` is not specified on CLI or API calls. |

---

## CLI Reference

### `ctxt alias set <alias> <object-id>`

Create an alias pointing to a knowledge object.

```bash
# Global alias (available everywhere)
ctxt alias set project-charter obj_abc123

# Profile-scoped alias (only active when the "founder" profile is selected)
ctxt alias set roadmap obj_xyz789 --scope profile --profile founder
```

| Flag | Default | Description |
|------|---------|-------------|
| `--scope` | `global` | `global` or `profile` |
| `--profile` | `""` | Profile name — required when `--scope profile` |

---

### `ctxt alias list`

List all aliases, optionally filtered by object ID.

```bash
# List all aliases
ctxt alias list

# List aliases pointing to a specific object
ctxt alias list --object obj_abc123
```

Output format:
```
project-charter → obj_abc123 (scope=global)
roadmap → obj_xyz789 (scope=profile, profile=founder)
```

| Flag | Description |
|------|-------------|
| `--object <id>` | Filter to aliases for a specific object ID |

---

### `ctxt alias remove <alias>`

Delete an alias.

```bash
# Remove a global alias
ctxt alias remove project-charter

# Remove a profile-scoped alias
ctxt alias remove roadmap --scope profile --profile founder
```

| Flag | Default | Description |
|------|---------|-------------|
| `--scope` | `global` | Scope of the alias to remove |
| `--profile` | `""` | Profile name — required when `--scope profile` |

---

## Scope Explained

| Scope | When it applies | Typical use |
|-------|----------------|-------------|
| `global` | Always | Stable, project-wide shorthand |
| `profile` | Only when the named profile is active | Role-specific views (e.g., "roadmap" means different objects for Founder vs Engineer) |

**Shadowing:** if both a profile alias and a global alias exist with the same name, the profile alias wins when that profile is active. This is silent — no warning is shown.

**Resolution order** (implemented in `resolver.go`):
1. Profile-scoped alias matching the active profile
2. Global alias
3. Input returned unchanged (treated as a direct object ID — no error)

---

## Transparent Resolution

Alias resolution happens automatically before every object lookup. You do not need to add any flags or change your workflow:

```bash
ctxt open project-charter    # resolved transparently
ctxt edit project-charter    # same
ctxt delete project-charter  # same
```

If no alias matches, the input is passed through as-is, so unaliased IDs continue to work normally.

---

## Disabling

Set `enabled: false` in config to disable alias resolution. Existing aliases remain stored in the database and are restored if you re-enable the plugin later.

```yaml
plugins:
  - name: aliasing
    config:
      enabled: false
```

---

## Contributor Reference

### Architecture

The aliasing plugin implements two interfaces from `pkg/pluginapi`:

- **`plugin.Plugin`** — standard lifecycle (`Init`, `Close`, `PipelineSteps`)
- **`plugin.AliasResolver`** — `ResolveID(ctx, idOrAlias, profile) (string, error)`

It is **not** a pipeline step. Resolution happens at the service layer: `service` calls `registry.ResolveID(id, profile)` before every object lookup. The plugin is registered into the resolver registry during `Init`.

### Key Files

| File | Purpose |
|------|---------|
| `plugins/aliasing/plugin.go` | `Plugin` struct; `Init`, `ResolveID`, `SetAliasOp`, `ListAliases`, `RemoveAlias` |
| `plugins/aliasing/resolver.go` | `ResolveAlias` function and resolution order documentation |
| `plugins/aliasing/store.go` | `SetAlias` wrapping `AliasStore.Create` |
| `plugins/aliasing/config.go` | `AliasConfig`, `DefaultConfig`, `ConfigFromMap` |
| `plugins/aliasing/cli.go` | Cobra subtree; mounts as `ctxt alias` |
| `internal/storage/sqlite/migrations/010_aliases.sql` | Database schema |

### Storage

The plugin uses `pluginapi.AliasStore`, accessed via `deps.Store.Aliases()` in `Init`. `AliasStore.Resolve` performs the profile→global fallback at the DB layer. The store interface exposes:

- `Create(ctx, alias)` — insert or replace
- `List(ctx, AliasFilter)` — filter by object ID or scope
- `Delete(ctx, alias, scope, profile)` — remove a specific alias
- `Resolve(ctx, alias, profile)` — profile-first lookup (used by `ResolveAlias`)

### Adding a New Scope Type

To add a scope beyond `global` and `profile`:

1. Add the new scope value to `AliasStore` SQL schema and queries
2. Update `ResolveAlias` in `resolver.go` with the new resolution step
3. Add validation in `ConfigFromMap` if the scope is configurable
4. Add `--scope <new-scope>` handling in `cli.go`
5. Write a migration under `internal/storage/sqlite/migrations/`

### Running Tests

```bash
GOWORK=off go test ./plugins/aliasing/...
```
