# Compliance & Standards

Compliance mapping for ContextHelp security controls.

> **Pre-alpha status:** Auth (ADR-023) and encryption at rest (ADR-019) are designed
> but not yet shipped. Controls below reflect what is actually implemented today.
> "Partial" = design exists, runtime behaviour not yet enforced.

---

## Overview

ContextHelp aligns with major security and privacy standards:

- **OWASP Top 10** — Web application security risks
- **NIST Cybersecurity Framework** — Risk management framework
- **GDPR** — Data protection and privacy
- **SOC 2** — Security, availability, and processing integrity
- **ISO 27001** — Information security management

---

## OWASP Top 10 (2021)

### A01: Broken Access Control

**Risk**: Users acting outside their intended permissions

**ContextHelp Mitigations**:
- Localhost-only API binding by default ✅
- File permission validation (0600/0700) ✅
- Plugin sandboxing with capability-based permissions ⚠️ planned (ADR-027)
- Authorization checks on all API endpoints ⚠️ planned (ADR-023)

**Note:** Code sample shows the intended design; not yet enforced at runtime.

**Compliance Level**: ⚠️ Partial (localhost binding + file perms active; authz not enforced)

---

### A02: Cryptographic Failures

**Risk**: Sensitive data exposed due to weak crypto

**ContextHelp Mitigations**:
- No plaintext secrets in configuration ✅
- Certificate validation for outbound TLS ✅
- AES-256-GCM encryption at rest ⚠️ planned (ADR-019)
- TLS 1.2+ for remote connections ⚠️ operator-configured; not enforced by server

**Note:** Config snippet shows planned feature; encryption not active in current release.

**Compliance Level**: ⚠️ Partial (no plaintext secrets; storage encryption and TLS not yet enforced)

---

### A03: Injection

**Risk**: Untrusted data executed as code or queries

**ContextHelp Mitigations**:
- Parameterized database queries
- JSON schema validation for all inputs
- Type checking on configuration values
- No shell command execution
- Input sanitization

**Implementation**:
```go
// Parameterized queries
db.Query("SELECT * FROM objects WHERE id = ?", objectID)

// Schema validation
validate := validator.New()
err := validate.Struct(request)
```

**Compliance Level**: ✅ Full

---

### A04: Insecure Design

**Risk**: Missing or ineffective security controls

**ContextHelp Mitigations**:
- Comprehensive threat modeling
- Secure defaults (localhost-only, encryption opt-in)
- Least privilege principle
- Defense in depth
- Security by design

**Documentation**:
- [threat.md](threat.md) — Threat model
- [boundaries.md](boundaries.md) — Trust boundaries
- [controls.md](controls.md) — Security controls

**Compliance Level**: ✅ Full

---

### A05: Security Misconfiguration

**Risk**: Insecure default settings or incomplete configurations

**ContextHelp Mitigations**:
- Configuration validation tools
- Secure defaults (localhost-only, no telemetry)
- Warning messages for insecure settings
- Pre-commit hooks for secrets
- File permission checks

**Implementation**:
```bash
# Configuration validation
ctxt config validate

# Security scanning
ctxt security validate config.yaml
```

**Compliance Level**: ✅ Full

---

### A06: Vulnerable and Outdated Components

**Risk**: Using components with known vulnerabilities

**ContextHelp Mitigations**:
- Dependency scanning in CI/CD
- Regular dependency updates
- Version pinning
- CVE monitoring
- Security advisory subscriptions

**CI/CD Integration**:
```yaml
security-scan:
  stage: security
  script:
    - go install golang.org/x/vuln/cmd/govulncheck@latest
    - govulncheck ./...
```

**Compliance Level**: ✅ Full

---

### A07: Identification and Authentication Failures

**Risk**: Weak authentication or session management

**ContextHelp Mitigations**:
- JWT token authentication ⚠️ planned (ADR-023)
- Secure session handling ⚠️ planned (ADR-023)
- Strong token generation ⚠️ planned (ADR-023)

**Note:** Auth system is designed; not yet enforced — server accepts unauthenticated
requests to localhost in the current release.

