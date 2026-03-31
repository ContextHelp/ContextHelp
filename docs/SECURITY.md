# Security Policy

## Overview

ContextHelp is a local-first context engine designed with security and privacy as core principles. This document outlines security practices, vulnerability reporting procedures, and guidance for secure deployment.

**Security Principles:**
- 🔒 **Local-first**: Data sovereignty by design
- 🔐 **Encryption**: Data encrypted at rest and in transit (when implemented)
- 🛡️ **Privacy-preserving**: No telemetry by default
- 🔑 **Secure by default**: Conservative security defaults
- 📝 **Transparent**: Open-source security model

---

## 🚨 Reporting Security Vulnerabilities

### How to Report

**DO NOT** open public GitHub issues for security vulnerabilities.

Instead, report vulnerabilities privately via:

1. **Email**: security@context.help
2. **GitHub Security Advisory**: https://github.com/ideacrafterslabs/ctxt/security/advisories/new

### What to Include

Please provide:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if available)
- Your contact information for follow-up

### Response Timeline

- **Initial Response**: Within 48 hours
- **Triage**: Within 5 business days
- **Fix Timeline**: Depends on severity (see below)
- **Public Disclosure**: Coordinated after fix is released

### Severity Levels

| Severity | Description | Fix Timeline |
|----------|-------------|--------------|
| **Critical** | Remote code execution, authentication bypass, secret exposure | 1-3 days |
| **High** | Privilege escalation, data leakage, DoS | 7 days |
| **Medium** | Information disclosure, limited DoS | 30 days |
| **Low** | Minor security improvements | Next release |

---

## 🔐 Secrets Management

### Overview

ContextHelp requires various secrets (API keys, tokens, passwords) to function. **Never commit secrets to version control.**

### Supported Secret Storage Methods

#### 1. Environment Variables (Recommended for Development)

```bash
# Set environment variables
export OPENAI_API_KEY="sk-proj-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export CH_REGISTRY_TOKEN_UXPATTERNS="token_..."

# Run ContextHelp
ctxt analyze "content"
```

**Advantages:**
- ✅ Simple to use
- ✅ Supported across all platforms
- ✅ Works with container orchestration

**Disadvantages:**
- ⚠️ Visible in process listings (`ps aux`)
- ⚠️ Inherited by child processes
- ⚠️ No encryption at rest

#### 2. Configuration Files with Environment Variable References

**config.yaml:**
```yaml
ai_providers:
  openai:
    key: ${OPENAI_API_KEY}  # References environment variable
```

**Advantages:**
- ✅ Separation of config structure from secrets
- ✅ Config files can be version controlled
- ✅ Clear audit trail of what secrets are needed

**Best Practice:**
- Always use `${VAR_NAME}` syntax
- Never put plaintext secrets in config files
- Add `config.yaml` to `.gitignore` if it contains plaintext secrets

#### 3. .env Files (Recommended for Local Development)

**Project root .env file:**
```bash
# .env - NEVER commit this file
OPENAI_API_KEY=sk-proj-...
ANTHROPIC_API_KEY=sk-ant-...
POSTGRES_PASSWORD=securepassword
```

**Setup:**
```bash
# Copy example file
cp .env.example .env

# Edit with your secrets
nano .env

# Verify it's in .gitignore
git check-ignore .env  # Should output: .env
```

**Advantages:**
- ✅ Centralized secret management
- ✅ Easy to rotate secrets
- ✅ Automatically loaded by many tools

**Important:** `.env` files are in `.gitignore` - verify before committing!

#### 4. OS Credential Stores (Recommended for Production)

##### macOS Keychain

```bash
# Store secret in macOS Keychain
security add-generic-password \
  -a contexthelp \
  -s openai_key \
  -w "sk-proj-..."

# Reference in config
export OPENAI_API_KEY=$(security find-generic-password \
  -a contexthelp -s openai_key -w)
```

##### Linux Secret Service

```bash
# Using secret-tool (GNOME Keyring)
secret-tool store --label="OpenAI API Key" \
  application contexthelp \
  key openai

# Retrieve
export OPENAI_API_KEY=$(secret-tool lookup \
  application contexthelp key openai)
```

##### Windows Credential Manager

