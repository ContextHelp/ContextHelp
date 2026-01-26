# Security Environment Variables

Encryption, authentication, and secret management.

---

## Encryption

### ENCRYPTION_ENABLED
**Type:** boolean  
**Default:** `false`

Enable at-rest encryption for storage.

```bash
ENCRYPTION_ENABLED=true
```

### ENCRYPTION_KEY_DERIVATION
**Type:** string  
**Default:** `pbkdf2`  
**Values:** `pbkdf2`, `argon2`

Key derivation function.

```bash
ENCRYPTION_KEY_DERIVATION=argon2
```

### ENCRYPTION_PASSPHRASE
**Type:** string (secret)  
**Default:** _(none)_  
**Required:** When encryption enabled

Encryption passphrase. **Never commit to version control.**

```bash
ENCRYPTION_PASSPHRASE=strong_passphrase_here
```

**Security best practices:**
```bash
# Use secret management
ENCRYPTION_PASSPHRASE=$(vault kv get -field=passphrase secret/encryption)

# Or prompt user
read -s -p "Encryption passphrase: " ENCRYPTION_PASSPHRASE
export ENCRYPTION_PASSPHRASE
```

---

## Authentication

### AUTH_ENABLED
**Type:** boolean  
**Default:** `false`

Enable authentication for API access.

```bash
AUTH_ENABLED=true
```

### JWT_SECRET
**Type:** string (secret)  
**Default:** _(none)_  
**Required:** When auth enabled

JWT signing secret. **Never commit to version control.**

```bash
JWT_SECRET=random_secret_key_here
```

**Generate secure secret:**
```bash
# 32-byte random secret
JWT_SECRET=$(openssl rand -base64 32)
export JWT_SECRET
```

### JWT_EXPIRATION
**Type:** duration  
**Default:** `24h`

JWT token expiration time.

```bash
JWT_EXPIRATION=12h
```

---

## Examples

### Development (No Security)
```bash
ENCRYPTION_ENABLED=false
AUTH_ENABLED=false
```

### Staging (Basic Security)
```bash
ENCRYPTION_ENABLED=true
ENCRYPTION_KEY_DERIVATION=pbkdf2
ENCRYPTION_PASSPHRASE=$(vault kv get -field=passphrase secret/staging/encryption)

AUTH_ENABLED=true
JWT_SECRET=$(vault kv get -field=secret secret/staging/jwt)
JWT_EXPIRATION=24h
```

### Production (Full Security)
```bash
ENCRYPTION_ENABLED=true
ENCRYPTION_KEY_DERIVATION=argon2
ENCRYPTION_PASSPHRASE=$(vault kv get -field=passphrase secret/prod/encryption)

AUTH_ENABLED=true
JWT_SECRET=$(vault kv get -field=secret secret/prod/jwt)
JWT_EXPIRATION=12h
```

---

## Secret Management Patterns

### AWS Secrets Manager
```bash
ENCRYPTION_PASSPHRASE=$(aws secretsmanager get-secret-value \
  --secret-id encryption_passphrase \
  --query SecretString --output text)

JWT_SECRET=$(aws secretsmanager get-secret-value \
  --secret-id jwt_secret \
  --query SecretString --output text)
```

### HashiCorp Vault
```bash
ENCRYPTION_PASSPHRASE=$(vault kv get -field=passphrase secret/encryption)
JWT_SECRET=$(vault kv get -field=secret secret/jwt)
```

### Kubernetes Secrets
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: contexthelp-secrets
type: Opaque
data:
  encryption-passphrase: <base64-encoded>
  jwt-secret: <base64-encoded>
```

Mount as environment variables:
```yaml
env:
  - name: ENCRYPTION_PASSPHRASE
    valueFrom:
      secretKeyRef:
        name: contexthelp-secrets
        key: encryption-passphrase
  - name: JWT_SECRET
    valueFrom:
      secretKeyRef:
        name: contexthelp-secrets
        key: jwt-secret
```

---

## Security Checklist

### Never Do This ❌
```bash
# .env file (committed to git)
ENCRYPTION_PASSPHRASE=my_password
JWT_SECRET=secret123
```

### Always Do This ✅
```bash
# .env.example (template, committed)
ENCRYPTION_PASSPHRASE=
JWT_SECRET=

# .env (actual values, in .gitignore)
ENCRYPTION_PASSPHRASE=actual_secure_passphrase
JWT_SECRET=actual_random_jwt_secret

# Or use secret management
ENCRYPTION_PASSPHRASE=$(vault kv get -field=passphrase secret/encryption)
JWT_SECRET=$(vault kv get -field=secret secret/jwt)
```

### Validation Script
```bash
#!/bin/bash
# validate-secrets.sh

required_secrets=("ENCRYPTION_PASSPHRASE" "JWT_SECRET")
for secret in "${required_secrets[@]}"; do
  if [ -z "${!secret}" ]; then
    echo "Error: $secret is not set"
    exit 1
  fi
  
  # Check minimum length
  if [ ${#!secret} -lt 16 ]; then
    echo "Error: $secret must be at least 16 characters"
    exit 1
  fi
done

echo "✓ All secrets validated"
```

---

## Related

- [../security/secret-management.md](../security/secret-management.md) — Secret lifecycle
- [../security/security-model.md](../security/security-model.md) — Security architecture
- [../security/configuration-security.md](../security/configuration-security.md) — Secure configuration
