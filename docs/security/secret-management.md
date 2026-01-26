# Secret Management Best Practices

This document provides recommended approaches and procedures for managing secrets in ContextHelp environments.

## Overview

Secret management in ContextHelp encompasses the full lifecycle of sensitive information:

1. **Generation**: Creating strong secrets
2. **Storage**: Protecting secrets at rest
3. **Distribution**: Securely delivering secrets to applications
4. **Rotation**: Regularly changing secrets
5. **Revocation**: Removing compromised secrets
6. **Audit**: Logging secret access and changes

## Types of Secrets in ContextHelp

### API Keys

**Examples**: OpenAI, Anthropic, GitHub, Stripe
**Sensitivity**: High
**Rotation**: Monthly or on compromise
**Storage**: Environment variables or secret manager

```bash
# GOOD - From environment
export OPENAI_API_KEY=$(pass show openai/production)
ctxt analyze --model gpt-4o

# GOOD - From secret manager
export OPENAI_API_KEY=$(aws secretsmanager get-secret-value \
  --secret-id prod/openai-key \
  --query SecretString \
  --output text)

# BAD - Hardcoded
export OPENAI_API_KEY="sk-proj-hardcoded-key"  # DON'T DO THIS
```

### Database Credentials

**Examples**: PostgreSQL, MySQL, MongoDB
**Sensitivity**: Critical
**Rotation**: Quarterly or on compromise
**Storage**: Secret manager with encryption

```bash
# GOOD - From secret manager with encryption
export POSTGRES_PASSWORD=$(aws secretsmanager get-secret-value \
  --secret-id prod/postgres \
  --query SecretString \
  --output json | jq -r '.password')

# GOOD - Using vault
export POSTGRES_PASSWORD=$(vault kv get -field=password secret/prod/postgres)

# BAD - In config file
POSTGRES_PASSWORD=my_database_password  # DON'T
```

### Registry Tokens

**Examples**: GitHub, GitLab, private registries
**Sensitivity**: High
**Rotation**: Quarterly or on scope change
**Storage**: Environment variables or secret files

```bash
# GOOD - From environment
export CH_REGISTRY_TOKEN_PRIVATE=$(pass show registries/private-token)

# GOOD - From secret file with restricted permissions
export CH_REGISTRY_TOKEN_PRIVATE=$(cat /etc/contexthelp/secrets/registry-token)

# BAD - Hardcoded in config
CH_REGISTRY_TOKEN_PRIVATE: "ghp_abc123def456"  # DON'T
```

### Encryption Keys

**Examples**: Storage encryption, signing keys
**Sensitivity**: Critical
**Rotation**: Annually or on key compromise
**Storage**: Hardware security module (HSM) or secure vault

```bash
# GOOD - From HSM
ENCRYPTION_KEY=$(aws kms get-data-key \
  --key-id arn:aws:kms:region:account:key/id \
  --query 'Plaintext' \
  --output text | base64 -d)

# GOOD - From secret manager
ENCRYPTION_PASSPHRASE=$(vault kv get -field=passphrase secret/prod/encryption)

# BAD - In environment variable
ENCRYPTION_KEY="my-secret-key"  # DON'T
```

## Secret Storage Strategies

### Strategy 1: Environment Variables (Development)

**Best For**: Local development, testing
**Security Level**: Low
**Advantages**: Simple, fast, no additional tooling
**Disadvantages**: Exposed in process listings, shell history

```bash
# .env file (git-ignored)
OPENAI_API_KEY=sk-dev-abc123
POSTGRES_PASSWORD=dev_password

# Load and use
export $(cat .env | xargs)
ctxt analyze "test"
```

**Protection**:
```bash
# Restrict .env file permissions
chmod 600 .env

# Don't commit to git
echo ".env" >> .gitignore

# Clear shell history
unset OPENAI_API_KEY
history -c
```

### Strategy 2: Pass (Password Manager)

**Best For**: Local development, small teams
**Security Level**: Medium
**Advantages**: Encrypted storage, easy to use, no cloud dependency
**Disadvantages**: Manual management, single-point-of-failure

```bash
# Initialize pass
pass init your-gpg-key

# Store secrets
pass insert openai/production
pass insert prod/postgres

# Retrieve in scripts
export OPENAI_API_KEY=$(pass show openai/production)
export POSTGRES_PASSWORD=$(pass show prod/postgres)

# Generate strong passwords
pass generate openai/production 32
```

