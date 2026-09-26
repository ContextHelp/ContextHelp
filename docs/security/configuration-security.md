# Configuration Security

This document provides guidance on securing ContextHelp configuration files and handling sensitive configuration values.

## Overview

Configuration files often contain secrets and sensitive information. ContextHelp provides multiple mechanisms to protect configuration while keeping it flexible and maintainable.

## Configuration File Security

### File Permissions

Configuration files should have restricted permissions to prevent unauthorized access:

```bash
# Secure main configuration
chmod 600 ~/.config/contexthelp/config.yaml
chmod 700 ~/.config/contexthelp/

# System-wide configuration
sudo chmod 600 /etc/contexthelp/config.yaml
sudo chmod 700 /etc/contexthelp/

# Secret files
chmod 600 ~/.config/contexthelp/secrets/*
chmod 700 ~/.config/contexthelp/secrets/

# Verify permissions
ls -la ~/.config/contexthelp/config.yaml
# Should show: -rw------- (600)

ls -la ~/.config/contexthelp/
# Should show: drwx------ (700)
```

### File Ownership

Configuration files should be owned by the user running ContextHelp:

```bash
# Check ownership
ls -la ~/.config/contexthelp/config.yaml

# Correct ownership (for personal use)
chown $USER:$USER ~/.config/contexthelp/config.yaml

# For system daemon
sudo chown contexthelp:contexthelp /etc/contexthelp/config.yaml
```

### File Encryption

Optionally encrypt configuration files:

```bash
# Using GPG
gpg --symmetric ~/.config/contexthelp/config.yaml
# Creates config.yaml.gpg

# Using git-crypt
git-crypt init
echo "*.secret.yaml filter=git-crypt diff=git-crypt" > .gitattributes
git-crypt lock

# Using OpenSSL
openssl enc -aes-256-cbc -in config.yaml -out config.yaml.enc
# Requires password to decrypt

# Using SOPS
sops config.yaml
# Opens encrypted file in editor
```

## Managing Secrets in Configuration

### Best Practice: Environment Variables

Store secrets in environment variables, reference them in configuration:

```yaml
# config.yaml
registries:
  - name: private-registry
    url: https://registry.example.com
    auth:
      token: ${CH_REGISTRY_TOKEN_PRIVATE}

storage:
  type: postgres
  connection_string: ${CONTEXTHELP_DB_URL}

providers:
  openai:
    api_key: ${OPENAI_API_KEY}
    model: gpt-4o
```

Set environment variables before running:

```bash
# Load from environment file
export $(cat ~/.config/contexthelp/.env | xargs)

# Load from secret manager
export CH_REGISTRY_TOKEN_PRIVATE=$(pass show registries/private)
export CONTEXTHELP_DB_URL=$(vault kv get -field=url secret/prod/postgres)
export OPENAI_API_KEY=$(aws secretsmanager get-secret-value --secret-id openai-key --query SecretString --output text)

# Run application
ctxt serve
```

### Alternative: External Secret Files

Reference external secret files instead of embedding them:

```yaml
# config.yaml
registries:
  - name: private-registry
    url: https://registry.example.com
    auth:
      token_file: ${SECRETS_DIR}/registry-token

storage:
  type: postgres
  # Connection string in separate file
  connection_file: ${SECRETS_DIR}/postgres-url

providers:
  openai:
    api_key_file: ${SECRETS_DIR}/openai-key
    model: gpt-4o
```

Structure:
```
~/.config/contexthelp/
├── config.yaml          (0600)
└── secrets/             (0700)
    ├── registry-token   (0600)
    ├── postgres-url     (0600)
    └── openai-key       (0600)
```

Create and protect secret files:

```bash
# Create secrets directory
mkdir -p ~/.config/contexthelp/secrets
chmod 700 ~/.config/contexthelp/secrets

# Add secrets
echo "ghp_abc123def456" > ~/.config/contexthelp/secrets/registry-token
echo "postgresql://user:pass@host/db" > ~/.config/contexthelp/secrets/postgres-url
echo "sk-proj-123456789" > ~/.config/contexthelp/secrets/openai-key

# Restrict permissions
chmod 600 ~/.config/contexthelp/secrets/*

# Set environment variable
export SECRETS_DIR="$HOME/.config/contexthelp/secrets"
```

