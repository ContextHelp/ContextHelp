# `ctxt stats`

Display a live at-a-glance summary of corpus and system counts.

- **Use `stats`** for corpus counts and utilization.
- **Use `status`** for `dpkms` daemon health ([status.md](status.md)) or `doctor` for graph hygiene ([doctor.md](doctor.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Usage & Flags

```bash
ctxt stats [--watch]
```

| Flag | Default | Purpose |
|---|---|---|
| `--watch` | `false` | Re-print every 3s until Ctrl-C (avoid in agent scripts) |

## Agent Recipes

```bash
ctxt stats --format json
```

## Next Steps

- Daemon health: [status.md](status.md) · Graph health: [doctor.md](doctor.md)
