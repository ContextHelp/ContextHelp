# `ctxt instance`

Discover running `dpkms` instances and pin the active target instance persisted across CLI invocations.

Instance resolution priority:
1. Per-call `--instance <name|port>` flag or `CTXT_INSTANCE` env var
2. Persisted state file written by `ctxt instance use`
3. Default `storage.path` from config

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `instance list` (`ls`) | `read` | `yes` |
| `instance current` | `read` | `yes` |
| `instance use <name\|-\|port>` | `write-local` | `yes` |

## Subcommands

```bash
ctxt instance list               # alias: ls
ctxt instance current
ctxt instance use <name|-|port>  # "-" clears persisted override
```

No local flags beyond globals ([../../SKILL.md](../../SKILL.md)).

## Agent Recipes

```bash
# Discover running instances
ctxt instance list --format json

# Switch persistent target and confirm
ctxt instance use work && ctxt instance current

# One-off call against another instance without mutating global state
CTXT_INSTANCE=work ctxt stats --format json
ctxt find "roadmap" --instance work --format json
```

## Gotchas & Next Steps

- In parallel/multi-agent sessions, prefer per-command `--instance` or `CTXT_INSTANCE` over mutating the shared state file with `instance use`.
- Next: verify instance health ([status.md](status.md)) or manage daemons ([../dpkms/serve.md](../dpkms/serve.md), [../dpkms/lifecycle.md](../dpkms/lifecycle.md)).