```powershell
# Store credential
cmdkey /generic:contexthelp_openai /user:apikey /pass:sk-proj-...

# Retrieve (using PowerShell)
$cred = Get-StoredCredential -Target contexthelp_openai
$env:OPENAI_API_KEY = $cred.GetNetworkCredential().Password
```

**Advantages:**
- ✅ Encrypted at rest by OS
- ✅ Integrated with system security
- ✅ Supports biometric authentication
- ✅ Secrets not visible in process listings

#### 5. Password Managers (Recommended)

##### 1Password

```bash
# Using 1Password CLI
export OPENAI_API_KEY=$(op item get "OpenAI API Key" --fields label=credential)
```

##### pass (Unix Password Manager)

```bash
# Store secret
pass insert contexthelp/openai_key

# Use in config
export OPENAI_API_KEY=$(pass show contexthelp/openai_key)
```

##### Bitwarden CLI

```bash
# Unlock vault
bw unlock

# Get secret
export OPENAI_API_KEY=$(bw get password openai_key)
```

**Advantages:**
- ✅ Centralized secret management
- ✅ Encrypted vault
- ✅ Audit trail
- ✅ Team sharing (for team password managers)

#### 6. HashiCorp Vault (Enterprise/Production)

```bash
# Authenticate
vault login -method=userpass username=myuser

# Store secret
vault kv put secret/contexthelp/openai key=sk-proj-...

# Retrieve
export OPENAI_API_KEY=$(vault kv get -field=key secret/contexthelp/openai)
```

**Advantages:**
- ✅ Enterprise-grade secret management
- ✅ Dynamic secrets with automatic rotation
- ✅ Detailed audit logging
- ✅ Fine-grained access control
- ✅ High availability

---

## 🔑 API Key Management

### OpenAI API Keys

**Format:** `sk-proj-...` (project keys) or `sk-...` (legacy keys)

**Best Practices:**
1. Use project-scoped keys (limits blast radius)
2. Set usage limits in OpenAI dashboard
3. Monitor usage for anomalies
4. Rotate keys quarterly or after suspected compromise

**Validation:**
```bash
# Test key validity
curl https://api.openai.com/v1/models \
  -H "Authorization: Bearer $OPENAI_API_KEY" | jq '.data[0].id'
```

**Revocation:**
- Go to https://platform.openai.com/api-keys
- Click "Revoke" on compromised key
- Generate new key
- Update ContextHelp configuration

### Anthropic API Keys

**Format:** `sk-ant-...`

**Best Practices:**
1. Use separate keys for development and production
2. Set rate limits in Anthropic console
3. Monitor billing alerts
4. Rotate every 90 days

**Validation:**
```bash
# Test key validity
curl https://api.anthropic.com/v1/messages \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{"model":"claude-3-opus-20240229","max_tokens":1,"messages":[{"role":"user","content":"test"}]}'
```

### Registry Authentication Tokens

**Format:** Varies by registry (often JWT or opaque tokens)

**Best Practices:**
1. Use read-only tokens when possible
2. Scope tokens to specific registries
3. Set expiration dates
4. Use separate tokens per environment

**Configuration:**
```yaml
registries:
  enabled:
    - name: uxpatterns
      url: https://api.uxpatterns.io
      auth:
        token: ${CH_REGISTRY_TOKEN_UXPATTERNS}
```

**Environment variable:**
```bash
export CH_REGISTRY_TOKEN_UXPATTERNS="eyJhbGc..."
```

---

## 🔒 Encryption

### Current Status

**⚠️ IMPORTANT:** As of the current release, encryption features are **documented but not yet fully implemented**. See [ADR-019](docs/decisions/ADR-019-encryption-and-privacy.md) for the complete encryption architecture.

### Planned Encryption Features

#### 1. Storage Encryption (Coming Soon — ADR-019)

**SQLite with SQLCipher (planned config):**
```yaml
storage:
  type: sqlite
  path: ~/.local/share/contexthelp/data.db
  encryption:
    enabled: true
    provider: sqlcipher
    key_derivation: argon2id
```

**Until ADR-019 ships, use OS-level disk encryption:**
- macOS: enable FileVault (`System Settings → Privacy & Security → FileVault`)
- Linux: LUKS full-disk or ecryptfs home directory
- Windows: BitLocker

