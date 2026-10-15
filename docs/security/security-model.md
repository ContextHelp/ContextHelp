# Security Model for ContextHelp

This document defines the comprehensive security model, threat analysis, and protection mechanisms for ContextHelp.

## Executive Summary

ContextHelp implements a **local-first, defense-in-depth security model** that protects against unauthorized access, data exfiltration, secret exposure, and malicious execution. The system treats all external sources as untrusted and enforces isolation at multiple levels.

### Core Security Guarantees

> **Pre-alpha:** encryption at rest (ADR-019) is designed but not active at
> runtime. There is no plugin system ([ADR-078](../decisions/ADR-078-no-plugin-system.md)). API authentication is active:
> static tokens with roles and scopes on protected and public instances.

- **Local-First**: Minimal external communication by default ✅
- **No Auto-Telemetry**: Zero automatic data transmission ✅
- **Loopback by Default**: a `private` instance (the default) binds `127.0.0.1` only ✅
- **Authenticated Remote Access**: `protected` / `public` instances require static tokens with roles ✅
- **Encrypted Storage**: ⚠️ Planned — optional AES-256-GCM at rest (ADR-019)
- **Process Isolation**: ⚠️ Planned — sandboxing for installed steps (ADR-055)
- **Secrets Protection**: plaintext-secret scan of config (`ctxt config validate`); logs are **not** sanitized
- **Explicit Configuration**: Remote features require opt-in configuration ✅

## Threat Model

### Protected Assets

ContextHelp protects the following critical assets:

1. **User Data**
   - Knowledge objects (bookmarks, notes, documents)
   - Entity relationships and knowledge graphs
   - Personal information and preferences
   - Search queries and activity logs

2. **Credentials & Secrets**
   - API keys (OpenAI, Anthropic, etc.)
   - Database passwords
   - Registry authentication tokens
   - Encryption keys and passphrases
   - OAuth tokens

3. **Configuration**
   - System settings and policies
   - Pipeline definitions
   - Agent profiles and scope rules
   - Registry connections

4. **System Integrity**
   - Core application code
   - Database schemas
   - Configuration files

### Threat Categories

#### 1. Unauthorized Access

**Threat**: Unauthorized users or processes access stored knowledge or configuration

**Attack Vectors**:
- File system permissions misconfiguration
- Unencrypted storage files
- Network access to unprotected APIs
- Local privilege escalation

**Mitigations**:
- Localhost-only API binding by default (`server.access: private`)
- Static-token authentication with role-based scopes on protected/public instances
- Host header allowlist and cross-origin guard against DNS rebinding and CSRF
- Config file permission check (`ctxt config doctor`, `--fix` sets 0600)
- OS disk encryption for the data directory (no built-in encryption yet)

#### 2. Credential Exposure

**Threat**: API keys, passwords, and tokens are exposed in logs, error messages, or files

**Attack Vectors**:
- Accidental inclusion in log files
- Debug output with sensitive values
- Configuration file disclosure
- Error stack traces
- Version control history

**Mitigations**:
- Plaintext-secret scan of config (`ctxt config validate`, `ctxt config doctor`)
- Configuration validation (rejects `${VAR}` values, short or placeholder tokens)
- Registry tokens in the OS keychain, never in config
- No log sanitization: logs and `ctxt config show --format json` print values as-is
- Pre-commit hook integration
- Secrets management guidance

#### 3. Configuration Injection

**Threat**: Malicious configuration modifies system behavior

**Attack Vectors**:
- Untrusted registry responses
- Compromised configuration files
- Environment variable injection
- YAML/JSON deserialization attacks

**Mitigations**:
- Strict schema validation
- Type checking on all config values
- Registry data treated as untrusted
- Configuration file integrity checks
- Schema versioning for compatibility

#### 4. Installed Step Exploitation

**Threat**: A malicious or compromised step installed from a step registry executes unauthorized code

**Attack Vectors**:
- Steps access system resources
- Steps exfiltrate data
- Steps execute arbitrary commands
- Steps modify protected data

**Mitigations**:
- Steps run out of process (ADR-058), never inside dpkms
- Step install and uninstall need the `admin:steps` scope (admin role); a registry install needs a `trusted` or `sandboxed` registry and a matching archive sha256
- The executable's sha256 is recorded at install and checked before every run
- Process isolation: minimal environment, private working directory, timeout, CPU-time and stdout limits; no filesystem or network isolation
- Container isolation (required for `sandboxed` registries): no network, read-only root, no capabilities, memory and CPU limits
- A step may change only its writable object fields; a failing step fails its object

#### 5. Data Exfiltration

**Threat**: Knowledge objects or user data transmitted without authorization

**Attack Vectors**:
- Misconfigured external registries
- Malicious installed steps
- Compromised AI provider integrations
- Unencrypted network traffic

