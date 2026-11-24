# `ctxt approval`

Review requests that require a second person (such as steward elevation requests or widening share changes by non-owner admins): list, inspect, approve, reject, or withdraw your own pending request.

## Execution Profile

| Subcommand | Side effect |
|---|---|
| `approval list` / `show <id>` | `read` |
| `approval approve <id>` / `reject <id>` / `withdraw <id>` | `write` |

## Subcommands & Flags

```bash
ctxt approval list [--kind <k>] [--state <requested|approved|rejected|withdrawn|stale|expired>] [--limit <n>]
ctxt approval show <request-id>
ctxt approval approve <request-id>
ctxt approval reject <request-id>
ctxt approval withdraw <request-id>
```

| Flag | Subcommand | Purpose |
|---|---|---|
| `--kind <k>` | `list` | Filter by request kind |
| `--state <st>` | `list` | `requested`, `approved`, `rejected`, `withdrawn`, `stale`, or `expired` |
| `--limit <n>` | `list` | Max requests to show |

## Agent Recipes

```bash
# Inspect pending approval queue as JSON
ctxt approval list --state requested --format json
ctxt approval show apr_12345 --format json

# Approve or reject after verifying scope
ctxt approval approve apr_12345
```

## Next Steps

- Sharing rules: [share.md](share.md) · Steward grants: [steward.md](steward.md)
