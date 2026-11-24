# `ctxt steward`

Request, list, inspect, and revoke time-bounded elevated steward access (chained-audit read windows with explicit reason, scope, and matter reference).

## Execution Profile

| Subcommand | Side effect |
|---|---|
| `steward list` / `show <id>` | `read` |
| `steward request` / `revoke <id>` | `write` |

## Subcommands & Flags

```bash
ctxt steward request --reason "<why>" --expires <dur|date|rfc3339> (--scope <file|-> | --as-audience <aud>) [--steward <prn>] [--not-before <t>] [--matter <ref>] [--renews <grant-id>]
ctxt steward list [--state <approved|active|expired|revoked>] [--steward <prn>] [--limit <n>]
ctxt steward show <grant-id>
ctxt steward revoke <grant-id> --reason "<why>"
```

| Flag | Subcommand(s) | Purpose |
|---|---|---|
| `--scope <file\|->` | `request` | Share policy YAML/JSON naming readable scope (`-` reads stdin) |
| `--as-audience <aud>` | `request` | Copy current visibility of `<aud>` in place of `--scope` |
| `--reason <text>` | `request`, `revoke` | Justification shown to approvers / recorded on revocation |
| `--expires <expr>` | `request` | Window end: duration (`7d`, `72h`), date (`2026-12-31`), or RFC3339 |
| `--not-before <expr>` | `request` | Window start (default: now) |
| `--steward <prn>` | `request`, `list` | Target steward principal (defaults to caller on `request`) |
| `--matter <ref>` | `request` | External case/ticket reference |
| `--renews <grant-id>` | `request` | Prior grant being renewed |
| `--state <st>` | `list` | `approved\|active\|expired\|revoked` |
| `--limit <n>` | `list` | Max grants to show |

## Agent Recipes

```bash
# Request a 24h steward window matching an audience's visibility for an incident ticket
ctxt steward request --as-audience oncall --expires 24h --matter INC-402 --reason "investigate ingestion failure"

# List active steward grants and revoke early when done
ctxt steward list --state active --format json
ctxt steward revoke stw_abc123 --reason "incident resolved"
```

## Next Steps

- Approve/reject pending steward requests: [approval.md](approval.md) · Verify steward audit chain on host: `dpkms audit verify` ([../dpkms/access-governance.md](../dpkms/access-governance.md))