**Compliance Level**: ⚠️ Partial (localhost-only binding reduces exposure; auth not enforced)

---

### A08: Software and Data Integrity Failures

**Risk**: Code or data modified without verification

**ContextHelp Mitigations**:
- Schema validation for registry responses
- HMAC verification (future)
- Plugin manifest validation
- Signature verification (future)
- Immutable audit logs

**Implementation**:
```go
// Schema validation
func ValidateRegistryResponse(response []byte) error {
    var data RegistryResponse
    if err := json.Unmarshal(response, &data); err != nil {
        return err
    }
    return validate.Struct(data)
}
```

**Compliance Level**: ⚠️ Partial (signature verification planned)

---

### A09: Security Logging and Monitoring Failures

**Risk**: Insufficient logging to detect breaches

**ContextHelp Mitigations**:
- Comprehensive audit logging
- Automatic log sanitization
- Structured logging
- Security event tracking
- Syslog integration

**Events Logged**:
- Authentication attempts
- Permission checks
- Configuration changes
- Plugin installations
- Secret detections
- Data access

**Compliance Level**: ✅ Full

---

### A10: Server-Side Request Forgery (SSRF)

**Risk**: Application fetches remote resources without validation

**ContextHelp Mitigations**:
- No automatic external calls
- Explicit opt-in for external features
- URL validation
- Domain whitelisting
- TLS enforcement

**Implementation**:
```go
// Registry access requires configuration
registries:
  - name: trusted-registry
    url: https://registry.example.com  # Must be HTTPS
    enabled: true  # Explicit opt-in
```

**Compliance Level**: ✅ Full

---

## OWASP Compliance Summary

> ✅ = active in current release; ⚠️ = partial / planned; see per-category notes.

| Category | Compliance | Notes |
|----------|-----------|-------|
| A01 Broken Access Control | ⚠️ Partial | localhost binding ✅; authz not enforced |
| A02 Cryptographic Failures | ⚠️ Partial | no plaintext secrets ✅; encryption/TLS not enforced |
| A03 Injection | ✅ Full | Parameterized queries, schema validation |
| A04 Insecure Design | ✅ Full | Threat model, secure defaults |
| A05 Security Misconfiguration | ✅ Full | `ctxt config validate/lint`, file perms |
| A06 Vulnerable Components | ✅ Full | `govulncheck` in CI |
| A07 Authentication Failures | ⚠️ Partial | auth designed (ADR-023), not yet enforced |
| A08 Data Integrity Failures | ⚠️ Partial | Schema validation ✅; HMAC/signatures planned |
| A09 Logging Failures | ✅ Full | Append-only audit log, log sanitization |
| A10 SSRF | ✅ Full | No auto-external calls, explicit opt-in |

**Overall**: 6/10 Full, 4/10 Partial (pre-alpha; improves as ADR-019/023/027 land)

---

## NIST Cybersecurity Framework

### 1. Identify

**Asset Management**:
- User data (knowledge objects, graphs)
- Credentials (API keys, passwords)
- Configuration (settings, pipelines)
- System integrity (code, schemas)

**Risk Assessment**:
- Threat modeling: [threat.md](threat.md)
- Attack vectors documented
- Risk levels assigned

**Compliance**: ✅ Full

---

### 2. Protect

**Access Control**:
- File permission validation ✅
- Capability-based permissions ⚠️ planned (ADR-027)
- Authentication/authorization ⚠️ planned (ADR-023)

**Data Security**:
- Encryption at rest ⚠️ planned (ADR-019)
- TLS 1.2+ in transit ⚠️ operator-configured
- Secure key derivation ⚠️ planned

**Awareness & Training**:
- Security documentation ✅
- Best practices guides ✅

**Compliance**: ⚠️ Partial (file perms + docs active; auth/encryption not yet enforced)

---

### 3. Detect

**Security Monitoring**:
- Audit logging
- Secret detection
- Permission denial tracking

**Anomaly Detection**:
- Failed authentication monitoring
- Unusual registry access patterns
- Resource usage monitoring

**Compliance**: ✅ Full

---

### 4. Respond

**Incident Response**:
- Detection procedures
- Containment steps
- Eradication process
- Recovery procedures