Also restrict file permissions:
```bash
chmod 600 ~/.local/share/contexthelp/data.db
chmod 700 ~/.local/share/contexthelp/
```

#### 2. Object-Level Encryption (Planned — ADR-019)

Encrypt individual knowledge objects (not yet implemented):
```yaml
# Planned config — not active in current release
storage:
  encryption:
    enabled: true
    object_level: true
```

#### 3. End-to-End Encryption for Registry Sync (Planned)

```yaml
registries:
  enabled:
    - name: private-registry
      url: https://registry.example.com
      encryption:
        enabled: true
        public_key: ~/.config/ctxt/keys/registry.pub
```

### Until Encryption is Implemented

**Current Mitigations:**
1. Use encrypted filesystems (FileVault, LUKS, BitLocker)
2. Restrict file permissions: `chmod 600 ~/.config/contexthelp/config.yaml`
3. Use encrypted backups
4. Consider full-disk encryption

**Check whether encryption is active:**
```bash
ctxt version --output json | grep -i encrypt   # will show nothing until ADR-019 ships
```

---

## 🛡️ Database Security

### SQLite (Default Backend)

**File Permissions:**
```bash
# Restrict database file access
chmod 600 ~/.local/share/contexthelp/data.db

# Verify permissions
ls -l ~/.local/share/contexthelp/data.db
# Should show: -rw------- (owner read/write only)
```

**Configuration:**
```yaml
storage:
  type: sqlite
  path: ~/.local/share/contexthelp/data.db
  concurrency: safe
  options:
    journal_mode: WAL
    synchronous: NORMAL
    foreign_keys: ON
```

### PostgreSQL

**Connection Security:**

**DO NOT** embed passwords in connection strings:
```yaml
# ❌ INSECURE - Password visible in config
storage:
  path: "postgresql://user:password@localhost/contexthelp"
```

**✅ Use environment variables:**
```yaml
storage:
  type: postgres
  path: "postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}/${POSTGRES_DB}"
```

**✅ Better: Use .pgpass file:**
```bash
# Create ~/.pgpass
echo "localhost:5432:contexthelp:ctxt_user:secure_password" > ~/.pgpass
chmod 600 ~/.pgpass

# Connection string without password
postgresql://ctxt_user@localhost/contexthelp
```

**✅ Best: Use pg_service.conf:**
```ini
# ~/.pg_service.conf
[contexthelp]
host=localhost
port=5432
dbname=contexthelp
user=ctxt_user
# Password read from .pgpass
```

```yaml
# config.yaml
storage:
  path: "service=contexthelp"
```

**SSL/TLS Configuration:**
```yaml
storage:
  type: postgres
  path: "postgresql://user@host/db?sslmode=require"
  options:
    sslmode: require  # or verify-ca, verify-full
    sslcert: /path/to/client.crt
    sslkey: /path/to/client.key
    sslrootcert: /path/to/ca.crt
```

---

## 🔐 Authentication & Authorization

### Current Status

**⚠️ IMPORTANT:** Authentication system is **designed but not yet implemented**.
See [ADR-023](docs/decisions/ADR-023-authentication-authorization-model.md) for
specifications. The server currently accepts unauthenticated requests to localhost.

**Operator guidance until ADR-023 ships:**
- Keep `dpkms serve` on `127.0.0.1` (default; do not use `--public` without a
  TLS-terminating reverse proxy and network ACLs).
- If exposing the port on a shared machine, use firewall rules to restrict access
  to trusted UIDs/IPs.

### Planned Features (ADR-023)

#### 1. Personal Access Tokens (PATs)

```yaml
# Planned config — not active in current release
security:
  authentication:
    enabled: true
    jwt:
      secret: ${JWT_SECRET}
      expiration: 24h
```

#### 2. Registry Authentication (active)

Registry `login` / `logout` is shipped:
```bash
ctxt registry login <name>    # prompts for token; stores in secrets backend
ctxt registry logout <name>
```

#### 3. Plugin Capabilities (Planned — ADR-027)

```yaml
# Planned config — not active in current release
plugins:
  load:
    - name: github-plugin
      capabilities:
        - network.github.com  # Restricted to GitHub API only
        - storage.read        # Read-only storage access
```

---

## 🚀 Deployment Security

### Development Environment