### Anti-Pattern: Hardcoded Secrets in Config

Never hardcode secrets in configuration files:

```yaml
# BAD - Never do this
registries:
  - name: private-registry
    url: https://registry.example.com
    auth:
      token: ghp_abc123def456  # DON'T HARDCODE SECRETS

storage:
  type: postgres
  connection_string: postgresql://user:SecurePass123@db.example.com/contexthelp  # DON'T

providers:
  openai:
    api_key: sk-proj-abc123def456  # DON'T
```

## Configuration Validation

### Syntax Validation

Validate configuration file syntax:

```bash
# Validate YAML syntax
ctxt config validate

# Check with verbose output
ctxt config validate --verbose

# Export validation report
ctxt config validate --output json > validation-report.json
```

### Security Validation

Check configuration for security issues:

```bash
# Scan for hardcoded secrets
ctxt security validate config.yaml

# Check file permissions
ls -la ~/.config/contexthelp/config.yaml
chmod 600 ~/.config/contexthelp/config.yaml  # If needed

# Verify environment variables are set
ctxt config check-env-vars
```

### Configuration Review Checklist

Before deploying configuration:

- [ ] No hardcoded secrets in YAML/JSON files
- [ ] All secrets use environment variables or external files
- [ ] Configuration files have mode 0600 or 0700
- [ ] Secret files have mode 0600
- [ ] Secret directories have mode 0700
- [ ] File ownership is correct
- [ ] No plaintext passwords in `.env` files
- [ ] `.env` files are `.gitignore`d
- [ ] No test/mock secrets that look real
- [ ] All references use `${VARIABLE}` syntax correctly

## Configuration as Code

When storing configuration in version control, be extra careful:

### What to Commit

```yaml
# GOOD - Safe configuration structure
registries:
  - name: private-registry
    url: https://registry.example.com  # No credentials in URL
    type: taxonomy

storage:
  type: postgres
  # No hardcoded password

providers:
  openai:
    model: gpt-4o
    max_tokens: 4096
    # No API key
```

### What NOT to Commit

```yaml
# BAD - Never commit secrets
registries:
  - name: private-registry
    url: https://registry.example.com
    auth:
      token: ghp_abc123def456  # DON'T COMMIT

# BAD - Never commit passwords
storage:
  type: postgres
  connection_string: postgresql://user:password@host/db  # DON'T COMMIT

# BAD - Never commit in environment variables
providers:
  openai:
    api_key: sk-proj-abc123def456  # DON'T COMMIT
```

### Git Hooks for Configuration

Use pre-commit hooks to prevent committing secrets:

```bash
#!/bin/bash
# .git/hooks/pre-commit

echo "Checking configuration files for secrets..."

# Check staged files
STAGED_FILES=$(git diff --cached --name-only)

for file in $STAGED_FILES; do
    # Skip test files
    if [[ $file == *"test"* ]]; then
        continue
    fi

    # Check for secrets patterns in YAML/JSON config files
    if [[ $file == *.yaml ]] || [[ $file == *.yml ]] || [[ $file == *.json ]]; then
        if git diff --cached -- "$file" | grep -E "api_key:|password:|token:|secret:" > /dev/null; then
            # Check if it's a reference or actual value
            if git diff --cached -- "$file" | grep -E "(api_key:|password:|token:|secret:)\s+\$\{" > /dev/null; then
                continue  # OK - it's a variable reference
            else
                echo "ERROR: Found potential secret in $file"
                exit 1
            fi
        fi
    fi
done

echo "✓ Configuration security check passed"
exit 0
```

Install:
```bash
cp hooks/pre-commit .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

## Configuration for Different Environments

### Development Configuration

```yaml
# config.dev.yaml
storage:
  type: sqlite
  path: ~/.local/share/contexthelp/dev.db

server:
  http:
    port: 7700
    public: false
  grpc:
    port: 7701

logging:
  level: debug
  format: console
  sanitize: true

privacy:
  telemetry: false
  allow_external_pipelines: false

providers:
  openai:
    api_key: ${OPENAI_API_KEY}  # From environment
    model: gpt-4o
