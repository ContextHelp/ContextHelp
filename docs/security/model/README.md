# Security Model Overview

The security model for ctxt and dpkms, split by concern. Each page says what the code does today and where there is no protection.

## Pages

1. **[threat.md](threat.md)** — threats, the mitigations that exist, and the gaps
2. **[boundaries.md](boundaries.md)** — trust boundaries: local machine, registries, AI providers, installed steps, APIs
3. **[controls.md](controls.md)** — controls: inbound auth, secrets, policy rules, validation, audit log, deployment
4. **[compliance.md](compliance.md)** — OWASP Top 10 mapping; GDPR and SOC 2 pointers

Supporting: [../secrets-validation-and-log-sanitization.md](../secrets-validation-and-log-sanitization.md) (config secret scan; logs are not sanitized), [../validation.md](../validation.md) (input validation).

## Summary

| Property | Today |
|----------|-------|
| Loopback by default | `server.access: private` (default) binds `127.0.0.1` and authenticates nobody |
| Remote access needs auth | `protected` / `public` bind all interfaces and refuse to start without `server.auth.provider: static` and at least one token (32+ characters, roles `admin` / `writer` / `reader`) |
| Browser attacks on the local API | Host header check (DNS rebinding), cross-origin guard, JSON-only request bodies |
| No telemetry | Nothing is sent anywhere unless you configure a provider, registry, federation peer or remote store |
| Secrets in config | `ctxt config validate` / `ctxt config doctor` flag plaintext secrets; provider keys belong in the secrets backend (`dpkms secret set`) or env |
| Registry data | Ed25519 response signatures when the registry declares a key (`registries_global.require_signatures` makes them mandatory); per-registry trust level gates entity writes |
| Audit | Append-only audit log in the dpkms database (security events, dedup, fan-out, doctor runs; not object edits); `ctxt audit list`, `ctxt audit export`; `security.alerts` posts auth-failure and ACL-denial bursts to a webhook or SMTP. The `audit:` config block is read by no code |
| Encryption at rest | **None.** Use OS disk encryption. [ADR-019](../../decisions/ADR-019-encryption-and-privacy.md) is design |
| TLS | **None in dpkms.** Terminate TLS at a reverse proxy |
| Log sanitization | **None.** Logs are written as-is |
| Step sandbox | **None.** No pipeline executes installed external steps yet; [ADR-055](../../decisions/ADR-055-sandbox-isolation-levels.md) is design. There is no plugin system ([ADR-078](../../decisions/ADR-078-no-plugin-system.md)) |
| Rate limiting | **None.** `server.quotas` caps the entity-serving surface per principal |

## Quick reference

- Building on the API: [controls.md](controls.md#inbound-authentication) and [Inbound auth, roles and scopes](../../manual/reference/config-and-permissions.md#inbound-auth-roles-and-scopes)
- Handling secrets: [../secret-management.md](../secret-management.md), [../secrets-backends.md](../secrets-backends.md)
- Exposing dpkms: [controls.md](controls.md#deployment) and [Put dpkms Behind a Reverse Proxy](../../manual/operations/reverse-proxy.md)
- Leaked secret: [../secret-management.md](../secret-management.md#incident-response), [../../operations/incident-response.md](../../operations/incident-response.md)

## Reporting a vulnerability

See [../../SECURITY.md](../../SECURITY.md).

## Related

- [../security-model.md](../security-model.md) — single-page security model
- [../../dpkms/security.md](../../dpkms/security.md) — dpkms security
- [ADR-019](../../decisions/ADR-019-encryption-and-privacy.md), [ADR-023](../../decisions/ADR-023-authentication-authorization-model.md), [ADR-078](../../decisions/ADR-078-no-plugin-system.md)
