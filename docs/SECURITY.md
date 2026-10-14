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

# Run ContextHelp
ctxt analyze "content"
```

Registry tokens are not environment variables: `ctxt registry login <name>`
stores them in the OS keychain (see [Registry Tokens](#registry-tokens)).

**Advantages:**
- ✅ Simple to use
- ✅ Supported across all platforms
- ✅ Works with container orchestration

**Disadvantages:**
- ⚠️ Visible in process listings (`ps aux`)
- ⚠️ Inherited by child processes
- ⚠️ No encryption at rest

#### 2. Config Files: No `${VAR}`, Secrets in Their Own File

Config files (`ctxt.yaml`, `dpkms.yaml`) take every value literally:
`${VAR_NAME}` is not expanded, so `token: ${MY_TOKEN}` sets the token to
the string `${MY_TOKEN}`. Keep secrets out of the shared config:

- Provider keys (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`,
  `OPENROUTER_API_KEY`) resolve from the secrets store under that name,
  then from the environment variable of the same name; the store wins.
- The embedding provider's `providers.embedding.api_key_env` names the
  variable holding its key, never the key itself.
- S3 blob credentials: `CTXT_BLOB_S3_ACCESS_KEY` /
  `CTXT_BLOB_S3_SECRET_KEY` override `storage.blob.s3.access_key` /
  `secret_key`.
- Registry tokens: `ctxt registry login <name>` stores them in the OS
  keychain.
- `secrets.backend` (`env` by default; `keyring`, `agefile`,
  `onepassword`, `ghsecrets`) is the store `dpkms secret set|get|list`
  manage. `auto` provider selection checks it for a key, but the provider
  client itself reads the environment variable, so export the key too.

Secrets that have no environment variable (`server.auth` tokens,
`federation.token`) go into the config literally. Keep them in their own
file, outside version control, and layer it with a second `--config`:

**dpkms-secrets.yaml** (mode 0600, gitignored):
```yaml
server:
  auth:
    provider: static
    static:
      tokens:
        - token: <owner-token>   # openssl rand -hex 32
          principal: owner
          roles: [admin]
```

```bash
dpkms serve --config dpkms.yaml --config dpkms-secrets.yaml
```