**Typical Structure**:
```
password-store/
├── openai/
│   ├── development
│   └── production
├── postgres/
│   ├── development
│   └── production
├── github/
│   └── personal-token
└── registries/
    ├── private-token
    └── company-token
```

### Strategy 3: AWS Secrets Manager

**Best For**: AWS deployments, team environments
**Security Level**: High
**Advantages**: Scalable, auditable, rotatable, encrypted
**Disadvantages**: AWS-specific, requires credentials, costs

```bash
# Store secret
aws secretsmanager create-secret \
  --name prod/openai-key \
  --secret-string '{"api_key":"sk-..."}'

# Retrieve in script
export OPENAI_API_KEY=$(aws secretsmanager get-secret-value \
  --secret-id prod/openai-key \
  --query SecretString \
  --output text | jq -r '.api_key')

# Or use in CloudFormation
Resources:
  ContextHelpApp:
    Type: AWS::ECS::Service
    Properties:
      Environment:
        - Name: OPENAI_API_KEY
          ValueFrom: !Sub 'arn:aws:secretsmanager:${AWS::Region}:${AWS::AccountId}:secret:prod/openai-key'
```

### Strategy 4: HashiCorp Vault

**Best For**: Enterprise, complex environments
**Security Level**: Very High
**Advantages**: Centralized, rotatable, audit logging, multi-cloud
**Disadvantages**: Complex to set up, requires maintenance

```bash
# Authenticate
vault login -method=oidc

# Store secret
vault kv put secret/prod/openai \
  api_key="sk-..."

# Retrieve
export OPENAI_API_KEY=$(vault kv get -field=api_key secret/prod/openai)

# Enable secret rotation
vault write -f secret/config/prod/openai/rotate
```

### Strategy 5: Kubernetes Secrets

**Best For**: Kubernetes deployments
**Security Level**: Medium (at rest: low, in transit: high with encryption)
**Advantages**: Native to Kubernetes, RBAC integration
**Disadvantages**: Etcd vulnerability, not encrypted by default

```yaml
# Create secret
apiVersion: v1
kind: Secret
metadata:
  name: contexthelp-secrets
type: Opaque
data:
  openai-api-key: c2stcHJvai1hYmMxMjM=  # base64 encoded
  postgres-password: cGFzc3dvcmQxMjM=

---
# Use in Pod
apiVersion: v1
kind: Pod
metadata:
  name: contexthelp
spec:
  containers:
  - name: app
    image: contexthelp:latest
    env:
    - name: OPENAI_API_KEY
      valueFrom:
        secretKeyRef:
          name: contexthelp-secrets
          key: openai-api-key
```

**Enable Encryption at Rest**:
```yaml
# kube-apiserver argument
--encryption-provider-config=/etc/kubernetes/encryption-config.yaml
```

### Strategy 6: Git-Crypt or SOPS

**Best For**: Configuration as code, team collaboration
**Security Level**: High
**Advantages**: Encrypted secrets in git, version control, diff support
**Disadvantages**: Requires key distribution, complex setup

```bash
# Initialize git-crypt
git-crypt init

# Mark secrets for encryption
echo "secrets/" > .gitattributes
echo "config/*.secret.yaml" >> .gitattributes

# Add team members' GPG keys
git-crypt add-gpg-user user@company.com

# Encrypt on commit, decrypt on checkout automatically
git add .
git commit -m "Add secrets"
```

Or using SOPS:

```bash
# Create encrypted file
sops secrets/prod.yaml

# Decrypt for use
export $(sops -d secrets/prod.yaml | grep -v '^#' | xargs)

# Or in CI/CD
sops -d secrets/prod.yaml | export $(xargs)
```

## Secret Rotation

### Rotation Strategies

#### Time-Based Rotation

Rotate secrets on a schedule:

| Secret Type | Interval | Notes |
|------------|----------|-------|
| API Keys | Monthly | Less critical keys |
| Database Passwords | Quarterly | Requires coordination |
| Encryption Keys | Annually | Plan for re-encryption |
| Registry Tokens | Quarterly | If token is compromised |
| SSH Keys | Annually | Or on employee change |

#### Event-Based Rotation

Rotate secrets when:
- Employee joins or leaves (immediately)
- Secret is compromised (immediately)
- Failed authentication attempts spike
- Security audit finding (within 30 days)
- Dependency update with security fix

### Rotation Procedures

#### API Key Rotation

