# `ctxt share`

Share a `dpkms` instance safely with other people via audiences, YAML share policies, time-bounded grants, per-principal class ceilings, invite links, viewer previews, and sharing read-audit inspection.

Reads on protected/public instances are fail-closed per caller; non-owner principals see only what their grants allow. Widening share changes by a non-owner admin wait for approval ([approval.md](approval.md)).

## Execution Profile

| Subcommand | Side effect |
|---|---|
| `audience list`, `policy list/show`, `template list`, `preview`, `audit` | `read` |
| `audience create/add/remove`, `policy create/edit`, `template install`, `grant`, `revoke`, `ceiling`, `invite` | `write` |

## Subcommands & Flags

### Audiences & invites

```bash
ctxt share audience create <name>
ctxt share audience list
ctxt share audience add <audience> <principal> [--expires <dur|date|rfc3339>]
ctxt share audience remove <audience> <principal>
ctxt share invite --name <display-name> --audience <aud> [--expires <dur>]   # default expires: 7d
```

### Share policies & templates

```bash
ctxt share template list
ctxt share template install <name>
ctxt share policy list
ctxt share policy show <name>
ctxt share policy create <file.yaml>
ctxt share policy edit <name> <file.yaml>
```

### Grants & class ceilings

```bash
ctxt share grant <audience> <policy> [--expires <dur|date|rfc3339>]
ctxt share revoke <grant-id>
ctxt share ceiling <principal> --deny-classes <c1,c2>   # empty clears ceiling
```

### Preview & read audit

```bash
ctxt share preview <principal-or-policy> -- <read-command...>
ctxt share audit [--principal <id>] [--object <id>] [--decision <allowed|denied|deferred>] [--since <expr>] [--limit <n>] [--checks]
```

> You can also preview any read command directly via global `--as <principal|policy>` (owner only).

## Agent Recipes

```bash
# Install a built-in template, create an audience, grant access for 30d
ctxt share template list --format json
ctxt share audience create advisors
ctxt share audience add advisors prn_123 --expires 90d
ctxt share grant advisors public-notes --expires 30d

# Preview what another viewer sees before granting
ctxt find "roadmap" --as advisors --format json

# Audit who read a specific object and every policy check that ran
ctxt share audit --object obj_12345678 --checks --format json
```

## Next Steps

- Review pending approval requests for widening shares: [approval.md](approval.md) · Request temporary steward elevation: [steward.md](steward.md) · Verify host access posture: [../dpkms/access-governance.md](../dpkms/access-governance.md)