**Advantages:**
- ✅ The main config stays committable
- ✅ One file to protect, rotate and back up separately
- ✅ In containers the file mounts as a secret (see [Docker: Secrets](deployment/docker.md#secrets))

**Best Practice:**
- Never put plaintext secrets in a committed config file
- Prefer an env var where the code reads one (`OPENAI_API_KEY`, `CTXT_BLOB_S3_SECRET_KEY`, `PGPASSWORD`)
- Add the secrets file to `.gitignore`
- `ctxt config validate` flags plaintext secrets it finds and rejects `${VAR}` values, which config never expands

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

Neither `ctxt` nor `dpkms` loads `.env`; export it into the shell first:

```bash
set -a; . ./.env; set +a
```

**Advantages:**
- ✅ Centralized secret management
- ✅ Easy to rotate secrets

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

#### Registry Tokens

The config names the registry and its auth mechanism only; the token
never goes in a config file or an environment variable:

```yaml
registries:
  - name: uxpatterns
    url: https://api.uxpatterns.io
    auth:
      type: bearer          # bearer | api_key | oauth2
```

```bash
ctxt registry login uxpatterns    # prompts; stores the token in the OS keychain
ctxt registry logout uxpatterns   # removes it
```

---

## 🔒 Encryption

### Current Status

**⚠️ IMPORTANT:** As of the current release, encryption features are **documented but not yet fully implemented**. See [ADR-019](decisions/ADR-019-encryption-and-privacy.md) for the complete encryption architecture.

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
2. Restrict file permissions: `chmod 600 ~/.config/contexthelp/ctxt.yaml ~/.config/contexthelp/dpkms.yaml`
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
```

### PostgreSQL

**Connection Security:**

**DO NOT** embed passwords in connection strings:
```yaml
# ❌ INSECURE - Password visible in config
storage:
  path: "postgresql://user:<password>@localhost/contexthelp"
```

**✅ Leave the password out of the DSN:** config files do not expand `${VAR}`, so let libpq read `PGPASSWORD` (or the other `PG*` variables):
```yaml
storage:
  type: postgres
  path: "postgresql://ctxt_user@db.internal/contexthelp"
```

```bash
PGPASSWORD=$(vault kv get -field=password secret/db) dpkms serve
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
# dpkms.yaml
storage:
  path: "service=contexthelp"
```

**SSL/TLS Configuration:**
```yaml
storage:
  type: postgres
  # sslmode: require | verify-ca | verify-full
  path: "postgresql://user@host/db?sslmode=verify-full&sslcert=/path/to/client.crt&sslkey=/path/to/client.key&sslrootcert=/path/to/ca.crt"
```

---

## 🔐 Authentication & Authorization

### Access classes

`server.access` decides who can reach a dpkms instance:

| `server.access` | Binds | Inbound auth |
|---|---|---|
| `private` (default) | `127.0.0.1` only | none; every local caller gets every scope |
| `protected` | all interfaces | required; for a trusted network or behind a reverse proxy |
| `public` | all interfaces | required; internet-facing, and anonymous `/healthz` shows only the health verdict |

`dpkms serve --public` (or the deprecated `server.public: true`) is
shorthand for `public`. A protected or public instance without
`server.auth` fails `ctxt config validate`, and `dpkms serve` refuses to
start before binding a port.

### Static tokens and roles

The only implemented provider is `static` (`internal/auth`). `oidc` and
`mtls` are reserved names with no backend behind them. Each token maps to a
principal and one or more roles:

```yaml
server:
  access: protected
  auth:
    provider: static
    static:
      tokens:
        - token: <owner-token>      # openssl rand -hex 32
          principal: owner
          roles: [admin]
        - token: <phone-token>
          principal: phone
          roles: [writer]           # capture-only device
```

- Tokens must be at least 32 characters and must not contain `REPLACE_ME`
  (the Docker example's placeholder). Keep them in a separate 0600 file
  layered with a second `--config` (see
  [Config Files](#2-config-files-no-var-secrets-in-their-own-file)).
- Clients send `Authorization: Bearer <token>` or `X-API-Key: <token>`.
- Roles are fixed scope bundles: `admin` holds every scope, `writer` every
  `read:*` and `write:*` scope, `reader` every `read:*` scope.
- Every `/api/v1` route and gRPC method requires one scope (`read:objects`,
  `write:objects`, `delete:edges`, `read:mcp`, `admin:audit`, ...). A missing
  or invalid token gets `401`; a token without the scope gets
  `403 INSUFFICIENT_SCOPE`. `GET /api/v1/whoami` shows the caller's
  principal, roles and scopes.
- On non-private instances the federation push route also needs the
  `federation.token` credential.

The full scope table is in
[Inbound auth, roles and scopes](manual/reference/config-and-permissions.md#inbound-auth-roles-and-scopes).
On the client side, `server.token` or a `server.urls[].token` entry holds
the token ctxt sends; see [Docker: Secrets](deployment/docker.md#secrets).

### Web UI sessions

`ctxt ui open` mints a single-use sign-in link for the web UI. The browser
session acts as the principal of the token that minted it, but only with
the scopes the web UI needs, and ends after `server.ui.session.idle_ttl`
(default 12h) without a request, `server.ui.session.max_ttl` (default
168h) after sign-in, or as soon as its token is removed from the config.
`dpkms session list` and `dpkms session revoke` manage sessions. See
[Sign In to the Web UI](manual/workflows/web-ui-sign-in.md).

### Transport security

dpkms serves plain HTTP and has no TLS settings. For remote access, put a
protected instance behind a TLS-terminating reverse proxy
([Put dpkms Behind a Reverse Proxy](manual/operations/reverse-proxy.md)),
or keep it private and tunnel (`ssh -L 8080:127.0.0.1:8080 <host>`). Never
proxy a private instance: it authenticates nobody.

### Registry Authentication

```bash
ctxt registry login <name>    # prompts for token; stores it in the OS keychain
ctxt registry logout <name>
```

### Not implemented

OIDC/JWT and mTLS providers, per-user accounts and plugin capability
restrictions ([ADR-027](decisions/ADR-027-plugin-isolation-sandboxing.md))
are designed but not implemented. The auth design is in
[ADR-023](decisions/ADR-023-authentication-authorization-model.md).

---

## 🚀 Deployment Security

### Development Environment

```bash
# Use separate config for development
export CTXT_CONFIG=./config.dev.yaml

# Use separate data directory
export CTXT_DATA_DIR=./dev-data

# Debug logging: no env var, pass --verbose
dpkms serve --verbose
```

### Production Environment

**Checklist:**
- [ ] Keep `server.access: private` unless remote clients need the instance
- [ ] For remote access: `server.access: protected`, static tokens with the
      narrowest role each client needs, `server.allowed_hosts` set
- [ ] Terminate TLS at a reverse proxy; dpkms has no TLS listener
- [ ] Keep tokens and passwords out of committed config (separate 0600 file,
      env vars, `PGPASSWORD`)
- [ ] Set restrictive file permissions (600 for config, 700 for data dirs);
      `ctxt config doctor` checks the ctxt config file
- [ ] Run `ctxt config validate` before deploying a config change
- [ ] Telemetry is off by default (`privacy.telemetry: false`);
      `CH_DISABLE_TELEMETRY=true` forces it off
- [ ] Forward the audit log (`audit.syslog`, `audit.webhook`) and set
      `security.alerts` for auth-failure alerts
- [ ] Use full-disk encryption for the data directory (no built-in
      encryption at rest)
- [ ] Configure automated backups
- [ ] Run with a least-privilege user account

**Example production config:**
```yaml
# dpkms.yaml (committable)
storage:
  type: postgres
  # Password from PGPASSWORD or ~/.pgpass, never in this file
  path: "postgresql://contexthelp@db.internal/contexthelp?sslmode=verify-full"

server:
  access: protected
  allowed_hosts:
    - dpkms.example.com

audit:
  syslog:
    enabled: true
    protocol: tls
    address: logs.internal:6514
```

```yaml
# dpkms-secrets.yaml (mode 0600, gitignored)
server:
  auth:
    provider: static
    static:
      tokens:
        - token: <owner-token>   # openssl rand -hex 32
          principal: owner
          roles: [admin]
```

```bash
dpkms serve --config dpkms.yaml --config dpkms-secrets.yaml
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

**What the audit log records:**
- Security alerts: `security.auth_failure` and `security.acl_denial` once
  the `security.alerts` thresholds are crossed, plus quota exhaustion,
  signing failures and worker crash loops
- Duplicate detection (`dedup.*`) and fan-out enrichment
  (`fanout.completed`)
- `ctxt doctor` runs (`doctor.run`)

Successful sign-ins, configuration changes and plugin installs are not
recorded.

**Process logs are not sanitized.** ctxt and dpkms log to stderr through
`slog` with no redaction layer; see
[What output shows secrets](security/secrets-validation-and-log-sanitization.md#what-output-shows-secrets).

**Audit log location:** the audit log is kept by dpkms, not in a file.
Read it with `ctxt audit list` / `ctxt audit export`; forward it with the
`audit.syslog` and `audit.webhook` config keys.

**Review audit logs:**
```bash
# View recent entries
ctxt audit list --limit 100

# Filter by event type and time window
ctxt audit list --event-type security.auth_failure --since 24h

# Export for analysis
ctxt audit export --format json > audit-2024-01.json
```

### Secret Scanning

**Pre-commit hooks:**
```bash
# Install the pinned gitleaks, then the repo's hooks
make install-gitleaks
make install-hooks
# pre-commit runs: gitleaks git --staged --config .gitleaks.toml --redact --verbose
```

**Scan the working tree** (tracked plus untracked, non-ignored files):
```bash
make secret-scan-local   # scripts/secret-scan-tree.sh: gitleaks dir on that file set
```

**Scan repository history:**
```bash
# Using gitleaks: new findings only, .gitleaksignore is the baseline
make security-scan
# or directly, with a report
gitleaks git . --config .gitleaks.toml --gitleaks-ignore-path .gitleaksignore \
  --redact --verbose --report-path gitleaks-report.json

# Using trufflehog
trufflehog git file://. --json > trufflehog-report.json
```

**Configuration validation:**
```bash
# Validate config and scan it for plaintext secrets
ctxt config validate

# Lint: validation, secrets, file permissions, access smells
ctxt config doctor

# Scan the config directory as files — use gitleaks:
gitleaks dir ~/.config/contexthelp/ --verbose --redact
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

# 3. Update the env var the provider client reads
#    (and the secrets store, if you keep the key there too)
export OPENAI_API_KEY="${OPENAI_API_KEY_NEW}"
printf '%s' "$OPENAI_API_KEY_NEW" | dpkms secret set OPENAI_API_KEY

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
# 2. Replace the stored token (overwrites the keychain entry)
ctxt registry login uxpatterns

# 3. Test registry access
ctxt registry sync uxpatterns

# 4. Revoke old token
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
git filter-repo --invert-paths --path path/to/leaked-file

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
- [ ] Keep secrets out of committed `ctxt.yaml` / `dpkms.yaml`: env vars where the code reads them, otherwise a gitignored file passed as a second `--config` (config files do not expand `${VAR}`)
- [ ] Validate configuration: `ctxt config validate`
- [ ] Enable OS-level disk encryption (FileVault / LUKS / BitLocker) until ADR-019 ships

### Development Workflow

- [ ] Never commit secrets to version control
- [ ] Use separate API keys for dev/staging/prod
- [ ] Install pre-commit hooks for secret scanning
- [ ] Review diffs before committing: `git diff --cached`
- [ ] Use read-only API keys when possible
- [ ] Rotate development keys quarterly

### Production Deployment

- [ ] Use production-grade secret management (Vault, AWS Secrets Manager)
- [ ] Remote access only as `server.access: protected` (or `public`) with
      static tokens, narrowest role per client, `server.allowed_hosts` set
- [ ] Terminate TLS at a reverse proxy (dpkms has no TLS listener)
- [ ] Use disk encryption for the data directory (no built-in encryption at rest)
- [ ] Set restrictive file permissions (600/700)
- [ ] Disable telemetry: `CH_DISABLE_TELEMETRY=true`
- [ ] Forward the audit log (`audit.syslog` / `audit.webhook`) and set `security.alerts`
- [ ] Configure automated backups (encrypted)
- [ ] Set up monitoring and alerting
- [ ] Run as non-root user
- [ ] Use read-only filesystem where possible
- [ ] Review the audit log and `dpkms session list` weekly

### Regular Maintenance

- [ ] Rotate API keys quarterly
- [ ] Review access logs monthly
- [ ] Update dependencies regularly
- [ ] Scan for vulnerabilities: `govulncheck ./...` (Go) / `trivy fs .` (container)
- [ ] Test backup restoration
- [ ] Review and update security policies
- [ ] Conduct security training for team

---

## 🔗 Additional Resources

### Documentation

- [Environment Variables Reference](environment-variables/README.md)
- [Configuration Guide](ctxt/configuration.md)
- [CLI Flag Binding](ctxt/cli-flag-binding.md)
- [ADR-019: Encryption Architecture](decisions/ADR-019-encryption-and-privacy.md)
- [ADR-023: Authentication Model](decisions/ADR-023-authentication-authorization-model.md)
- [Security Design](dpkms/security.md)
- [Compliance — GDPR, SOC 2, erasure, dependency assessment](operations/compliance.md)

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