```bash
# 1. Generate new key in provider (e.g., OpenAI)
# 2. Update in secret manager
aws secretsmanager update-secret \
  --secret-id prod/openai-key \
  --secret-string '{"api_key":"sk-new-..."}'

# 3. Verify with test
export OPENAI_API_KEY=$(aws secretsmanager get-secret-value \
  --secret-id prod/openai-key \
  --query SecretString \
  --output text | jq -r '.api_key')

ctxt analyze --test "rotation test"

# 4. Update all deployments
kubectl set env deployment/contexthelp \
  -e OPENAI_API_KEY="$(pass show openai/production)"

# 5. Monitor for errors
kubectl logs -f deployment/contexthelp | grep -i error

# 6. Revoke old key in provider
# 7. Document rotation in changelog
```

#### Database Password Rotation

```bash
# 1. Generate new password
NEW_PASSWORD=$(openssl rand -base64 32)

# 2. Update in database
psql -U admin -d contexthelp -c \
  "ALTER USER ctxt_prod WITH PASSWORD '$NEW_PASSWORD';"

# 3. Update in secret manager
vault kv put secret/prod/postgres password="$NEW_PASSWORD"

# 4. Restart services (if no connection pooling)
# Services with connection pooling pick up automatically

# 5. Verify connectivity
export POSTGRES_PASSWORD=$(vault kv get -field=password secret/prod/postgres)
psql -U ctxt_prod -d contexthelp -c "SELECT 1"

# 6. Document rotation
echo "$(date): Rotated postgres password" >> /var/log/rotations.log
```

#### Encryption Key Rotation

```bash
# 1. Generate new key
NEW_KEY=$(openssl rand 32 | base64)

# 2. Update key manager
vault kv put secret/prod/encryption key="$NEW_KEY"

# 3. Re-encrypt database with new key
# This requires application support
ctxt migrate-encryption \
  --old-key "$(vault kv get -field=key secret/prod/encryption-old)" \
  --new-key "$NEW_KEY"

# 4. Verify all data accessible
ctxt verify-encryption

# 5. Securely destroy old key after verification
vault kv delete secret/prod/encryption-old

# 6. Document in audit log
```

### Automated Rotation

Use cron jobs or Lambda functions for automated rotation:

```bash
#!/bin/bash
# /etc/cron.d/secret-rotation

# Rotate API keys monthly
0 2 1 * * root /usr/local/bin/rotate-api-keys.sh

# Rotate database password quarterly
0 2 1 */3 * root /usr/local/bin/rotate-db-password.sh

# Rotate encryption keys annually
0 2 1 1 * root /usr/local/bin/rotate-encryption-key.sh
```

Script example:

```bash
#!/bin/bash
# /usr/local/bin/rotate-api-keys.sh

set -euo pipefail

LOG_FILE="/var/log/secret-rotation.log"

log() {
    echo "$(date): $*" >> "$LOG_FILE"
}

# Rotate OpenAI key
log "Starting OpenAI API key rotation"
NEW_KEY=$(curl -s -X POST https://api.openai.com/admin/rotate-key \
  -H "Authorization: Bearer $ADMIN_TOKEN" | jq -r '.api_key')

# Update in Vault
vault kv put secret/prod/openai api_key="$NEW_KEY"

# Verify
if OPENAI_API_KEY="$NEW_KEY" ctxt analyze --test "rotation test"; then
    log "OpenAI API key rotation successful"
else
    log "ERROR: OpenAI API key rotation failed"
    exit 1
fi
```

## Incident Response

### Secret Exposure Detection

Monitor for exposed secrets:

```bash
# Check logs for unredacted secrets
ctxt security verify-logs --show-unredacted

# Scan git history
git log -p | grep -i "sk-proj\|ghp_\|password"

# Check environment variables
env | grep -E "KEY|PASSWORD|TOKEN|SECRET"

# Monitor failed authentications
grep "authentication failed" ~/.local/share/contexthelp/logs/*.log
```

### Compromise Response Checklist

**Immediate (0-1 hour)**:
- [ ] Confirm secret exposure
- [ ] Stop using the compromised secret
- [ ] Revoke the secret in the provider
- [ ] Alert team members
- [ ] Check audit logs for unauthorized access
- [ ] Enable detailed logging temporarily

**Short Term (1-24 hours)**:
- [ ] Rotate the compromised secret
- [ ] Update in all systems
- [ ] Deploy updated configurations
- [ ] Monitor for malicious activity
- [ ] Generate incident report
- [ ] Check git history for commits with secret

