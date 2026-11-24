# `ctxt registry`

Manage decentralized knowledge/entity registries: add, authenticate, sync, reconcile conflicts, inspect capabilities/entitlements/usage, search indexes, and submit bundles.

## Execution Profile

| Subcommand | Side effect | Idempotency | Confirmation |
|---|---|---|---|
| `list`, `info`, `search`, `capabilities`, `entitlements`, `usage` | `read` | `yes` | — |
| `add`, `sync`, `login`, `logout`, `submit` | `write` | `yes` | — |
| `delete` | `destructive` | `yes` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt registry list
ctxt registry add <name> <url>
ctxt registry delete <name> --confirm=yes --confirm-token=<sha>
ctxt registry info <name>
ctxt registry sync <name> [--reconcile] [--merge-strategy <last-write-wins|trust-score>]
ctxt registry search <query> [--index-url <url>]
ctxt registry login <name> [--token <tok>]
ctxt registry logout <name>
ctxt registry capabilities <name>
ctxt registry entitlements
ctxt registry usage [name] [--days <n>]
ctxt registry submit <bundle-path>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--reconcile` | `sync` | `false` | Sync all configured registries with multi-registry entity reconciliation |
| `--merge-strategy <s`> | `sync` | `last-write-wins` | Conflict resolution: `last-write-wins` or `trust-score` |
| `--index-url <url>` | `search` | built-in default | Registry index URL to search |
| `--token <tok>` | `login` | `""` | Auth token (reads from stdin prompt if omitted; stored in OS keychain) |
| `--days <n>` | `usage` | `0` | Metering window in days (`0` = all time) |

## Agent Recipes

```bash
# Discover, register, and sync a registry
ctxt registry search "ux patterns" --format json
ctxt registry add core https://registries.context.help/core
ctxt registry sync core --format json

# Reconcile entity conflicts across all registries by trust score
ctxt registry sync core --reconcile --merge-strategy trust-score

# Inspect capabilities, entitlements, and 30-day metering usage
ctxt registry capabilities core --format json
ctxt registry entitlements --format json
ctxt registry usage core --days 30 --format json
```

## Gotchas & Next Steps

- Avoid passing `--token` directly on the command line when interactive stdin is available; `login` persists tokens in the OS keychain.
- To validate a local registry YAML before publishing, use `dpkms dev validate-registry <file>` ([../dpkms/dev.md](../dpkms/dev.md)).
- Next: inspect synced entities ([entity.md](entity.md)) or run federated search ([find.md](find.md)).
