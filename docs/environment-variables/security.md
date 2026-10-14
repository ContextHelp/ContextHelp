# Security Environment Variables

Secrets backend and backup-bundle encryption. Inbound auth has no env
vars: set `server.access` and `server.auth` in the config file — see
[Inbound auth, roles and scopes](../manual/reference/config-and-permissions.md#inbound-auth-roles-and-scopes).

---

## Secrets backend

Where `ctxt` and `dpkms` look up secrets such as provider API keys.

### CTXT_SECRETS_BACKEND
**Type:** string\
**Default:** `env`\
**Values:** `env`, `keyring`, `agefile`, `onepassword`, `ghsecrets`\
**Config key:** `secrets.backend`

`env` reads each secret from the environment variable of the same name.

```bash
CTXT_SECRETS_BACKEND=keyring
```

### CTXT_AGE_FILE
**Type:** string\
**Default:** _(none)_\
**Config key:** `secrets.age_file`

Path to the age-encrypted YAML secrets file. Required by the `agefile`
backend.

### CTXT_AGE_IDENTITY
**Type:** string\
**Default:** _(none)_\
**Config key:** `secrets.age_identity_file`

Path to the age identity (private key) file. Required by the `agefile`
backend.

```bash
CTXT_SECRETS_BACKEND=agefile
CTXT_AGE_FILE=~/.config/contexthelp/secrets.age
CTXT_AGE_IDENTITY=~/.config/contexthelp/age.key
```

Config-file only: `secrets.keychain_service` (default `ctxt`),
`secrets.onepassword_vault`, `secrets.gh_repo`.

---

## Backup encryption

### CTXT_BACKUP_PASSPHRASE
**Type:** string (secret)\
**Default:** _(none)_

Passphrase for encrypting `ctxt config backup` bundles and decrypting them
on `ctxt config restore`. Resolution order: `--passphrase` flag (backup
only), this env var, the OS keychain, then an interactive prompt.
`backup.encrypt_by_default: true` encrypts every backup. **Never commit it
to version control.**

```bash
CTXT_BACKUP_PASSPHRASE=$(vault kv get -field=passphrase secret/ctxt-backup) \
  ctxt config backup --encrypt
```

---

## Secret Management Patterns

### AWS Secrets Manager
```bash
CTXT_BACKUP_PASSPHRASE=$(aws secretsmanager get-secret-value \
  --secret-id ctxt_backup_passphrase \
  --query SecretString --output text)
```

### HashiCorp Vault
```bash
OPENAI_API_KEY=$(vault kv get -field=key secret/openai)
CTXT_BLOB_S3_SECRET_KEY=$(vault kv get -field=blob_secret secret/ctxt)
```

### Kubernetes Secrets
```yaml
env:
  - name: ANTHROPIC_API_KEY
    valueFrom:
      secretKeyRef:
        name: contexthelp-secrets
        key: anthropic-api-key
```

---

## Related

- [../security/secret-management.md](../security/secret-management.md) — Secret lifecycle
- [../security/security-model.md](../security/security-model.md) — Security architecture
- [../security/configuration-security.md](../security/configuration-security.md) — Secure configuration