**Mitigations**:
- Explicit opt-in for external features
- No default external communication
- TLS for all external connections
- Registry permission scoping
- Audit logging of data access

#### 6. Supply Chain Attacks

**Threat**: Compromised dependencies, steps, or registries

**Attack Vectors**:
- Malicious steps published to a step registry
- Compromised registry data
- Vulnerable dependencies
- Typosquatting on step names

**Mitigations**:
- Registry signature verification (future)
- Dependency scanning
- Step review before installation
- Allowlist/blocklist support
- Version pinning recommendations

#### 7. Denial of Service

**Threat**: System becomes unavailable through resource exhaustion

**Attack Vectors**:
- Infinite loops in pipelines
- Unbounded memory allocation
- Large input processing
- Registry response flooding

**Mitigations**:
- Input size limits
- Execution timeouts
- Memory limits per pipeline/step
- Rate limiting on registry access
- Job queue backpressure
- Resource monitoring and alerts

#### 8. Privilege Escalation

**Threat**: Lower-privileged operations gain higher privileges

**Attack Vectors**:
- File descriptor inheritance
- Environment variable leakage
- Unsafe command execution
- Unquoted shell arguments

**Mitigations**:
- Strict file permission handling
- Environment variable sanitization
- No shell command execution
- Argument quoting and validation
- User capability enforcement

## Security Boundaries

### Boundary 1: Local Machine

**Trust Level**: Medium (Owner trusted, machine not fully trusted)

**Components**:
- CLI application
- Local storage (SQLite, JSON)
- Configuration files
- Log files

**Protection**:
- File permission validation (0600 for secrets)
- User ownership checks
- Encryption at rest (optional)
- Log sanitization

### Boundary 2: External Registries

**Trust Level**: Low (Completely untrusted)

**Components**:
- Registry endpoints
- Registry responses
- Registry authentication credentials
- Registry metadata

**Protection**:
- All responses validated against schema
- No direct use of registry data
- Schema-enforced transformation
- Version pinning support
- Signature verification (future)
- Cache validation (ETag/Last-Modified)

### Boundary 3: AI Providers

**Trust Level**: Medium (Trusted service, but external)

**Components**:
- API endpoints (OpenAI, Anthropic, Ollama)
- API requests and responses
- Model outputs
- API credentials

**Protection**:
- Explicit opt-in configuration
- TLS-only communication
- Credentials stored securely
- Request/response logging sanitized
- Rate limiting
- Fallback to local models

### Boundary 4: Installed Steps

**Trust Level**: Low (Untrusted code)

**Components**:
- Step code installed from a step registry
- Step configuration
- Step data and network access

**Protection**:
- Out-of-process execution (ADR-058, designed)
- Sandbox isolation levels (ADR-055)
- Memory and CPU limits
- File system restrictions
- Network access whitelisting

### Boundary 5: REST/gRPC APIs

**Trust Level**: High (Localhost), Low (Remote)

**Components**:
- HTTP/gRPC endpoints
- API requests/responses
- Authentication tokens
- TLS certificates

**Protection**:
- Localhost binding by default; `protected` / `public` bind all interfaces
  and require inbound auth
- Bearer / `X-API-Key` static tokens; one scope per route and gRPC method
- Host header allowlist, cross-origin guard, JSON-only request bodies
- `/healthz` details hidden from anonymous callers on public instances
- No TLS in dpkms: terminate it at a reverse proxy
- No rate limiting; per-principal quotas (`server.quotas`) cap the
  entity-serving surface only

## Security Controls

### Authentication & Authorization

**Access classes** (`server.access`):
- `private` (default): binds `127.0.0.1`, authenticates nobody, every local
  caller holds every scope. The Host check is always on.
- `protected` / `public`: bind all interfaces and require
  `server.auth.provider: static` with at least one token; `dpkms serve`
  refuses to start otherwise.

**REST, gRPC and MCP** (`internal/auth`, active on protected/public):
- Static tokens from `server.auth.static.tokens`, each with a `principal`
  and one or more roles (`admin`, `writer`, `reader`); at least 32
  characters, no `REPLACE_ME`. Sent as `Authorization: Bearer` or
  `X-API-Key`.
- Roles expand to scopes (`read:objects`, `write:objects`, `delete:edges`,
  `read:mcp`, `admin:audit`, ...). Every `/api/v1` route and gRPC method
  requires one; `401` without a valid token, `403 INSUFFICIENT_SCOPE`
  without the scope.
- Web UI sessions: `ctxt ui open` mints a single-use sign-in link; the
  session holds the intersection of the web UI's scope set and its
  token's scopes, expires on `server.ui.session.idle_ttl` / `max_ttl`, and
  ends when the token is removed. `dpkms session list|revoke` manage them.
