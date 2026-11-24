# `ctxt auth`

Inspect your authenticated principal on the active `dpkms` instance and manage your own API keys, bound identities, and browser sessions.

- **Use `ctxt auth`** for self-service identity/key/session operations.
- **Use `ctxt principal`** (admin) to manage other principals ([principal.md](principal.md)) or `dpkms auth key create` for direct host bootstrap ([../dpkms/access-governance.md](../dpkms/access-governance.md)).

## Execution Profile

| Subcommand | Side effect |
|---|---|
| `whoami`, `key list`, `identity list`, `session list` | `read` |
| `key create`, `key revoke`, `identity remove`, `session end` | `write` / `destructive` |

## Subcommands & Flags

```bash
ctxt auth whoami
ctxt auth key create [--name <label>] [--expires <dur-or-date>] [--scope <scope>...]
ctxt auth key list
ctxt auth key revoke <key-id>
ctxt auth identity list
ctxt auth identity remove <identity-id>
ctxt auth session list [--all]
ctxt auth session end <session-id>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--name <label>` | `key create` | `""` | Human label for the key (e.g. `"laptop CLI"`) |
| `--expires <expr>` | `key create` | never | Duration (`720h`, `30d`) or date (`2026-12-31`) |
| `--scope <s`> | `key create` | all allowed | Narrow key to listed scopes (repeatable) |
| `--all` | `session list` | `false` | Include ended (expired/revoked) sessions |

## Agent Recipes

```bash
# Confirm active principal before privileged operations
ctxt auth whoami --format json

# Mint a scoped, 30-day API key (secret is printed once — never log it)
ctxt auth key create --name "ci-reader" --expires 30d --scope read:objects

# Audit active browser sessions
ctxt auth session list --format json
```

## Gotchas & Next Steps

- `auth key create` prints the raw secret key **once**; store it in a secret manager or `dpkms secret` immediately and never echo it into logs.
- Next: administer other principals ([principal.md](principal.md)) or sharing ([share.md](share.md)).
