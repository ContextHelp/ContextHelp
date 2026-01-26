# Security Model Overview

This directory contains the detailed security model for ContextHelp, decomposed into focused documents.

---

## Security Model Components

### Core Documents

1. **[threat.md](threat.md)** — Threat modeling and attack vectors
2. **[boundaries.md](boundaries.md)** — Trust boundaries and isolation
3. **[controls.md](controls.md)** — Security controls (auth, encryption, validation)
4. **[compliance.md](compliance.md)** — Compliance standards (GDPR, SOC 2, OWASP)

### Supporting Documents

5. **[../log-sanitization.md](../log-sanitization.md)** — Log sanitization and secret detection
6. **[../validation.md](../validation.md)** — Input validation and secret patterns

---

## Security Model Summary

ContextHelp implements a **local-first, defense-in-depth security model** with:

- **Zero trust** for all external sources (registries, AI providers, plugins)
- **Minimal attack surface** through localhost-only defaults
- **Process isolation** for plugins and pipelines
- **Automatic secret protection** via detection and sanitization
- **Optional encryption** at rest for sensitive data
- **Explicit opt-in** for external features

---

## Core Security Guarantees

| Guarantee | Implementation |
|-----------|---------------|
| **Local-First** | No external communication by default |
| **No Auto-Telemetry** | Zero automatic data transmission |
| **Encrypted Storage** | Optional AES-256-GCM encryption at rest |
| **Process Isolation** | Sandboxed plugins with capability-based permissions |
| **Secrets Protection** | Automatic detection and log sanitization |
| **Schema Validation** | All external inputs validated against strict schemas |
| **Explicit Configuration** | Remote features require opt-in configuration |

---

## Protected Assets

ContextHelp protects four categories of critical assets:

### 1. User Data
- Knowledge objects (bookmarks, notes, documents)
- Entity relationships and knowledge graphs
- Personal information and preferences
- Search queries and activity logs

### 2. Credentials & Secrets
- API keys (OpenAI, Anthropic, etc.)
- Database passwords
- Registry authentication tokens
- Encryption keys and passphrases
- OAuth tokens

### 3. Configuration
- System settings and policies
- Pipeline definitions
- Agent profiles and scope rules
- Registry connections

### 4. System Integrity
- Core application code
- Plugin manifests
- Database schemas
- Configuration files

---

## Threat Categories

ContextHelp defends against 8 threat categories:

| Category | Summary | Details |
|----------|---------|---------|
| **Unauthorized Access** | File system, network, database access control | [threat.md](threat.md#1-unauthorized-access) |
| **Credential Exposure** | API keys, passwords leaked in logs/files | [threat.md](threat.md#2-credential-exposure) |
| **Configuration Injection** | Malicious config modifying behavior | [threat.md](threat.md#3-configuration-injection) |
| **Plugin/Pipeline Exploitation** | Malicious code execution | [threat.md](threat.md#4-plugin-pipeline-exploitation) |
| **Data Exfiltration** | Unauthorized data transmission | [threat.md](threat.md#5-data-exfiltration) |
| **Supply Chain Attacks** | Compromised dependencies/plugins | [threat.md](threat.md#6-supply-chain-attacks) |
| **Denial of Service** | Resource exhaustion | [threat.md](threat.md#7-denial-of-service) |
| **Privilege Escalation** | Gaining unauthorized higher privileges | [threat.md](threat.md#8-privilege-escalation) |

---

## Security Boundaries

Five trust boundaries with different protection levels:

| Boundary | Trust Level | Protection Summary |
|----------|-------------|-------------------|
| **Local Machine** | Medium | File permissions, encryption, log sanitization |
| **External Registries** | Low (untrusted) | Schema validation, signature verification |
| **AI Providers** | Medium | TLS-only, credential protection, rate limiting |
| **Plugins** | Low (untrusted) | Capability-based sandbox, resource limits |
| **REST/gRPC APIs** | High (localhost), Low (remote) | Authentication, TLS, input validation |

See [boundaries.md](boundaries.md) for detailed boundary definitions.

---

## Security Controls

Comprehensive defense-in-depth controls:

- **Authentication & Authorization** — JWT tokens, mTLS, session-based auth
- **Encryption** — AES-256-GCM at rest, TLS 1.2+ in transit
- **Data Validation** — JSON schema, size limits, type checking
- **Process Isolation** — Plugin sandboxing, memory/CPU limits
- **Audit Logging** — Comprehensive event logging with sanitization

See [controls.md](controls.md) for implementation details.

---

## Compliance & Standards

ContextHelp aligns with industry security standards:

- **OWASP Top 10** — Mitigations for all top 10 vulnerabilities
- **NIST Cybersecurity Framework** — Identify → Protect → Detect → Respond → Recover
- **GDPR** — Data protection and privacy by design
- **SOC 2** — Security, availability, processing integrity

See [compliance.md](compliance.md) for detailed mappings.

---

## Quick Reference

### For Developers

1. **Threat Modeling**: Start with [threat.md](threat.md)
2. **Building Plugins**: Review [boundaries.md](boundaries.md#boundary-4-plugins)
3. **Handling Secrets**: See [../secret-management.md](../secret-management.md)
4. **Input Validation**: Check [../validation.md](../validation.md)

### For Security Auditors

1. **Threat Analysis**: [threat.md](threat.md)
2. **Security Controls**: [controls.md](controls.md)
3. **Compliance Mapping**: [compliance.md](compliance.md)
4. **Incident Response**: [../secret-management.md](../secret-management.md#incident-response)

### For DevOps/SREs

1. **Deployment Security**: [controls.md](controls.md#deployment-security)
2. **Container Security**: [../configuration-security.md](../configuration-security.md#container-configuration)
3. **Secret Management**: [../secret-management.md](../secret-management.md)
4. **Monitoring**: [controls.md](controls.md#audit-logging)

---

## Future Enhancements

Planned security improvements:

- **WASM Sandboxing**: Replace Go plugin sandbox with WebAssembly
- **Registry Signatures**: Cryptographic verification of registry data
- **Distributed Trust**: Multi-signature support for critical operations
- **Hardware Keys**: Hardware security module support
- **Zero-Knowledge Proofs**: Privacy-preserving operations
- **Formal Verification**: Mathematical proof of security properties

---

## Security Contacts

For security issues:

- **Security Team**: security@example.com
- **Responsible Disclosure**: Please follow responsible disclosure process
- **Hall of Fame**: Acknowledge successful security researchers

---

## Related Documentation

### Security Documents
- [../security-model.md](../security-model.md) — Comprehensive security model
- [../secret-management.md](../secret-management.md) — Secret lifecycle
- [../configuration-security.md](../configuration-security.md) — Secure configuration
- [../secrets-validation-and-log-sanitization.md](../secrets-validation-and-log-sanitization.md) — Pattern detection

### Architecture Documents
- [../../dpkms/security.md](../../dpkms/security.md) — dPKMS security
- [../../plugins/plugin-isolation.md](../../plugins/plugin-isolation.md) — Plugin sandboxing
- [../../decisions/ADR-019-encryption-and-privacy.md](../../decisions/ADR-019-encryption-and-privacy.md) — Encryption ADR
- [../../decisions/ADR-023-authentication-authorization-model.md](../../decisions/ADR-023-authentication-authorization-model.md) — Auth ADR
- [../../decisions/ADR-027-plugin-isolation-sandboxing.md](../../decisions/ADR-027-plugin-isolation-sandboxing.md) — Isolation ADR

---

**Last Updated**: 2026-01-26
**Version**: 1.0
**Status**: Active
**Maintained by**: Security Team