- Federation push on non-private instances also needs `federation.token`.
- Not implemented: `oidc` and `mtls` providers (reserved names), per-user
  accounts.

Reference: [Inbound auth, roles and scopes](../manual/reference/config-and-permissions.md#inbound-auth-roles-and-scopes),
[Web UI sessions](../manual/reference/config-and-permissions.md#web-ui-sessions).

**CLI**:
- `server.token` / `server.urls[].token` hold the token ctxt sends to a
  protected instance
- Local-only operation needs no token

### Encryption

> ⚠️ **Planned** — encryption at rest designed in ADR-019; not yet active at runtime.
> Operators handling PII must use OS-level disk encryption (FileVault, LUKS, BitLocker)
> until ADR-019 lands.

**At Rest** (planned):
- Optional AES-256-GCM encryption for storage (ADR-019)
- Configurable key derivation (PBKDF2, Argon2)
- Transparent encryption/decryption
- Key rotation support

**In Transit** (operator-configured):
- TLS 1.2+ for external connections — not enforced by the server; configure via
  reverse proxy (nginx, Caddy, etc.) or PostgreSQL `sslmode=require`
- Certificate validation active for outbound API calls
- No plaintext communication for remote features (client enforces HTTPS URLs)

**Configuration**:
- Sensitive values stored separately
- File permission enforcement
- Environment variable usage recommended

### Data Validation

**Input Validation**:
- JSON schema validation for all payloads
- Size limits on all inputs
- Type checking on all fields
- Whitelist-based validation where possible

**Configuration Validation**:
- YAML/JSON schema validation
- Type checking on values
- Enum validation for choices
- URL format validation
- Path traversal prevention

**External Data**:
- Registry response schema validation
- AI provider response validation
- Certificate validation

### Process Isolation

**Installed Step Isolation**:
- Separate process or container (ADR-058, ADR-055; designed)
- Memory and CPU time limits
- Disk I/O and network access restrictions

**Pipeline Isolation**:
- Separate execution context
- Input/output validation
- Timeout enforcement
- Resource limits
- No access to other pipelines' data

### Audit Logging

**Events Logged**:
- Repeated authentication failures and ACL denials past the
  `security.alerts` thresholds (`security.auth_failure`,
  `security.acl_denial`) and quota exhaustion (`security.quota_exhausted`)
- Duplicate detection (`dedup.*`) and fan-out enrichment (`fanout.completed`)
- `ctxt doctor` runs (`doctor.run`)
- Not recorded: successful sign-ins, configuration changes, step installs

**Log Protection**:
- The audit log lives in the dpkms database; read it with `ctxt audit list`
  / `ctxt audit export` (`admin:audit` scope over HTTP)
- No forwarding: ship it to a SIEM with a scheduled `ctxt audit export`
- Security alerts (`security.alerts`) to the audit log, a webhook or SMTP.
  Their audit rows go through a bounded queue (1024 rows) with one writer:
  `dpkms serve` writes the queued rows before it closes the store on
  shutdown (within `jobs.drain_timeout`). A full queue drops rows instead
  of slowing requests, with a `security: audit rows dropped` warning
  (`dropped_total`) at most every 10s. Each webhook POST or SMTP session
  ends after `security.alerts.delivery_timeout` (10s), and shutdown
  cancels the ones still running when `jobs.drain_timeout` runs out
- No sanitization: process logs are written as-is (see
  [Secrets Validation and Log Sanitization](secrets-validation-and-log-sanitization.md#what-output-shows-secrets))

## Compliance & Standards

### OWASP Top 10

| Category | Mitigation |
|----------|-----------|
| A01 Broken Access Control | User/permission validation, local-first binding |
| A02 Cryptographic Failures | Optional encryption, TLS required for remote |
| A03 Injection | Input validation, parameterized queries, schema validation |
| A04 Insecure Design | Threat modeling, secure defaults, least privilege |
| A05 Security Misconfiguration | Validation tools, secure defaults, warnings |
| A06 Vulnerable Components | Dependency scanning, version management |
| A07 Authentication Failure | Mandatory static-token auth off loopback, 32-char minimum, placeholder rejection, constant-time compare |
| A08 Data Integrity | Schema validation, HMAC verification |
| A09 Logging & Monitoring | Audit log, security alerts to webhook/SMTP (no audit forwarding, no log sanitization) |
| A10 SSRF | No automatic external calls, whitelisting |

### Security Standards

- **NIST Cybersecurity Framework**: Identify → Protect → Detect → Respond → Recover
- **ISO 27001**: Information security management
- **GDPR**: Data protection and privacy
- **SOC 2**: Security, availability, processing integrity

## Deployment Security

### Local Development

**Secure Defaults**: the defaults are already local-only. `dpkms serve`
binds 127.0.0.1 while `server.access` is `private` (the default), storage
is SQLite, telemetry is off. No env var is needed:

```bash
dpkms serve --verbose   # debug logs; there is no log-level env var
```

There is no storage-encryption or log-sanitization setting; see
[Encryption](#encryption) for at-rest protection.

### Server Deployment

**Production Requirements**:
```bash
CH_STORAGE_TYPE=postgres           # DSN goes in storage.path
PGPASSWORD=$(vault kv get -field=password secret/db)
CTXT_SECRETS_BACKEND=keyring       # provider API keys from the OS keychain
CH_DISABLE_TELEMETRY=true
```

Never put a `private` instance behind a reverse proxy: it authenticates
nobody, so the proxy opens the whole API. For remote access use
`server.access: protected` behind a TLS-terminating proxy
([Put dpkms Behind a Reverse Proxy](../manual/operations/reverse-proxy.md)),
or tunnel to a private instance over SSH. `protected` and `public`
instances need inbound auth under `server.auth`, or serve refuses to
start — see
[Inbound auth, roles and scopes](../manual/reference/config-and-permissions.md#inbound-auth-roles-and-scopes).

**Network Security**:
- Run behind reverse proxy (nginx, HAProxy)
- Enable TLS/HTTPS
- Restrict access by IP
- Use firewall rules
- Monitor network traffic

### Container Security

**Docker Security**:
```dockerfile
# Secure base image
FROM alpine:latest

# Run as non-root
USER contexthelp

# Read-only root filesystem
RUN chmod 755 /
USER contexthelp

# No privileged mode
# No excessive capabilities
# Volume for persistent data
```

**Kubernetes Security**:
- Pod security policies
- Network policies
- RBAC for service accounts
- Secret volume mounts
- Resource limits
- Security context enforcement

## Security Testing

### Test Coverage

- **Unit Tests**: Input validation, crypto functions
- **Integration Tests**: Database encryption, registry validation
- **Security Tests**: Injection attempts, privilege escalation
- **Penetration Testing**: Manual security review
- **Dependency Scanning**: Known vulnerabilities

### Continuous Security

- **SAST**: Static analysis for vulnerabilities
- **DAST**: Dynamic testing of APIs
- **Dependency Scanning**: Regular updates
- **Code Review**: Security-focused review process
- **Monitoring**: Runtime security monitoring

## Incident Response

### Detection

Monitor for:
- Unusual registry access patterns
- Failed authentication attempts
- `security.*` entries in the audit log (`ctxt audit list --event-type security.auth_failure`)
- Step crashes or resource overuse
- Unauthorized file access

### Response Procedures

1. **Containment**: Isolate affected systems
2. **Eradication**: Remove malicious components
3. **Recovery**: Restore from clean backup
4. **Lessons Learned**: Update security controls

See [Secret Management - Incident Response](./secret-management.md#incident-response) for detailed procedures.

## Future Enhancements

Planned security improvements:

- **WASM Sandboxing**: WebAssembly runtime for installed steps
- **Registry Signatures**: Cryptographic verification of registry data
- **Distributed Trust**: Multi-signature support for critical operations
- **Hardware Keys**: Hardware security module support
- **Zero-Knowledge Proofs**: Privacy-preserving operations
- **Formal Verification**: Mathematical proof of security properties

## Security Contacts

For security issues:

- **Security Team**: security@example.com
- **Responsible Disclosure**: Please follow responsible disclosure process
- **Hall of Fame**: Acknowledge successful security researchers

## References

### Internal Documentation
- [ADR-019: Encryption and Privacy](../decisions/ADR-019-encryption-and-privacy.md)
- [ADR-023: Authentication and Authorization](../decisions/ADR-023-authentication-authorization-model.md)
- [ADR-055: Sandbox Isolation Levels](../decisions/ADR-055-sandbox-isolation-levels.md)
- [ADR-078: No Plugin System](../decisions/ADR-078-no-plugin-system.md)
- [dPKMS Security](../dpkms/security.md)

### External Standards
- [NIST Cybersecurity Framework](https://www.nist.gov/cyberframework)
- [OWASP Top 10](https://owasp.org/www-project-top-ten/)
- [CWE Top 25](https://cwe.mitre.org/top25/)
- [GDPR - Data Protection](https://gdpr-info.eu/)

## Summary

ContextHelp's security model provides:

- **Defense in depth** with multiple protection layers
- **Local-first privacy** with explicit opt-in for external features
- **Zero trust** for all external sources
- **Automatic secret protection** through detection and sanitization
- **Process isolation** for installed steps and pipelines
- **Comprehensive audit logging** for compliance
- **Clear threat modeling** and documented mitigations

This ensures ContextHelp remains a trustworthy, secure foundation for personal and organizational knowledge management.

---

**Last Updated**: 2025-01-26
**Version**: 1.0
**Status**: Active
**Maintained by**: Security Team
