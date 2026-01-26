# Security Model for ContextHelp

This document defines the comprehensive security model, threat analysis, and protection mechanisms for ContextHelp.

## Executive Summary

ContextHelp implements a **local-first, defense-in-depth security model** that protects against unauthorized access, data exfiltration, secret exposure, and malicious execution. The system treats all external sources as untrusted and enforces isolation at multiple levels.

### Core Security Guarantees

- **Local-First**: Minimal external communication by default
- **No Auto-Telemetry**: Zero automatic data transmission
- **Encrypted Storage**: Optional encryption at rest for sensitive data
- **Process Isolation**: Plugins and pipelines run in isolated contexts
- **Secrets Protection**: Automatic detection and sanitization
- **Schema Validation**: All external inputs validated against strict schemas
- **Explicit Configuration**: Remote features require opt-in configuration

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
   - Plugin manifests
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
- Localhost-only API binding by default
- File permission validation (600 for sensitive files)
- Optional storage encryption
- User permission validation before data access
- SELinux/AppArmor integration points

#### 2. Credential Exposure

**Threat**: API keys, passwords, and tokens are exposed in logs, error messages, or files

**Attack Vectors**:
- Accidental inclusion in log files
- Debug output with sensitive values
- Configuration file disclosure
- Error stack traces
- Version control history

**Mitigations**:
- Automatic log sanitization (all output redacted)
- Secret pattern detection
- Configuration validation
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

#### 4. Plugin/Pipeline Exploitation

**Threat**: Malicious or compromised plugins execute unauthorized code

**Attack Vectors**:
- Plugins access system resources
- Plugins exfiltrate data
- Plugins execute arbitrary commands
- Plugins modify protected data

**Mitigations**:
- Plugin sandboxing (capability-based)
- Restricted file system access
- No direct network access without permission
- No access to other plugin data
- Memory limits and execution timeouts
- Permission manifest validation

#### 5. Data Exfiltration

**Threat**: Knowledge objects or user data transmitted without authorization

**Attack Vectors**:
- Misconfigured external registries
- Malicious plugins
- Compromised AI provider integrations
- Unencrypted network traffic

**Mitigations**:
- Explicit opt-in for external features
- No default external communication
- TLS for all external connections
- Registry permission scoping
- Plugin data access restrictions
- Audit logging of data access

#### 6. Supply Chain Attacks

**Threat**: Compromised dependencies, plugins, or registries

**Attack Vectors**:
- Malicious plugin packages
- Compromised registry data
- Vulnerable dependencies
- Typosquatting on plugin names

**Mitigations**:
- Plugin manifest validation
- Registry signature verification (future)
- Dependency scanning
- Plugin permission review before installation
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
- Memory limits per pipeline/plugin
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

### Boundary 4: Plugins

**Trust Level**: Low (Untrusted code)

**Components**:
- Plugin code
- Plugin configuration
- Plugin data access
- Plugin network access

**Protection**:
- Capability-based permissions
- Sandbox execution environment
- Memory and CPU limits
- File system restrictions
- Network access whitelisting
- Data access scoping

### Boundary 5: REST/gRPC APIs

**Trust Level**: High (Localhost), Low (Remote)

**Components**:
- HTTP/gRPC endpoints
- API requests/responses
- Authentication tokens
- TLS certificates

**Protection**:
- Localhost binding by default
- Optional TLS termination
- Authentication/authorization checks
- Input validation and rate limiting
- Output sanitization
- CORS restrictions

## Security Controls

### Authentication & Authorization

**REST API**:
- Optional JWT token authentication
- Session-based authentication
- mTLS support for internal connections
- Per-user permission scopes

**gRPC API**:
- JWT token validation
- Per-connection authentication
- Stream-level authorization checks
- Credential validation

**CLI**:
- Local user authentication (file permissions)
- No network authentication needed
- Configuration file permission validation

### Encryption

**At Rest**:
- Optional AES-256-GCM encryption for storage
- Configurable key derivation (PBKDF2, Argon2)
- Transparent encryption/decryption
- Key rotation support

**In Transit**:
- Mandatory TLS 1.2+ for external connections
- Certificate validation
- HSTS headers for HTTP APIs
- No plaintext communication for remote features

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
- Plugin manifest validation
- Certificate validation

### Process Isolation

**Plugin Isolation**:
- Separate process or WASM runtime
- Capability-based permission model
- Memory limits (default: 256MB)
- CPU time limits (default: 5 minutes)
- Disk I/O restrictions
- Network access restrictions
- File system restrictions to plugin directory

**Pipeline Isolation**:
- Separate execution context
- Input/output validation
- Timeout enforcement
- Resource limits
- No access to other pipelines' data

### Audit Logging

**Events Logged**:
- Authentication attempts (successes and failures)
- Permission checks and denials
- Configuration changes
- Plugin installations/removals
- Registry access attempts
- Secrets detection (without exposing)
- Data access patterns
- Error conditions

**Log Protection**:
- Automatic sanitization of sensitive data
- Structured logging for analysis
- Immutable log appending
- Log rotation and retention
- Syslog integration for centralization

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
| A07 Authentication Failure | Optional auth, secure token handling |
| A08 Data Integrity | Schema validation, HMAC verification |
| A09 Logging & Monitoring | Audit logging, sanitization |
| A10 SSRF | No automatic external calls, whitelisting |

### Security Standards

- **NIST Cybersecurity Framework**: Identify → Protect → Detect → Respond → Recover
- **ISO 27001**: Information security management
- **GDPR**: Data protection and privacy
- **SOC 2**: Security, availability, processing integrity

## Deployment Security

### Local Development

**Secure Defaults**:
```bash
# Dev config
CH_DEBUG=true
CH_LOG_LEVEL=debug
CH_SECURITY_LOGGING_SANITIZE=true  # Even in dev
CH_STORAGE_TYPE=sqlite
CH_STORAGE_ENCRYPTION=false        # OK for local dev
```

### Server Deployment

**Production Requirements**:
```bash
# Production config
CH_HTTP_PORT=8080
CH_PUBLIC=false                    # Local only, or use reverse proxy
ENCRYPTION_ENABLED=true
ENCRYPTION_PASSPHRASE=${VAULT_KEY}
POSTGRES_HOST=db.internal.local
POSTGRES_PASSWORD=${VAULT_DB_PASS}
CH_DISABLE_TELEMETRY=true
```

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
- Secrets detected in logs
- Plugin crashes or resource overuse
- Unauthorized file access

### Response Procedures

1. **Containment**: Isolate affected systems
2. **Eradication**: Remove malicious components
3. **Recovery**: Restore from clean backup
4. **Lessons Learned**: Update security controls

See [Secret Management - Incident Response](./secret-management.md#incident-response) for detailed procedures.

## Future Enhancements

Planned security improvements:

- **WASM Sandboxing**: Replace Go plugin sandbox with WebAssembly
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
- [ADR-027: Plugin Isolation and Sandboxing](../decisions/ADR-027-plugin-isolation-sandboxing.md)
- [dPKMS Security](../dpkms/security.md)
- [Plugin Isolation](../plugins/plugin-isolation.md)

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
- **Process isolation** for plugins and pipelines
- **Comprehensive audit logging** for compliance
- **Clear threat modeling** and documented mitigations

This ensures ContextHelp remains a trustworthy, secure foundation for personal and organizational knowledge management.

---

**Last Updated**: 2025-01-26
**Version**: 1.0
**Status**: Active
**Maintained by**: Security Team
