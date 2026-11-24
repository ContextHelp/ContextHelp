# `dpkms` Host Access & Governance (`auth`, `entitlement`, `session`, `instance`, `doctor`, `audit`)

Host-local administrative commands for bootstrapping API keys, granting namespace entitlements, managing web UI sessions, gating direct DB access, auditing host access/encryption posture, and verifying the steward audit chain.

## Execution Profile

| Command | Side effect | Purpose |
|---|---|---|
| `dpkms doctor` | `read` | Check host access posture, file permissions, and encryption readiness |
| `dpkms audit verify` | `read` | Verify cryptographic integrity of the steward read-audit hash chain |
| `dpkms auth key create` | `write` | Mint an API key directly on the host (bootstrap owner or `prn_` principal) |
| `dpkms entitlement list\|grant\|revoke\|ungate` | `read` / `write` | Manage principals' namespace entitlements |
| `dpkms session list\|revoke` | `read` / `write` | Inspect or revoke web UI browser sessions |
| `dpkms instance set-access` | `write` | Record the access class (`private`/`protected`/`public`) gating direct DB opens |

## Subcommands & Flags

### `dpkms doctor` & `dpkms audit verify`

```bash
dpkms doctor [--strict]           # --strict fails on permissions findings instead of warning
dpkms audit verify
```

### `dpkms auth key create`

```bash
dpkms auth key create --principal <owner|prn_id> [--name <label>] [--expires <dur|date>]
```

| Flag | Default | Purpose |
|---|---|---|
| `--principal <p>` | `""` | Principal the key acts for (`owner` or `prn_` ID) |
| `--name <label>` | `""` | Key label (e.g. `"laptop CLI"`) |
| `--expires <expr>` | never | Duration (`720h`) or date (`2026-12-31`) |

### `dpkms entitlement`

```bash
dpkms entitlement list
dpkms entitlement grant <principal> <namespace...> [--expires <dur|date|rfc3339|never>]
dpkms entitlement revoke <principal> <namespace...>
dpkms entitlement ungate <principal>    # delete grant, returning principal to unrestricted access
```

### `dpkms session`

```bash
dpkms session list [--principal <id>] [--limit <n>] [--all]
dpkms session revoke <session-id> [--principal <id>]   # --principal revokes every active session of that principal
```

### `dpkms instance set-access`

```bash
dpkms instance set-access <class>
```

## Agent Recipes

```bash
# Audit host permissions & encryption posture strictly (suitable for CI/pre-deploy)
dpkms doctor --strict --format json

# Verify steward audit hash chain
dpkms audit verify

# Bootstrap an owner API key on a fresh host (secret printed once — never log it)
dpkms auth key create --principal owner --name "admin-cli" --expires 90d

# Grant a principal read namespaces for 30 days
dpkms entitlement grant prn_123 project.alpha --expires 30d

# Revoke every active web UI session for a principal
dpkms session revoke --principal prn_123
```

## Next Steps

- Client-side sharing, principals, approvals & steward requests: [../ctxt/share.md](../ctxt/share.md), [../ctxt/principal.md](../ctxt/principal.md), [../ctxt/steward.md](../ctxt/steward.md)
- Storage encryption at rest: [storage.md](storage.md)
