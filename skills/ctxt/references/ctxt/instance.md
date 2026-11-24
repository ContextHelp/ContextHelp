# `ctxt instance`

List named endpoints and running local `dpkms` instances, print the active target, and persist an active target selection.

Resolution order: `--instance` / `CTXT_INSTANCE` → state file (`ctxt instance use`) → config default.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `instance list` (`ls`) / `current` | `read` | `yes` |
| `instance use <name\|-\|port>` | `write-local` | `yes` |

## Subcommands

```bash
ctxt instance list               # alias: ls
ctxt instance current
ctxt instance use <name|-|port>  # "-" clears persisted override
```

## Agent Recipes

```bash
ctxt instance list --format json
ctxt instance use work && ctxt instance current
CTXT_INSTANCE=work ctxt stats --format json
```

## Gotchas & Next Steps

- In parallel/multi-agent sessions, prefer per-call `--instance` or `CTXT_INSTANCE` over mutating the shared state file via `instance use`.
- Next: check health ([status.md](status.md)) or manage daemons ([../dpkms/serve.md](../dpkms/serve.md), [../dpkms/lifecycle.md](../dpkms/lifecycle.md)).