**Documentation**:
- [../secret-management.md](../secret-management.md#incident-response)

**Compliance**: ✅ Full

---

### 5. Recover

**Recovery Planning**:
- Backup strategies
- Restore procedures
- Lessons learned process

**Documentation**:
- [../secret-management.md](../secret-management.md#incident-response)
- [../../scaling.md](../../scaling.md#backup-strategies)

**Compliance**: ✅ Full

---

## GDPR Compliance

### Lawfulness, Fairness, Transparency

**Requirements**:
- Clear privacy policy
- Explicit consent
- Transparent data usage

**ContextHelp Implementation**:
- Local-first: data stays on user's machine
- No auto-telemetry
- Explicit opt-in for external features

**Compliance**: ✅ Full

---

### Purpose Limitation

**Requirements**:
- Collect data only for specified purposes
- No secondary use without consent

**ContextHelp Implementation**:
- User controls all data collection
- No hidden data transmission
- Plugin capabilities explicitly declared

**Compliance**: ✅ Full

---

### Data Minimization

**Requirements**:
- Collect only necessary data
- Limit retention periods

**ContextHelp Implementation**:
- Local-first: minimal external data sharing
- User controls data retention
- Configurable log rotation

**Compliance**: ✅ Full

---

### Accuracy

**Requirements**:
- Ensure data accuracy
- Allow corrections

**ContextHelp Implementation**:
- User owns and controls all data
- Full edit/delete capabilities
- Version control for changes

**Compliance**: ✅ Full

---

### Storage Limitation

**Requirements**:
- Retain data only as long as necessary

**ContextHelp Implementation**:
- User controls retention
- Configurable cleanup policies
- Manual deletion support

**Compliance**: ✅ Full

---

### Integrity and Confidentiality

**Requirements**:
- Protect data from unauthorized access
- Ensure data integrity

**ContextHelp Implementation**:
- Encryption at rest ⚠️ planned (ADR-019); operator must enable when handling PII
- TLS for external connections ⚠️ operator-configured; not enforced by server
- Access controls ⚠️ planned (ADR-023)
- Audit logging ✅

**Compliance**: ⚠️ Partial (audit log active; encryption/access controls not yet enforced)

---

### Accountability

**Requirements**:
- Demonstrate compliance
- Document processing activities

**ContextHelp Implementation**:
- Comprehensive documentation
- Audit logging
- Privacy by design

**Compliance**: ✅ Full

---

## SOC 2

### CC1: Control Environment

**Requirements**:
- Commitment to integrity and ethical values
- Oversight responsibility
- Organizational structure

**ContextHelp Implementation**:
- Security-first design
- Open source transparency
- Clear documentation

**Compliance**: ✅ Full

---

### CC2: Communication and Information

**Requirements**:
- Relevant information identified and communicated
- Internal and external communication

**ContextHelp Implementation**:
- Comprehensive security documentation
- Audit logging
- Status reporting

**Compliance**: ✅ Full

---

### CC3: Risk Assessment

**Requirements**:
- Identify and analyze risks
- Assess fraud risks

**ContextHelp Implementation**:
- Threat modeling
- Risk matrix
- Regular security reviews

**Compliance**: ✅ Full

---

### CC4: Monitoring Activities

**Requirements**:
- Ongoing and separate evaluations
- Remediation of deficiencies

**ContextHelp Implementation**:
- Continuous security monitoring
- Incident response procedures
- Regular updates

**Compliance**: ✅ Full

---

### CC5: Control Activities

**Requirements**:
- Selection and development of control activities
- Technology controls

**ContextHelp Implementation**:
- Multi-layer security controls
- Defense in depth
- Automated enforcement

**Compliance**: ✅ Full

---

### CC6: Logical and Physical Access

**Requirements**:
- Restrict access to authorized users
- Protect assets from unauthorized access

**ContextHelp Implementation**:
- File permission validation ✅
- Authentication/authorization ⚠️ planned (ADR-023)
- Plugin sandboxing ⚠️ planned (ADR-027)

**Compliance**: ⚠️ Partial (file perms active; auth/sandboxing not yet enforced)

---

### CC7: System Operations

**Requirements**:
- Detect and respond to system events
- Manage changes

**ContextHelp Implementation**:
- Audit logging
- Change management
- Configuration validation

**Compliance**: ✅ Full

---

### CC8: Change Management

**Requirements**:
- Identify and authorize changes
- Design and development controls

**ContextHelp Implementation**:
- Version control
- Security reviews
- Testing requirements

**Compliance**: ✅ Full

---

### CC9: Risk Mitigation

**Requirements**:
- Identify and manage risks
- Implement mitigating controls

**ContextHelp Implementation**:
- Threat model with mitigations
- Security controls
- Continuous improvement

**Compliance**: ✅ Full

---

## ISO 27001

### A.5: Information Security Policies

**ContextHelp Implementation**:
- Security model documented
- Secure defaults
- Best practices guides

**Compliance**: ✅ Full

---

### A.6: Organization of Information Security

**ContextHelp Implementation**:
- Security roles defined
- Responsibilities documented
- Contact procedures

**Compliance**: ✅ Full

---

### A.8: Asset Management

**ContextHelp Implementation**:
- Asset inventory (threat model)
- Classification (risk levels)
- Handling procedures

**Compliance**: ✅ Full

---

### A.9: Access Control

**ContextHelp Implementation**:
- Access control policy ✅ (documented)
- User authentication ⚠️ planned (ADR-023)
- Authorization checks ⚠️ planned (ADR-023)

**Compliance**: ⚠️ Partial (policy documented; runtime enforcement not yet active)

---

### A.10: Cryptography

**ContextHelp Implementation**:
- Encryption at rest (AES-256-GCM) ⚠️ planned (ADR-019)
- TLS 1.2+ in transit ⚠️ operator-configured; not enforced by server
- Key management ⚠️ planned (ADR-019)

**Compliance**: ⚠️ Partial (design documented; not yet active at runtime)

---

### A.12: Operations Security

**ContextHelp Implementation**:
- Secure operations procedures
- Malware protection (sandboxing)
- Logging and monitoring

**Compliance**: ✅ Full

---

### A.14: System Acquisition, Development and Maintenance

**ContextHelp Implementation**:
- Security requirements
- Secure development lifecycle
- Security testing

**Compliance**: ✅ Full

---

### A.16: Information Security Incident Management

**ContextHelp Implementation**:
- Incident response procedures
- Detection mechanisms
- Lessons learned

**Compliance**: ✅ Full

---

### A.18: Compliance

**ContextHelp Implementation**:
- Compliance documentation (this file)
- Regular reviews
- Security audits

**Compliance**: ✅ Full

---

## Compliance Summary

> Pre-alpha. Controls marked ⚠️ are designed (ADRs exist) but not yet runtime-active.
> Re-evaluate at GA once ADR-019, ADR-023, ADR-027 are fully implemented.

| Standard | Compliance Level | Notes |
|----------|-----------------|-------|
| **OWASP Top 10** | ⚠️ Partial (6/10 full) | A01/A02/A07 partial; auth/encryption not enforced |
| **NIST CSF** | ⚠️ Partial | Identify/Detect/Respond ✅; Protect partial |
| **GDPR** | ⚠️ Partial | Local-first ✅; encryption/access controls pending |
| **SOC 2** | ⚠️ Partial | Logging ✅; CC6 auth/encryption not enforced |
| **ISO 27001** | ⚠️ Partial | A.9 access control and A.10 crypto pending |

---

## Audit Trail

### Internal Audits

**Frequency**: Quarterly

**Scope**:
- Security controls review
- Compliance verification
- Threat model updates
- Documentation updates

---

### External Audits

**Recommended Frequency**: Annually

**Scope**:
- Penetration testing
- Code security review
- Compliance certification
- Third-party validation

---

## Related Documentation

- [threat.md](threat.md) — Threat model
- [boundaries.md](boundaries.md) — Trust boundaries
- [controls.md](controls.md) — Security controls
- [../security-model.md](../security-model.md) — Comprehensive security model

---

**Last Updated**: 2026-01-26
**Version**: 1.0
**Status**: Active
**Maintained by**: Security Team
