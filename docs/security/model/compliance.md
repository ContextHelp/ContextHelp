# Compliance & Standards

How ctxt and dpkms map to common security frameworks. No certification exists and none is claimed; this page records which controls exist so an operator can judge fit.

Operational guidance (data residency, retention, erasure, dependency review) lives in [../../operations/compliance.md](../../operations/compliance.md).

## OWASP Top 10 (2021)

| Category | What exists | Gaps |
|----------|-------------|------|
| A01 Broken Access Control | `private` binds loopback; `protected` / `public` require static tokens; one scope per route and gRPC method; Host check, cross-origin guard | no per-user accounts or object-level ACLs |
| A02 Cryptographic Failures | outbound HTTPS with certificate verification; Ed25519 registry signatures; signed config bundles | no encryption at rest; no TLS in dpkms (use a proxy); `dpkms backup` unencrypted |
| A03 Injection | parameterized SQL; RSQL compiled to placeholders with a fixed field list; config decoded strictly | — |
| A04 Insecure Design | local-only defaults; serve refuses `protected` / `public` without auth; policy rules on destructive pipeline operations | — |
| A05 Security Misconfiguration | `ctxt config validate` / `doctor` (schema, plaintext secrets, permissions); token length and placeholder checks | a `private` instance behind a proxy is open |
| A06 Vulnerable Components | `govulncheck`, gosec and Trivy in the release workflow; pinned tool versions in `mise.toml` | — |
| A07 Authentication Failures | 32+ character tokens, placeholder rejection, constant-time compare; auth-failure alerts | no token expiry; no lockout |
| A08 Data Integrity Failures | registry signatures (optional unless `require_signatures`); entity integrity guard; registry trust levels | installed steps carry no signature check |
| A09 Logging & Monitoring | append-only audit log for security events; `security.alerts` to webhook / SMTP; `ctxt audit list|export` | logs not sanitized; object edits not audited; no audit forwarding |
| A10 SSRF | egress only to configured providers, registries and peers | captured URLs are fetched without a private-address block |

## NIST CSF

- **Identify**: assets and boundaries in [threat.md](threat.md) and [boundaries.md](boundaries.md).
- **Protect**: [controls.md](controls.md).
- **Detect**: audit log security events and `security.alerts`; `dpkms healthcheck` / `ctxt status`.
- **Respond**: [../../operations/incident-response.md](../../operations/incident-response.md), [../secret-management.md](../secret-management.md#incident-response).
- **Recover**: `dpkms backup` / `dpkms restore` (SQLite); `ctxt config backup` / `restore`.

## GDPR

- **Erasure (Art. 17)**: `ctxt delete` (admin token) by ID, tag, mention, type or `--all`; `dpkms housekeeping prune --before <date>`. Audit entries are not removed.
- **Minimisation and storage limitation**: no retention settings; data stays until deleted.
- **Security of processing (Art. 32)**: see the A02 gap; use OS disk encryption for personal data.
- **Processors**: hosted AI providers receive the content you enrich with them; keep `providers.*.backend` local (Ollama) to avoid that.

## SOC 2

No SOC 2 report exists. The control mapping in [../../operations/compliance.md](../../operations/compliance.md#soc-2-control-mapping) is a self-assessment.

## Related

- [README.md](README.md), [threat.md](threat.md), [controls.md](controls.md)