**Medium Term (1-7 days)**:
- [ ] Root cause analysis
- [ ] Update secret management procedures
- [ ] Add monitoring/detection for this secret type
- [ ] Security training for involved team
- [ ] Update runbooks

**Long Term (ongoing)**:
- [ ] Implement automated secret scanning
- [ ] Improve access controls
- [ ] Regular security audits
- [ ] Document lessons learned

### Example Incident Response

```bash
#!/bin/bash
# Incident response for exposed OpenAI key

COMPROMISED_KEY="sk-proj-abc123def456"
INCIDENT_ID="INC-2025-001"

log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] $*" | tee -a /var/log/incident-$INCIDENT_ID.log
}

# 1. Confirm exposure
log "Confirming secret exposure in logs..."
grep -r "$COMPROMISED_KEY" ~/.local/share/contexthelp/logs/ && \
    log "CONFIRMED: Secret found in logs" || \
    log "NOT FOUND: Secret not in logs"

# 2. Revoke in provider
log "Revoking compromised key in OpenAI..."
curl -X DELETE https://api.openai.com/admin/keys/$COMPROMISED_KEY \
    -H "Authorization: Bearer $ADMIN_TOKEN"

# 3. Check for unauthorized access
log "Checking for suspicious API usage..."
curl -s https://api.openai.com/admin/usage?key=$COMPROMISED_KEY > \
    /tmp/openai-usage-$INCIDENT_ID.json
log "Usage report saved to /tmp/openai-usage-$INCIDENT_ID.json"

# 4. Rotate to new key
log "Generating new OpenAI key..."
NEW_KEY=$(curl -s -X POST https://api.openai.com/admin/keys \
    -H "Authorization: Bearer $ADMIN_TOKEN" | jq -r '.key')

# 5. Update in vault
log "Updating secret in Vault..."
vault kv put secret/prod/openai api_key="$NEW_KEY"

# 6. Update all systems
log "Updating all deployments..."
kubectl set env deployment/contexthelp OPENAI_API_KEY="$NEW_KEY"

# 7. Verify
log "Verifying new key works..."
if export OPENAI_API_KEY="$NEW_KEY"; then
    ctxt analyze --test "incident response test"
    log "SUCCESS: New key verified"
else
    log "ERROR: New key verification failed"
    exit 1
fi

log "Incident response complete. Details in /var/log/incident-$INCIDENT_ID.log"
```

## Secret Scanning

### Pre-Commit Hooks

Prevent secrets from being committed:

```bash
#!/bin/bash
# .git/hooks/pre-commit

set -e

# Install this script
# cp hooks/pre-commit .git/hooks/
# chmod +x .git/hooks/pre-commit

STAGED_FILES=$(git diff --cached --name-only)

echo "Checking for secrets in staged files..."

# Patterns that indicate secrets
PATTERNS=(
    "sk-proj-"
    "sk-ant-"
    "ghp_"
    "password"
    "api_key"
    "secret"
    "token"
)

FOUND_SECRET=0

for pattern in "${PATTERNS[@]}"; do
    if git diff --cached | grep -i "$pattern" > /dev/null; then
        if ! git diff --cached | grep -i "$pattern" | grep -E "test|example|mock" > /dev/null; then
            echo "ERROR: Potential secret detected containing '$pattern'"
            FOUND_SECRET=1
        fi
    fi
done

if [ $FOUND_SECRET -eq 1 ]; then
    echo "Aborting commit. Verify secrets before committing."
    exit 1
fi

echo "✓ No secrets detected"
exit 0
```

Install:
```bash
cp hooks/pre-commit .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

### Server-Side Scanning

Scan repositories on push:

```bash
# Using GitLab
# .gitlab/merge_request_templates/default.md
Before merging, ensure:
- [ ] No secrets in code review
- [ ] Run `ctxt security validate` before push
- [ ] Secrets scanning passed in pipeline

# In CI/CD pipeline
stages:
  - security
  - build

secret-scan:
  stage: security
  image: python:3.11
  script:
    - pip install detect-secrets
    - detect-secrets scan --all-files --force-use-all-plugins
    - detect-secrets scan | grep "\"type\": \"Base64HighEntropyString\"" && exit 1 || true
  allow_failure: true
```

## For Plugin Developers

### Handling Secrets in Plugins

Never store hardcoded secrets:

```go
// BAD - Never do this
func (p *MyPlugin) Init(config map[string]interface{}) error {
    p.apiKey = "sk-proj-hardcoded"  // DON'T
    return nil
}