```bash
# Use separate config for development
export CTXT_CONFIG=./config.dev.yaml

# Use separate data directory
export CTXT_DATA_DIR=./dev-data

# Enable debug logging
export CH_LOG_LEVEL=debug
export CH_DEBUG=true
```

### Production Environment

**Checklist:**
- [ ] Use encrypted storage backend (SQLCipher or encrypted PostgreSQL)
- [ ] Enable TLS for all network communication
- [ ] Use production-grade secret management (Vault, AWS Secrets Manager)
- [ ] Set restrictive file permissions (600 for config, 700 for data dirs)
- [ ] Disable telemetry if required: `CH_DISABLE_TELEMETRY=true`
- [ ] Enable audit logging
- [ ] Configure automated backups (encrypted)
- [ ] Set up monitoring and alerting
- [ ] Use read-only filesystem where possible
- [ ] Run with least-privilege user account
- [ ] Enable rate limiting for API endpoints
- [ ] Configure CORS properly for web interfaces

**Example production config:**
```yaml
storage:
  type: postgres
  path: "service=contexthelp_prod"
  encryption:
    enabled: true

server:
  http:
    port: 7700
    public: false  # Only localhost
    tls:
      enabled: true
      cert: /etc/ctxt/tls/cert.pem
      key: /etc/ctxt/tls/key.pem

security:
  telemetry: false
  allow_external_pipelines: false
  rate_limiting:
    enabled: true
    requests_per_minute: 60

audit:
  enabled: true
  log_file: /var/log/ctxt/audit.log
```

### Docker/Container Deployment

**Dockerfile security:**
```dockerfile
FROM golang:1.21-alpine AS builder
# ... build steps ...

FROM alpine:3.19
RUN apk add --no-cache ca-certificates

# Run as non-root user
RUN addgroup -g 1000 ctxt && \
    adduser -D -u 1000 -G ctxt ctxt

USER ctxt
WORKDIR /home/ctxt

COPY --from=builder /app/bin/ctxt /usr/local/bin/
COPY --from=builder /app/bin/dpkms /usr/local/bin/

# Don't include secrets in image
VOLUME ["/home/ctxt/.config/contexthelp"]

ENTRYPOINT ["dpkms"]
CMD ["serve"]
```

**Docker Compose secrets:**
```yaml
version: '3.8'
services:
  dpkms:
    image: contexthelp/dpkms:latest
    secrets:
      - openai_key
      - postgres_password
    environment:
      OPENAI_API_KEY_FILE: /run/secrets/openai_key
      POSTGRES_PASSWORD_FILE: /run/secrets/postgres_password

secrets:
  openai_key:
    file: ./secrets/openai_key.txt
  postgres_password:
    file: ./secrets/postgres_password.txt
```

### Kubernetes Deployment

**Using Kubernetes Secrets:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: contexthelp-secrets
type: Opaque
stringData:
  openai-api-key: "sk-proj-..."
  anthropic-api-key: "sk-ant-..."
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dpkms
spec:
  template:
    spec:
      containers:
      - name: dpkms
        image: contexthelp/dpkms:latest
        env:
        - name: OPENAI_API_KEY
          valueFrom:
            secretKeyRef:
              name: contexthelp-secrets
              key: openai-api-key
        - name: ANTHROPIC_API_KEY
          valueFrom:
            secretKeyRef:
              name: contexthelp-secrets
              key: anthropic-api-key
        securityContext:
          runAsNonRoot: true
          runAsUser: 1000
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
```

**Using External Secrets Operator:**
```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: contexthelp-secrets
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: vault-backend
    kind: SecretStore
  target:
    name: contexthelp-secrets
  data:
  - secretKey: openai-api-key
    remoteRef:
      key: secret/data/contexthelp
      property: openai_key
```

---

## 🔍 Security Auditing

### Logging

**What is logged:**
- Authentication attempts (success/failure)
- API key usage (sanitized)
- Configuration changes
- Database operations
- Plugin installations

**What is NOT logged:**
- API keys (sanitized as `sk-***`)
- Passwords (sanitized as `***`)
- Full connection strings (sanitized)
- User content (unless explicitly enabled)

**Audit log location:**
```bash
# Default location
~/.local/share/contexthelp/audit.log

