# `ctxt hold`

List, place, and release legal and administrative holds that prevent any mutation, redaction, or deletion of protected knowledge objects.

Holds are enforced transactionally at the storage layer across edits, deletes, expiry sweeps, edges, attachments, and aliases until released.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `hold list` | `read` | `yes` |
| `hold place` | `write` | `yes` |
| `hold release` | `write` | `yes` |

## Subcommands & Flags

```bash
ctxt hold list    [--hold-id <id>] [--object <id>] [--source-key <key>]
ctxt hold place   --hold-id <id> (--object <id> | --source-key <key>) --reason "<why>"
ctxt hold release --hold-id <id> [--object <id>] [--source-key <key>] [--reason "<why>"]
```

| Flag | Subcommand(s) | Purpose |
|---|---|---|
| `--hold-id <id>` | `list`, `place`, `release` | Hold identifier (e.g. `lit-2026-01`) |
| `--object <id>` | `list`, `place`, `release` | Target knowledge object ID |
| `--source-key <key>` | `list`, `place`, `release` | Target every object matching `source_key` |
| `--reason <text>` | `place`, `release` | Non-empty justification recorded with the hold (required on `place`) |

## Agent Recipes

```bash
# Place a litigation hold on a single object
ctxt hold place --object obj_123 --hold-id lit-2026 --reason "litigation hold"

# Place a hold across all objects for a source_key
ctxt hold place --source-key "slack:C123:1700000000" --hold-id case-42 --reason "audit preservation"

# Inspect active holds for an object
ctxt hold list --object obj_123

# Release an operator hold when the matter closes
ctxt hold release --hold-id lit-2026 --object obj_123 --reason "matter closed"
```

## Gotchas & Next Steps

- `hold release` only releases operator-placed holds; holds originating from external peer obligations (`bos.obligations/v1`) remain governed by the obligation lifecycle.
- Next: confirm hold state with `ctxt hold list` before retrying `edit` ([edit.md](edit.md)) or `delete` ([delete.md](delete.md)).