```

### Staging Configuration

```yaml
# config.staging.yaml
storage:
  type: postgres
  connection_string: ${CONTEXTHELP_DB_URL}

server:
  http:
    port: 8080
    public: false
  grpc:
    port: 8081

logging:
  level: info
  format: json
  sanitize: true

security:
  encryption:
    enabled: true
    key_derivation: pbkdf2

providers:
  openai:
    api_key: ${OPENAI_API_KEY}
    model: gpt-4o
```

### Production Configuration

```yaml
# config.production.yaml
storage:
  type: postgres
  connection_string: ${CONTEXTHELP_DB_URL}

server:
  http:
    port: 8080
    public: false  # Behind reverse proxy
  grpc:
    port: 8081

logging:
  level: warn
  format: json
  sanitize: true
  outputs:
    - type: file
      path: /var/log/contexthelp/contexthelp.log
    - type: syslog
      facility: local0

security:
  encryption:
    enabled: true
    key_derivation: argon2

providers:
  openai:
    api_key: ${OPENAI_API_KEY}
    model: gpt-4o

registries:
  - name: company-registry
    url: ${COMPANY_REGISTRY_URL}
    auth:
      token: ${REGISTRY_TOKEN}
```

Load the correct configuration:

```bash
#!/bin/bash
# Run with environment-specific config

ENV=${ENVIRONMENT:-development}
CONFIG_FILE="config.$ENV.yaml"

if [ ! -f "$CONFIG_FILE" ]; then
    echo "ERROR: Configuration file not found: $CONFIG_FILE"
    exit 1
fi

# Load environment secrets
export $(cat .env.$ENV | xargs)

# Run application
ctxt --config "$CONFIG_FILE" serve
```

## Container Configuration

### Docker Best Practices

```dockerfile
FROM alpine:latest

# Create unprivileged user
RUN addgroup -S contexthelp && adduser -S contexthelp -G contexthelp

# Copy configuration template (no secrets)
COPY config.yaml.template /etc/contexthelp/config.yaml.template

# Create secrets directory
RUN mkdir -p /var/contexthelp/secrets && \
    chmod 700 /var/contexthelp/secrets && \
    chown contexthelp:contexthelp /var/contexthelp/secrets

# Switch to unprivileged user
USER contexthelp

# Secrets provided via:
# - Environment variables (for container orchestration)
# - Secret volumes (for Docker/Kubernetes)
# - Secret manager sidecars
```

```yaml
# docker-compose.yml
version: '3.8'

services:
  contexthelp:
    image: contexthelp:latest
    environment:
      # Load secrets from .env file or Docker secrets
      CONTEXTHELP_DB_URL: ${CONTEXTHELP_DB_URL}
      OPENAI_API_KEY: ${OPENAI_API_KEY}
      CH_REGISTRY_TOKEN_PRIVATE: ${CH_REGISTRY_TOKEN_PRIVATE}
    volumes:
      # Mount secrets directory
      - contexthelp_secrets:/var/contexthelp/secrets:ro
    env_file:
      - .env.production
    depends_on:
      - postgres

  postgres:
    image: postgres:15
    environment:
      POSTGRES_DB: contexthelp
      POSTGRES_USER: contexthelp
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data

secrets:
  contexthelp_db_password:
    external: true  # Managed by Docker/Swarm

volumes:
  contexthelp_secrets:
  postgres_data:
```

### Kubernetes Configuration

```yaml
# kubernetes/secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: contexthelp-secrets
  namespace: default
type: Opaque
data:
  # Use kubectl create secret to populate these
  openai-api-key: <base64-encoded-key>
  registry-token: <base64-encoded-token>
  db-password: <base64-encoded-password>

---
# kubernetes/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: contexthelp-config
  namespace: default
data:
  config.yaml: |
    storage:
      type: postgres
      connection_string: "postgresql://contexthelp:${DB_PASSWORD}@postgres:5432/contexthelp"
    providers:
      openai:
        api_key: ${OPENAI_API_KEY}
        model: gpt-4o

---
# kubernetes/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: contexthelp
  namespace: default
