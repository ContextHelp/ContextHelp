# `ctxt stats`

Display a live at-a-glance summary of corpus and system counts (objects, entities, jobs, storage).

- **Use `stats`** for corpus counts and utilization metrics.
- **Use `status`** to probe `dpkms` daemon health/reachability ([status.md](status.md)).
- **Use `doctor`** to audit graph health issues like orphans or duplicates ([doctor.md](doctor.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Usage

```bash
ctxt stats [flags]
```

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--watch` | `false` | Re-print every 3s until Ctrl-C (avoid in non-interactive scripts) |

## Agent Recipes

```bash
# Machine-readable corpus snapshot
ctxt stats --format json
```

## Next Steps

- Check daemon readiness: [status.md](status.md) · Audit graph quality: [doctor.md](doctor.md)
