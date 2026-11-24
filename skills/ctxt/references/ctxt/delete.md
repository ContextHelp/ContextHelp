# `ctxt delete`

Permanently delete knowledge objects by ID, index, or filter. **Irreversible.**

## Execution Profile

- **Side effect**: `destructive`
- **Idempotency**: `yes` (deleting an already-deleted ID is a no-op)
- **Confirmation**: requires kit's two-step confirmation (`--confirm=yes --confirm-token=<sha>`). Local `-y`/`--yes` does not exist.

## Usage

```bash
ctxt delete <filter-flags> --confirm=yes --confirm-token=<sha>
```

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--id <id>` | `""` | Delete specific knowledge object |
| `--index <csv>` | `""` | Delete by list index (comma-separated) |
| `--tagged <csv>` | `""` | Delete by tag (comma-separated) |
| `--hint <val>` | `""` | Delete by hint |
| `--mention <@slug>` | `""` | Delete by mention |
| `--type <kind>` | `""` | Delete by object type |
| `--subtype <val>` | `""` | Delete by subtype |
| `--all` | `false` | Delete **every** knowledge object |

## Agent Recipes

```bash
# 1. Always inspect matching objects first
ctxt list --tagged temporary --format json

# 2. Run delete to inspect scope / obtain confirmation token, then confirm
ctxt delete --id obj_12345678 --confirm=yes --confirm-token=<sha>

# 3. Verify removal
ctxt list --tagged temporary --format json
```

## Gotchas & Next Steps

- At least one filter (`--id`, `--tagged`, `--mention`, `--type`, or `--all`) is required.
- If too much was deleted, review the trail via `ctxt log --type delete` ([log.md](log.md)) or restore from a `dpkms` backup ([../dpkms/backup-restore.md](../dpkms/backup-restore.md)).
