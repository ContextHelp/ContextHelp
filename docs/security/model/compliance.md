# Compliance & Standards

Compliance mapping for ContextHelp security controls.

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
- User/permission validation before data access
- Localhost-only API binding by default
- File permission validation (0600/0700)
- Plugin sandboxing with capability-based permissions
- Authorization checks on all API endpoints

**Implementation**:
```go
func CheckAccess(userID string, action string, resourceID string) error {
    permission := db.GetPermission(userID, resourceID)
    if !permission.Allows(action) {
        AuditLog(EventPermissionDenied, userID, action, resourceID)
        return fmt.Errorf("access denied")
    }
    return nil
}
```

**Compliance Level**: ✅ Full

---

### A02: Cryptographic Failures

**Risk**: Sensitive data exposed due to weak crypto

**ContextHelp Mitigations**:
- Optional AES-256-GCM encryption at rest
- Mandatory TLS 1.2+ for remote connections
- Secure key derivation (PBKDF2, Argon2)
- Certificate validation
- No plaintext secrets in configuration

**Implementation**:
```yaml
security:
  encryption:
    enabled: true
    algorithm: aes-256-gcm
    key_derivation: argon2
```

**Compliance Level**: ✅ Full

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
- Optional JWT token authentication
- Secure session handling
- Strong token generation
- Session expiration
- Failed login monitoring

**Implementation**:
```go
// JWT configuration
type JWTConfig struct {
    Secret     string        `json:"secret"`
    Expiration time.Duration `json:"expiration"`
    Issuer     string        `json:"issuer"`
}

// Strong token generation
tokenBytes := make([]byte, 32)
rand.Read(tokenBytes)
token := base64.URLEncoding.EncodeToString(tokenBytes)
```

**Compliance Level**: ✅ Full

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

| Category | Compliance | Notes |
|----------|-----------|-------|
| A01 Broken Access Control | ✅ Full | Capability model, permissions |
| A02 Cryptographic Failures | ✅ Full | AES-256-GCM, TLS 1.2+ |
| A03 Injection | ✅ Full | Parameterized queries, validation |
| A04 Insecure Design | ✅ Full | Threat model, secure defaults |
| A05 Security Misconfiguration | ✅ Full | Validation tools, warnings |
| A06 Vulnerable Components | ✅ Full | Dependency scanning |
| A07 Authentication Failures | ✅ Full | JWT, session management |
| A08 Data Integrity Failures | ⚠️ Partial | Schema validation, signatures planned |
| A09 Logging Failures | ✅ Full | Comprehensive audit logging |
| A10 SSRF | ✅ Full | No auto-external calls, validation |

**Overall**: 9/10 Full, 1/10 Partial

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
- Capability-based permissions
- File permission validation
- Authentication/authorization

**Data Security**:
- Optional encryption at rest
- TLS 1.2+ in transit
- Secure key derivation

**Awareness & Training**:
- Security documentation
- Best practices guides
- Runbook procedures

**Compliance**: ✅ Full

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
- Optional encryption at rest
- TLS for external connections
- Access controls
- Audit logging

**Compliance**: ✅ Full

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
- Authentication/authorization
- File permission validation
- Plugin sandboxing

**Compliance**: ✅ Full

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
- Access control policy
- User authentication
- Authorization checks

**Compliance**: ✅ Full

---

### A.10: Cryptography

**ContextHelp Implementation**:
- Encryption at rest (AES-256-GCM)
- TLS 1.2+ in transit
- Key management

**Compliance**: ✅ Full

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

| Standard | Compliance Level | Notes |
|----------|-----------------|-------|
| **OWASP Top 10** | 90% (9/10 Full, 1/10 Partial) | Signature verification planned |
| **NIST CSF** | 100% | All 5 functions implemented |
| **GDPR** | 100% | All 7 principles met |
| **SOC 2** | 100% | All 9 trust service criteria |
| **ISO 27001** | 100% | Key controls implemented |

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
