# `ctxt registry`

Manage decentralized knowledge/entity registries: add, authenticate, sync, reconcile conflicts, review queued entity approvals (`pending`/`approve`/`reject`), inspect capabilities/entitlements/usage, search indexes, and submit bundles.

## Execution Profile

| Subcommand | Side effect | Confirmation |
|---|---|---|
| `list`, `info`, `search`, `capabilities`, `entitlements`, `usage`, `pending` | `read` | — |
| `add`, `sync`, `login`, `logout`, `submit`, `reject` | `write` | — |
| `delete`, `approve` | `destructive` / gated write | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt registry list
ctxt registry add <name> <url>
ctxt registry delete <name> --confirm=yes --confirm-token=<sha>
ctxt registry info <name>
ctxt registry sync <name> [--reconcile] [--merge-strategy <last-write-wins|trust-score>]
ctxt registry pending [--registry <name>]
ctxt registry approve [--all --registry <name>] --confirm=yes --confirm-token=<sha>
ctxt registry reject <id...>
ctxt registry search <query> [--index-url <url>]
ctxt registry login <name> [--token <tok>] [--origin <scheme://host[:port]>]
ctxt registry logout <name>
ctxt registry capabilities <name>
ctxt registry entitlements
ctxt registry usage [name] [--days <n>]
ctxt registry submit <bundle-path>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--reconcile` | `sync` | `false` | Sync all configured registries with entity reconciliation |
| `--merge-strategy <s>` | `sync` | `last-write-wins` | `last-write-wins` or `trust-score` |
| `--registry <name>` | `pending`, `approve` | `""` | Filter queued entities / target registry for `--all` |
| `--all` | `approve` | `false` | Approve every entity queued by `--registry` |
| `--token <tok>` | `login` | `""` | Auth token (reads from stdin if omitted; stored in OS keychain) |
| `--origin <origin>` | `login` | `""` | Origin (`scheme://host[:port]`) bound to token; required non-interactively for project-scoped registries |
| `--index-url <url>` | `search` | built-in | Registry index URL to search |
| `--days <n>` | `usage` | `0` | Metering window in days (`0` = all time) |

## Agent Recipes

```bash
# Discover, register, and sync a registry
ctxt registry search "ux patterns" --format json
ctxt registry add core https://registries.context.help/core
ctxt registry sync core --format json

# Review and approve queued registry entities
ctxt registry pending --registry core --format json
ctxt registry approve --all --registry core --confirm=yes --confirm-token=<sha>
```

## Gotchas & Next Steps

- Lint a local registry YAML before submitting via `dpkms dev validate-registry <file>` ([../dpkms/dev.md](../dpkms/dev.md)).
- Next: inspect synced entities ([entity.md](entity.md)) or resolve refs ([resolve.md](resolve.md)).
