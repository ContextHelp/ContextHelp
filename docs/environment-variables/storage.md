# Storage Environment Variables

Storage backend configuration for SQLite and PostgreSQL.

---

## Storage Backend Selection

### STORAGE_BACKEND
**Type:** string  
**Default:** `sqlite`  
**Values:** `sqlite`, `postgres`

Primary storage backend.

```bash
STORAGE_BACKEND=postgres
```

---

## SQLite Configuration

### SQLITE_PATH
**Type:** string  
**Default:** `./data/sqlite/contexthelp.db`

Path to SQLite database file.

```bash
SQLITE_PATH=~/.local/share/contexthelp/data.db
```

### SQLITE_WAL_MODE
**Type:** boolean  
**Default:** `true`

Enable WAL (Write-Ahead Logging) mode for better concurrency.

```bash
SQLITE_WAL_MODE=true
```

### SQLITE_BUSY_TIMEOUT
**Type:** integer (milliseconds)  
**Default:** `5000`

Timeout when database is locked.

```bash
SQLITE_BUSY_TIMEOUT=10000
```

---

## PostgreSQL Configuration

### POSTGRES_HOST
**Type:** string  
**Default:** `localhost`

PostgreSQL server hostname.

```bash
POSTGRES_HOST=db.internal.example.com
```

### POSTGRES_PORT
**Type:** integer  
**Default:** `5432`

PostgreSQL server port.

```bash
POSTGRES_PORT=5432
```

### POSTGRES_USER
**Type:** string  
**Default:** `contexthelp`

Database username.

```bash
POSTGRES_USER=contexthelp
```

### POSTGRES_PASSWORD
**Type:** string (secret)  
**Default:** _(none)_  
**Required:** Yes (for PostgreSQL)

Database password. **Never commit to version control.**

```bash
POSTGRES_PASSWORD=secure_password_here
```

**Security:** Use secret management in production:
```bash
# AWS Secrets Manager
POSTGRES_PASSWORD=$(aws secretsmanager get-secret-value \
  --secret-id db_password --query SecretString --output text)
```

### POSTGRES_DB
**Type:** string  
**Default:** `contexthelp`

Database name.

```bash
POSTGRES_DB=contexthelp_production
```

### POSTGRES_SSL_MODE
**Type:** string  
**Default:** `disable`  
**Values:** `disable`, `require`, `verify-ca`, `verify-full`

SSL connection mode.

```bash
POSTGRES_SSL_MODE=require
```

### POSTGRES_MAX_CONNECTIONS
**Type:** integer  
**Default:** `25`

Maximum connection pool size.

```bash
POSTGRES_MAX_CONNECTIONS=50
```

### POSTGRES_MAX_IDLE_CONNECTIONS
**Type:** integer  
**Default:** `5`

Maximum idle connections to keep open.

```bash
POSTGRES_MAX_IDLE_CONNECTIONS=10
```

---

## Examples

### Local Development (SQLite)
```bash
STORAGE_BACKEND=sqlite
SQLITE_PATH=./data/sqlite/dev.db
SQLITE_WAL_MODE=true
SQLITE_BUSY_TIMEOUT=5000
```

### Team Deployment (PostgreSQL)
```bash
STORAGE_BACKEND=postgres
POSTGRES_HOST=db-staging.internal
POSTGRES_PORT=5432
POSTGRES_USER=contexthelp
POSTGRES_PASSWORD=secure_password
POSTGRES_DB=contexthelp_staging
POSTGRES_SSL_MODE=disable
POSTGRES_MAX_CONNECTIONS=50
```

### Production (PostgreSQL with SSL)
```bash
STORAGE_BACKEND=postgres
POSTGRES_HOST=db-primary.internal
POSTGRES_PORT=5432
POSTGRES_USER=contexthelp
POSTGRES_PASSWORD=$(vault kv get -field=password secret/db)
POSTGRES_DB=contexthelp
POSTGRES_SSL_MODE=require
POSTGRES_MAX_CONNECTIONS=100
POSTGRES_MAX_IDLE_CONNECTIONS=20
```

---

## Migration Guide

### SQLite → PostgreSQL

1. Export data:
```bash
./bin/dpkms export --format=json --output=backup.json
```

2. Update environment:
```bash
STORAGE_BACKEND=postgres
POSTGRES_HOST=localhost
POSTGRES_PASSWORD=secure_password
```

3. Import data:
```bash
./bin/dpkms import --format=json --input=backup.json
```

---

## Related

- [../scaling.md](../scaling.md#storage-scaling) — Storage scaling strategies
- [../dpkms/storage.md](../dpkms/storage.md) — Storage architecture