# Custom location
export CH_AUDIT_LOG=/var/log/ctxt/audit.log
```

**Review audit logs:**
```bash
# View recent entries
ctxt audit logs --tail 100

# Filter by event type
ctxt audit logs --type auth --since 24h

# Export for analysis
ctxt audit export --format json --output audit-2024-01.json
```

### Secret Scanning

**Pre-commit hooks:**
```bash
# Install gitleaks
brew install gitleaks

# Add pre-commit hook
cat > .git/hooks/pre-commit << 'EOF'
#!/bin/bash
gitleaks detect --no-git --verbose --redact
EOF
chmod +x .git/hooks/pre-commit
```

**Scan repository history:**
```bash
# Using gitleaks
gitleaks detect --verbose --redact --report-path gitleaks-report.json

# Using trufflehog
trufflehog git file://. --json > trufflehog-report.json
```

**Configuration validation:**
```bash
# Check for plaintext secrets in config
ctxt config validate --check-secrets

# Scan all config files
ctxt config lint
```

---

## 🔄 Secret Rotation

### When to Rotate Secrets

**Immediate rotation required:**
- Secret compromised or exposed
- Employee with access leaves
- Service provider reports breach
- Secret found in logs or version control
- Suspicious API usage detected

**Scheduled rotation:**
- API keys: Every 90 days
- Database passwords: Every 180 days
- Encryption keys: Annually
- Registry tokens: Every 90 days

### Rotation Procedures

#### OpenAI API Key Rotation

```bash
# 1. Generate new key in OpenAI dashboard
# 2. Test new key
export OPENAI_API_KEY_NEW="sk-proj-new..."
curl https://api.openai.com/v1/models \
  -H "Authorization: Bearer $OPENAI_API_KEY_NEW"

# 3. Update configuration
ctxt config set ai_providers.openai.key "${OPENAI_API_KEY_NEW}"

# 4. Restart services
systemctl restart dpkms

# 5. Verify services using new key
ctxt analyze "test" --type text

# 6. Revoke old key in OpenAI dashboard
```

#### Database Password Rotation

```bash
# 1. Connect as admin
psql -U postgres

# 2. Set new password
ALTER USER ctxt_user WITH PASSWORD 'new_secure_password';

# 3. Update .pgpass
echo "localhost:5432:contexthelp:ctxt_user:new_secure_password" > ~/.pgpass
chmod 600 ~/.pgpass

# 4. Test connection
psql -U ctxt_user -d contexthelp -c "SELECT 1;"

# 5. Restart services
systemctl restart dpkms
```

#### Registry Token Rotation

```bash
# 1. Generate new token in registry dashboard
# 2. Update environment variable
export CH_REGISTRY_TOKEN_UXPATTERNS="new_token_..."

# 3. Test registry access
ctxt registry sync uxpatterns

# 4. Persist to environment
echo "export CH_REGISTRY_TOKEN_UXPATTERNS='new_token_...'" >> ~/.bashrc

# 5. Revoke old token
```

---

## 🚨 Incident Response

> Full runbook (detection signals, immediate/follow-up actions, post-incident checklists):
> **[docs/operations/incident-response.md](operations/incident-response.md)**

### If You Suspect a Secret Has Been Compromised

**Immediate Actions (within 1 hour):**

1. **Revoke the compromised secret**
   - OpenAI: https://platform.openai.com/api-keys
   - Anthropic: https://console.anthropic.com/settings/keys
   - Registry: Contact registry administrator

2. **Rotate to new secret**
   - Generate new key/token
   - Update configuration
   - Restart services

3. **Assess exposure**
   - Check git history: `git log -p -S "sk-proj-" --all`
   - Check logs: `grep -r "sk-proj-" /var/log/ctxt/`
   - Review API usage for anomalies

4. **Document incident**
   - When was secret exposed?
   - How was it exposed?
   - What systems had access?
   - What actions were taken?

**Follow-up Actions (within 24 hours):**

5. **Investigate impact**
   - Review API usage logs
   - Check for unauthorized access
   - Assess data exposure
   - Determine cost impact

6. **Implement additional controls**
   - Add monitoring alerts
   - Tighten access controls
   - Update rotation schedules
   - Review security policies

7. **Report if required**
   - Notify affected users (if applicable)
   - Report to security team
   - File incident report

### Secret Found in Git History

```bash
# 1. Remove from current repository
git filter-repo --invert-paths --path config.yaml

