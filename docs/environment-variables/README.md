# Environment Variables Documentation

**Version:** 0.1.0

This directory contains focused documentation for environment variable configuration, organized by topic.

---

## Overview

This system uses environment variables for configuration with the following precedence (later overrides earlier):

1. **Built-in defaults** (in code)
2. **Config file** (`config.yaml`)
3. **Environment file** (`.env`)
4. **Environment variables** (shell export)
5. **Command-line flags** (highest priority)

---

## Quick Start

```bash
# Copy example file
cp .env.example .env

# Edit configuration
vim .env

# Load environment
export $(cat .env | xargs)
```

---

## Documentation Files

### Core & Infrastructure
- **[core.md](core.md)** — Environment, logging (99 lines)
- **[storage.md](storage.md)** — SQLite & PostgreSQL (212 lines)
- **[services.md](services.md)** — Redis & Qdrant (178 lines)

### Application Layer
- **[api.md](api.md)** — REST & gRPC API (144 lines)
- **[workers.md](workers.md)** — Job queue & workers (110 lines)
- **[pipelines.md](pipelines.md)** — Pipelines & profiles (99 lines)

### External Integrations
- **[ai-providers.md](ai-providers.md)** — OpenAI, Anthropic, Ollama (202 lines)
- **[registries.md](registries.md)** — Registry sync (95 lines)

### Security & Development
- **[security.md](security.md)** — Encryption, auth, secrets (227 lines)
- **[development.md](development.md)** — Profiling, debugging, testing (264 lines)

**Total:** 10 focused files, 1,630 lines

---

## Quick Reference

| Topic | File | Key Variables |
|-------|------|---------------|
| **Basic Setup** | [core.md](core.md) | `ENV`, `LOG_LEVEL`, `LOG_FORMAT` |
| **Storage** | [storage.md](storage.md) | `STORAGE_BACKEND`, `SQLITE_PATH`, `POSTGRES_*` |
| **API** | [api.md](api.md) | `API_HOST`, `API_PORT`, `GRPC_PORT` |
| **Workers** | [workers.md](workers.md) | `WORKER_COUNT`, `JOB_TIMEOUT` |
| **AI** | [ai-providers.md](ai-providers.md) | `OPENAI_API_KEY`, `ANTHROPIC_API_KEY` |
| **Security** | [security.md](security.md) | `ENCRYPTION_ENABLED`, `JWT_SECRET` |

---

## Common Workflows

### Initial Setup
1. Start with [core.md](core.md) — Set `ENV` and logging
2. Configure [storage.md](storage.md) — Choose SQLite or PostgreSQL
3. Set up [ai-providers.md](ai-providers.md) — Add API keys

### Scaling Up
1. Review [storage.md](storage.md) — Migrate to PostgreSQL
2. Enable [services.md](services.md) — Add Redis and Qdrant
3. Adjust [workers.md](workers.md) — Increase worker count
4. Configure [api.md](api.md) — Set up CORS and timeouts

### Production Hardening
1. Enable [security.md](security.md) — Turn on encryption and auth
2. Optimize [development.md](development.md) — Enable metrics, disable debug
3. Configure [registries.md](registries.md) — Set auth tokens

---

## Common Patterns

### Local Development

```bash
ENV=development
LOG_LEVEL=debug
STORAGE_BACKEND=sqlite
SQLITE_PATH=./data/sqlite/dev.db
WORKER_COUNT=2
```

### Team Deployment

```bash
ENV=staging
STORAGE_BACKEND=postgres
POSTGRES_HOST=db-staging.internal
WORKER_COUNT=8
REDIS_ENABLED=true
```

### Production

```bash
ENV=production
LOG_FORMAT=json
STORAGE_BACKEND=postgres
POSTGRES_SSL_MODE=require
WORKER_COUNT=16
AUTH_ENABLED=true
ENCRYPTION_ENABLED=true
```

---

## Security Best Practices

### Never Commit Secrets

**Bad:**
```bash
# .env (committed to git)
POSTGRES_PASSWORD=my_password
```

**Good:**
```bash
# .env.example (template, committed)
POSTGRES_PASSWORD=

# .env (actual values, in .gitignore)
POSTGRES_PASSWORD=actual_secure_password
```

### Use Secret Management

```bash
# AWS Secrets Manager
POSTGRES_PASSWORD=$(aws secretsmanager get-secret-value \
  --secret-id db_password --query SecretString --output text)

# HashiCorp Vault
POSTGRES_PASSWORD=$(vault kv get -field=password secret/db)
```

### Validate Required Variables

```bash
required_vars=("POSTGRES_PASSWORD" "JWT_SECRET")
for var in "${required_vars[@]}"; do
  if [ -z "${!var}" ]; then
    echo "Error: $var is not set"
    exit 1
  fi
done
```

---

## Security Note

**Never commit secrets to version control.** See [security.md](security.md) for secret management best practices.

---

## Related

- [../environment-variables.md](../environment-variables.md) — Overview and examples
- [../security/secret-management.md](../security/secret-management.md) — Secret lifecycle
- [../.env.example](../../.env.example) — Configuration template