// GOOD - Get from environment
func (p *MyPlugin) Init(config map[string]interface{}) error {
    apiKey := os.Getenv("CH_PLUGIN_MYPLUGIN_API_KEY")
    if apiKey == "" {
        return fmt.Errorf("CH_PLUGIN_MYPLUGIN_API_KEY not set")
    }
    p.apiKey = apiKey
    return nil
}

// GOOD - Get from config file with restricted permissions
func (p *MyPlugin) Init(config map[string]interface{}) error {
    configFile := os.Getenv("CH_PLUGIN_MYPLUGIN_CONFIG")
    cfg, err := ioutil.ReadFile(configFile)
    if err != nil {
        return fmt.Errorf("failed to read config: %w", err)
    }
    // Parse config...
    return nil
}
```

### Configuration Example

```yaml
plugins:
  load:
    - name: my-plugin
      config:
        # GOOD - Reference environment variables
        api_key: ${MY_PLUGIN_API_KEY}

        # GOOD - Reference config files
        credentials_file: /etc/contexthelp/secrets/my-plugin.yaml

        # GOOD - Safe defaults, secrets from environment
        endpoint: https://api.example.com
        timeout: 30s
```

## Monitoring & Audit

### Log Monitoring

Monitor for secret-related events:

```bash
# Watch for failed authentication attempts
tail -f ~/.local/share/contexthelp/logs/contexthelp.log | grep -i "auth.*fail"

# Alert on secret detection
tail -f ~/.local/share/contexthelp/logs/contexthelp.log | grep "SECRET_DETECTED"

# Monitor secret rotation events
tail -f ~/.local/share/contexthelp/logs/contexthelp.log | grep "secret.*rotated"
```

### Audit Logging

Configure comprehensive audit logging:

```yaml
logging:
  audit:
    enabled: true
    level: all
    events:
      - authentication_attempt
      - secret_access
      - secret_rotation
      - permission_check
      - configuration_change
      - registry_access
    output:
      file: /var/log/contexthelp/audit.log
      syslog: true
      format: json
```

### Metrics & Alerts

Set up monitoring:

```bash
# Prometheus metrics
contexthelp_secrets_detected_total
contexthelp_secrets_rotation_failures_total
contexthelp_authentication_failures_total
contexthelp_unauthorized_access_attempts_total

# Grafana dashboard: Secret Management Metrics
# Alert on:
# - Failed rotations
# - Unexpected secret access patterns
# - Authentication failures exceeding threshold
```

## Security Best Practices Summary

### DO

- **DO** use environment variables for API keys
- **DO** store encrypted secrets in a secret manager
- **DO** rotate secrets on a regular schedule
- **DO** use strong, random secrets (32+ characters)
- **DO** limit who has access to secrets
- **DO** audit secret access and changes
- **DO** verify secrets aren't leaked in logs
- **DO** enable log sanitization everywhere
- **DO** use HTTPS/TLS for secret transmission
- **DO** keep secrets out of version control

### DON'T

- **DON'T** hardcode secrets in code
- **DON'T** commit secrets to git
- **DON'T** log secrets unredacted
- **DON'T** store plaintext secrets on disk
- **DON'T** share secrets via insecure channels
- **DON'T** use the same secret for multiple services
- **DON'T** store secrets in public configuration files
- **DON'T** forget to rotate compromised secrets
- **DON'T** skip secret scanning in CI/CD
- **DON'T** assume "no one will look" at test secrets

## Tools & Resources

### Secret Management Tools

- **pass**: Password manager with encryption
- **AWS Secrets Manager**: Managed secret storage
- **HashiCorp Vault**: Enterprise secret management
- **SOPS**: Encrypted files in version control
- **git-crypt**: Transparent git encryption
- **1Password**: Commercial password manager
- **LastPass**: Commercial password manager

### Secret Scanning Tools

- **detect-secrets**: Python secret detection
- **GitGuardian**: SaaS secret scanning
- **TruffleHog**: Git repository secret scanning
- **gitleaks**: Git secret detection
- **Semgrep**: Code scanning with secret rules

### Related Documentation

- [Secrets Validation & Log Sanitization](./secrets-validation-and-log-sanitization.md)
- [Configuration Security](./configuration-security.md)
- [Environment Variables - Security Section](../environment-variables.md#security-best-practices)
- [dPKMS Security](../dpkms/security.md)

---

**Last Updated**: 2025-01-26
**Version**: 1.0
**Status**: Active
**Maintained by**: Security Team