# 2. Force push (CAUTION: coordinate with team)
git push --force --all
git push --force --tags

# 3. Revoke exposed secret immediately

# 4. Notify team members to re-clone
# 5. File incident report
```

---

## 📋 Security Checklist

### Before First Run

- [ ] Review `.env.example` and create `.env` with your secrets
- [ ] Verify `.env` is in `.gitignore`
- [ ] Set file permissions: `chmod 600 .env`
- [ ] Use `${VAR_NAME}` syntax in `config.yaml`
- [ ] Test configuration: `ctxt config validate`
- [ ] Enable encryption (when available)

### Development Workflow

- [ ] Never commit secrets to version control
- [ ] Use separate API keys for dev/staging/prod
- [ ] Install pre-commit hooks for secret scanning
- [ ] Review diffs before committing: `git diff --cached`
- [ ] Use read-only API keys when possible
- [ ] Rotate development keys quarterly

### Production Deployment

- [ ] Use production-grade secret management (Vault, AWS Secrets Manager)
- [ ] Enable encryption at rest
- [ ] Configure TLS for all network communication
- [ ] Set restrictive file permissions (600/700)
- [ ] Disable telemetry: `CH_DISABLE_TELEMETRY=true`
- [ ] Enable audit logging
- [ ] Configure automated backups (encrypted)
- [ ] Set up monitoring and alerting
- [ ] Run as non-root user
- [ ] Use read-only filesystem where possible
- [ ] Configure rate limiting
- [ ] Review security logs weekly

### Regular Maintenance

- [ ] Rotate API keys quarterly
- [ ] Review access logs monthly
- [ ] Update dependencies regularly
- [ ] Scan for vulnerabilities: `ctxt security scan`
- [ ] Test backup restoration
- [ ] Review and update security policies
- [ ] Conduct security training for team

---

## 🔗 Additional Resources

### Documentation

- [Environment Variables Reference](docs/environment-variables.md)
- [Configuration Guide](docs/ctxt/configuration.md)
- [CLI Flag Binding](docs/ctxt/cli-flag-binding.md)
- [ADR-019: Encryption Architecture](docs/decisions/ADR-019-encryption-and-privacy.md)
- [ADR-023: Authentication Model](docs/decisions/ADR-023-authentication-authorization-model.md)
- [Security Design](docs/dpkms/security.md)
- [Compliance — GDPR, SOC 2, erasure, dependency assessment](docs/operations/compliance.md)

### External Resources

- [OWASP Secrets Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html)
- [NIST Cryptographic Key Management](https://csrc.nist.gov/publications/detail/sp/800-57-part-1/rev-5/final)
- [CIS Benchmarks](https://www.cisecurity.org/cis-benchmarks/)
- [SQLCipher Documentation](https://www.zetetic.net/sqlcipher/documentation/)
- [HashiCorp Vault Best Practices](https://learn.hashicorp.com/tutorials/vault/pattern-centralized-secrets)

### Tools

- **Secret Scanning:**
  - [gitleaks](https://github.com/gitleaks/gitleaks)
  - [trufflehog](https://github.com/trufflesecurity/trufflehog)
  - [git-secrets](https://github.com/awslabs/git-secrets)

- **Secret Management:**
  - [HashiCorp Vault](https://www.vaultproject.io/)
  - [Mozilla SOPS](https://github.com/mozilla/sops)
  - [pass](https://www.passwordstore.org/)
  - [1Password CLI](https://developer.1password.com/docs/cli)

- **Security Auditing:**
  - [gosec](https://github.com/securego/gosec) - Go security checker
  - [nancy](https://github.com/sonatype-nexus-community/nancy) - Dependency vulnerability scanner
  - [trivy](https://github.com/aquasecurity/trivy) - Container security scanner

---

## 📞 Contact

- **Security Issues**: security@context.help
- **General Support**: support@context.help
- **GitHub Security**: https://github.com/ideacrafterslabs/ctxt/security/advisories

---

## 📄 License

This security policy is part of the ContextHelp project and is licensed under AGPL-3.0.

**Last Updated:** 2024-01-26
**Version:** 1.0.0
