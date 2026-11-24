# `ctxt principal`

Administer principals (people, devices, or peers) on a `dpkms` instance: create, list, show, suspend, revoke, mint single-use sign-in links, and bind/remove external identities.

- **Use `ctxt principal`** for admin user/device/peer lifecycle.
- **Use `ctxt auth`** for your own keys/sessions ([auth.md](auth.md)) or `ctxt share` for audiences/policies/grants ([share.md](share.md)).

## Execution Profile

| Subcommand | Side effect | Confirmation |
|---|---|---|
| `list`, `show`, `identity list` | `read` | — |
| `create`, `suspend`, `link`, `identity add`, `identity remove` | `write` | — |
| `revoke` | `destructive` (revokes principal + all credentials) | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt principal list [--kind <k>] [--status <st>]
ctxt principal show <principal-id>
ctxt principal create --kind <k> --name <label> [--role <admin|writer|reader>...]
ctxt principal suspend <principal-id>
ctxt principal revoke <principal-id> --confirm=yes --confirm-token=<sha>
ctxt principal link <principal-id>
ctxt principal identity list <principal-id>
ctxt principal identity add <principal-id> --method <m> --issuer <iss> --subject <sub> [--name <label>]
ctxt principal identity remove <principal-id> <identity-id>
```

| Flag | Subcommand(s) | Purpose |
|---|---|---|
| `--kind <k>` | `list`, `create` | Principal kind (`person`, `device`, `peer`) |
| `--status <st>` | `list` | Filter by principal status |
| `--name <label>` | `create`, `identity add` | Display name / identity label |
| `--role <r>` | `create` | Role bundle (`admin`, `writer`, `reader`; repeatable; default by kind) |
| `--method` / `--issuer` / `--subject` | `identity add` | Proxy preset or OIDC issuer + asserted login/subject |

## Agent Recipes

```bash
# Create a reader principal and mint a single-use sign-in link
ctxt principal create --kind person --name "Alex" --role reader --format json
ctxt principal link prn_abc123

# List active principals and revoke a compromised account
ctxt principal list --format json
ctxt principal revoke prn_xyz999 --confirm=yes --confirm-token=<sha>
```

## Next Steps

- Share knowledge with principals via audiences/grants: [share.md](share.md) · Host namespace entitlements: [../dpkms/access-governance.md](../dpkms/access-governance.md)
