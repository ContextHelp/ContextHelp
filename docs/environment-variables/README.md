# Environment Variables Documentation

**Version:** 0.1.0

Every environment variable `ctxt` and `dpkms` read, organized by topic.
Most settings have no env var and live only in the config file — see the
[config reference](../manual/reference/config-and-permissions.md).

---

## Overview

A setting resolves from the first source that sets it:

1. **Flag** (e.g. `--port`)
2. **`-c key=value` override**
3. **Environment variable** (e.g. `CH_SERVER_PORT`)
4. **Config file** — `CTXT_CONFIG` if set, else the project → user → system cascade
5. **Built-in default**

An empty env var counts as unset. The embedding-provider settings rank
the environment above `-c` — see
[ai-providers.md](ai-providers.md#override-it-for-one-run).

Neither binary loads a `.env` file: export variables in the shell, the
service unit or the container environment.

---

## Documentation Files

### Core & Infrastructure
- **[core.md](core.md)** — Config file, instance routing, logging
- **[storage.md](storage.md)** — SQLite, PostgreSQL & blob backends (local / s3 / garage)
- **[services.md](services.md)** — none: no Redis or Qdrant integration

### Application Layer
- **[api.md](api.md)** — REST, gRPC & cookie bridge listener ports
- **[workers.md](workers.md)** — Job queue & workers
- **[pipelines.md](pipelines.md)** — Focus profile

### External Integrations
- **[ai-providers.md](ai-providers.md)** — Provider API keys, embedding provider
- **[registries.md](registries.md)** — none: config file + OS keychain

### Local Capture
- **[capture.md](capture.md)** — Clipboard opt-out, browser-history position file

### Security & Development
- **[security.md](security.md)** — Secrets backend, policy rules file, backup encryption
- **[development.md](development.md)** — Debug output, test suites

---

## Quick Reference

| Topic | File | Key Variables |
|-------|------|---------------|
| **Basic Setup** | [core.md](core.md) | `CTXT_CONFIG`, `CTXT_INSTANCE` |
| **Storage** | [storage.md](storage.md) | `CH_STORAGE_TYPE`, `CTXT_BLOB_*`, `PG*` |
| **API** | [api.md](api.md) | `CH_SERVER_PORT`, `CH_GRPC_PORT`, `CH_COOKIE_BRIDGE_PORT` |
| **Workers** | [workers.md](workers.md) | `DPKMS_WORKERS`, `DPKMS_POLL_INTERVAL`, `DPKMS_MAX_RETRIES` |
| **AI** | [ai-providers.md](ai-providers.md) | `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `CTXT_EMBEDDING_*` |
| **Local capture** | [capture.md](capture.md) | `CTXT_NO_CLIPBOARD`, `CTXT_AMBIENT_HISTORY_STATE_FILE` |
| **Security** | [security.md](security.md) | `CTXT_SECRETS_BACKEND`, `CTXT_POLICY_FILE`, `CTXT_BACKUP_PASSPHRASE` |

---

## Common Workflows

### Initial Setup
1. Start with [core.md](core.md) — Point `CTXT_CONFIG` at a config file, if not using the cascade
2. Configure [storage.md](storage.md) — Choose SQLite or PostgreSQL
3. Set up [ai-providers.md](ai-providers.md) — Add API keys

### Scaling Up
1. Review [storage.md](storage.md) — Move to PostgreSQL, externalize blobs to S3
2. Adjust [workers.md](workers.md) — Increase worker count
3. Configure [api.md](api.md) — Set listener ports

### Production Hardening
1. Configure [security.md](security.md) — Pick a secrets backend; inbound auth is config-file only
2. Keep [development.md](development.md) settings out of production — no `--verbose`

---

## Common Patterns

### Local Development

```bash
CH_STORAGE_TYPE=sqlite
DPKMS_WORKERS=2
```

with `storage.path` pointing at the database file (see
[storage.md](storage.md#database-location)).

### Team Deployment

```bash
CH_STORAGE_TYPE=postgres
PGPASSWORD=$(vault kv get -field=password secret/db)
DPKMS_WORKERS=8
```

with `storage.path` set to the PostgreSQL connection string (see
[storage.md](storage.md#postgresql-connection)).

### Production

```bash
CH_STORAGE_TYPE=postgres
DPKMS_WORKERS=16
CTXT_SECRETS_BACKEND=keyring
CTXT_BLOB_BACKEND=s3
```

---

## Security Best Practices

### Never Commit Secrets

**Bad:**
```bash
# committed to git
OPENAI_API_KEY=sk-live-key
```

**Good:** keep the value out of the repository and inject it at run time.

### Use Secret Management

```bash
# AWS Secrets Manager
ANTHROPIC_API_KEY=$(aws secretsmanager get-secret-value \
  --secret-id anthropic_api_key --query SecretString --output text)

# HashiCorp Vault
CTXT_BLOB_S3_SECRET_KEY=$(vault kv get -field=blob_secret secret/ctxt)
```

See [security.md](security.md) for the secrets backends.

---

## Related

- [../manual/reference/config-and-permissions.md](../manual/reference/config-and-permissions.md) — Config reference
- [../security/secret-management.md](../security/secret-management.md) — Secret lifecycle
- [../.env.example](../../.env.example) — Configuration template
