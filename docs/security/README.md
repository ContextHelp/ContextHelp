# Security Documentation

Security features of ctxt and dpkms, and the places where there is no
protection.

## Contents

- **[Secrets Backends](./secrets-backends.md)** — where ctxt reads API keys from: env, keychain, age-file, 1Password, GitHub secrets
- **[Secrets Validation and Log Sanitization](./secrets-validation-and-log-sanitization.md)** — the config secret scan, config file permissions, which output shows secrets, security alerts
- **[Log Sanitization](./log-sanitization.md)** — there is none
- **[Security Model](./security-model.md)** — threat model and guarantees
- **[Secret Management Best Practices](./secret-management.md)** — handling and rotating secrets
- **[Configuration Security](./configuration-security.md)** — securing config files and environment variables

Related:

- [dPKMS Security](../dpkms/security.md)
- [Security environment variables](../environment-variables/security.md) — secrets backend, `CTXT_POLICY_FILE`, backup passphrase
- [Inbound auth, roles and scopes](../manual/reference/config-and-permissions.md#inbound-auth-roles-and-scopes)

## What protects you today

- **Config secret scan.** `ctxt config validate` and `ctxt config doctor` flag values that look like plaintext secrets and print only a 4-character hint.
- **Config file permissions.** `ctxt config doctor` warns on group- or world-readable config; `--fix` sets `0600`.
- **Secret input.** `dpkms secret set` refuses a secret as a positional argument.
- **Inbound auth.** `server.access` (`private` / `protected` / `public`) and static tokens with roles and scopes.
- **Policy rules.** kit CEL rules in `policy/ctxt.yaml`, loaded by `dpkms serve`.
- **Security alerts.** Auth-failure and ACL-denial thresholds write audit entries and, if configured, post to a webhook or SMTP (`security.alerts`).

## What does not

- **Logs are not sanitized.** `slog` output, including `-V` / `--verbose`, is written as-is; no config key or env var redacts it. Check logs before sharing them.
- **`ctxt config show --format json|yaml`** prints the effective config unredacted, tokens included.
- **No plugin sandbox.** No plugin is loaded at runtime; manifest permissions are validated, not enforced. [Plugin Isolation](../plugins/plugin-isolation.md) and [ADR-027](../decisions/ADR-027-plugin-isolation-sandboxing.md) are design.

Details: [What output shows secrets](./secrets-validation-and-log-sanitization.md#what-output-shows-secrets).

## If a secret leaked

1. Rotate it — [Secret Rotation](./secret-management.md#secret-rotation).
2. Follow [Incident Response](./secret-management.md#incident-response).
3. Scan logs and data directories for other copies, e.g. with `gitleaks detect --no-git --redact` (see [the validation guide](./secrets-validation-and-log-sanitization.md#what-output-shows-secrets)).

## Related ADRs

- [ADR-019: Encryption and Privacy](../decisions/ADR-019-encryption-and-privacy.md)
- [ADR-023: Authentication and Authorization Model](../decisions/ADR-023-authentication-authorization-model.md)
- [ADR-027: Plugin Isolation and Sandboxing](../decisions/ADR-027-plugin-isolation-sandboxing.md) (not implemented)

## Reporting a vulnerability

Do not disclose publicly. See [../SECURITY.md](../SECURITY.md) for how to report.