spec:
  replicas: 2
  template:
    spec:
      containers:
      - name: contexthelp
        image: contexthelp:latest
        envFrom:
        - secretRef:
            name: contexthelp-secrets
        env:
        - name: CONTEXTHELP_CONFIG
          value: /etc/contexthelp/config.yaml
        volumeMounts:
        - name: config
          mountPath: /etc/contexthelp
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: contexthelp-config
```

Create secrets:

```bash
# Create from environment variables
kubectl create secret generic contexthelp-secrets \
  --from-literal=openai-api-key="$OPENAI_API_KEY" \
  --from-literal=registry-token="$REGISTRY_TOKEN" \
  --from-literal=db-password="$DB_PASSWORD"

# Or from files
kubectl create secret generic contexthelp-secrets \
  --from-file=openai-api-key=./secrets/openai-key \
  --from-file=registry-token=./secrets/registry-token \
  --from-file=db-password=./secrets/db-password
```

## Configuration Documentation

Document how to configure securely:

```markdown
# Configuration Guide

## Prerequisites

Ensure you have:
1. Configuration file (created with `ctxt config init`)
2. Secret files or environment variables for:
   - Database URL
   - OpenAI API key
   - Registry tokens

## Setting Up Configuration

### 1. Create Configuration File

\`\`\`bash
mkdir -p ~/.config/contexthelp
touch ~/.config/contexthelp/config.yaml
chmod 600 ~/.config/contexthelp/config.yaml
\`\`\`

### 2. Add Secret References

Edit `config.yaml` and reference environment variables:

\`\`\`yaml
storage:
  type: postgres
  connection_string: \${CONTEXTHELP_DB_URL}

providers:
  openai:
    api_key: \${OPENAI_API_KEY}
\`\`\`

### 3. Set Environment Variables

\`\`\`bash
export CONTEXTHELP_DB_URL=postgresql://user:pass@host/db
export OPENAI_API_KEY=sk-proj-...

ctxt serve
\`\`\`

## Securing Configuration

- Always use environment variables for secrets
- Never commit configuration with secrets
- Keep configuration files mode 0600
- Rotate secrets regularly
- Use secret managers in production
```

## Validation Tools

Create a validation script:

```bash
#!/bin/bash
# validate-config.sh

set -euo pipefail

CONFIG_FILE="${1:-.config/contexthelp/config.yaml}"

echo "Validating configuration: $CONFIG_FILE"

# Check file exists
if [ ! -f "$CONFIG_FILE" ]; then
    echo "ERROR: Configuration file not found: $CONFIG_FILE"
    exit 1
fi

# Check file permissions
PERMS=$(stat -c %a "$CONFIG_FILE" 2>/dev/null || stat -f %OLp "$CONFIG_FILE" 2>/dev/null)
if [ "$PERMS" != "600" ] && [ "$PERMS" != "0600" ]; then
    echo "WARNING: Configuration file has insecure permissions: $PERMS (should be 0600)"
fi

# Check for hardcoded secrets
if grep -E "(api_key|password|token|secret):\s+['\"]?[a-zA-Z0-9_\-\.]+['\"]?" "$CONFIG_FILE" > /dev/null; then
    # Check if they're variable references
    if ! grep -E "(api_key|password|token|secret):\s+\$\{" "$CONFIG_FILE" > /dev/null; then
        echo "ERROR: Found potential hardcoded secrets in configuration"
        exit 1
    fi
fi

# Check YAML syntax
if ! python3 -m yaml.load "$CONFIG_FILE" > /dev/null 2>&1; then
    echo "ERROR: Invalid YAML syntax in configuration"
    exit 1
fi

# Check required keys
required_keys=("storage.type" "providers.openai.model")
for key in "${required_keys[@]}"; do
    if ! grep -q "$key" "$CONFIG_FILE"; then
        echo "ERROR: Missing required configuration key: $key"
        exit 1
    fi
done

echo "✓ Configuration validation passed"
exit 0
```

## Related Documentation

- [Secret Management Best Practices](./secret-management.md)
- [Secrets Validation & Log Sanitization](./secrets-validation-and-log-sanitization.md)
- [Environment Variables Reference](../environment-variables.md)
- [Security Model](./security-model.md)

---

**Last Updated**: 2025-01-26
**Version**: 1.0
**Status**: Active
**Maintained by**: Security Team
