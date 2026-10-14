# Storage Environment Variables

Storage backend (SQLite or PostgreSQL) and the blob store.

Env vars override the config file; a flag or `-c key=value` override wins
over both. An empty env var counts as unset.

---

## Storage Backend Selection

### CH_STORAGE_TYPE
**Type:** string\
**Default:** `sqlite`\
**Values:** `sqlite`, `postgres`\
**Config key:** `storage.type`

Primary storage backend.

```bash
CH_STORAGE_TYPE=postgres
```

---

## Database Location

`storage.path` holds the SQLite database file, or the PostgreSQL
connection string when `storage.type` is `postgres`. Default:
`$XDG_DATA_HOME/contexthelp/db.sqlite` (OS data directory when
`XDG_DATA_HOME` is unset).

Set it in the config file or with `-c`:

```bash
dpkms serve -c storage.path=/data/dpkms.db
```

`CTXT_DATA_DIR` also writes `storage.path`, but `ctxt` and `dpkms` treat
the same value as a directory: they create it at startup and keep pidfiles
in its `run/` subdirectory. It cannot name the database file; use
`storage.path`.

### PostgreSQL connection

No `POSTGRES_*` variables are read by `ctxt` or `dpkms`. Put the connection
string in `storage.path`:

```yaml
storage:
  type: postgres
  path: postgres://contexthelp:secret@db.internal:5432/contexthelp?sslmode=require
```

```bash
CH_STORAGE_TYPE=postgres dpkms serve \
  -c storage.path='postgres://contexthelp@db.internal:5432/contexthelp?sslmode=require'
```

The PostgreSQL driver (`lib/pq`) fills any field the connection string
leaves out from the standard libpq variables: `PGHOST`, `PGPORT`, `PGUSER`,
`PGPASSWORD`, `PGDATABASE`, `PGSSLMODE` and the other `PG*` names libpq
defines. Keep the password out of the config file that way:

```bash
PGPASSWORD=$(vault kv get -field=password secret/db)
```

The connection pool is fixed at 25 open / 5 idle connections.

---

## Blob Storage Configuration

Externalizes oversized object content out of the database into a pluggable blob store. Mirrors the YAML config under `storage.blob` — see [`docs/ctxt/configuration.md`](../ctxt/configuration.md#blob-storage-configuration) for full reference.

### CTXT_BLOB_BACKEND
**Type:** string\
**Default:** `local`\
**Values:** `local`, `s3`, `garage`, `stub`\
**Config key:** `storage.blob.backend`

Selects the blob store implementation. `garage` is a preset on the `s3` backend that auto-applies path-style addressing and a default region — see [Garage example](../ctxt/configuration.md#garage-self-hosted-distributed).

```bash
CTXT_BLOB_BACKEND=garage
```

### CTXT_BLOB_THRESHOLD
**Type:** integer (bytes)\
**Default:** `65536`\
**Config key:** `storage.blob.threshold`

Object content above this size is externalized to the blob store and replaced with a `blob://<sha256>` reference. `0` disables externalization.

```bash
CTXT_BLOB_THRESHOLD=131072
```

### CTXT_BLOB_S3_ENDPOINT
**Type:** string\
**Default:** _(none — AWS default)_\
**Config key:** `storage.blob.s3.endpoint`

Custom S3 endpoint URL. Required for `garage` and self-hosted MinIO; optional for AWS / R2 / B2 / DO Spaces (where the SDK derives the endpoint from `region`).

```bash
CTXT_BLOB_S3_ENDPOINT=http://localhost:3900   # garage
CTXT_BLOB_S3_ENDPOINT=https://<account>.r2.cloudflarestorage.com  # R2
```

### CTXT_BLOB_S3_REGION / CTXT_BLOB_S3_BUCKET / CTXT_BLOB_S3_PREFIX
**Type:** string\
**Config keys:** `storage.blob.s3.region` (default `us-east-1`), `storage.blob.s3.bucket`, `storage.blob.s3.prefix`

Bucket location, name, and key prefix.

### Credentials

`CTXT_BLOB_S3_ACCESS_KEY` / `CTXT_BLOB_S3_SECRET_KEY` (`storage.blob.s3.access_key` / `storage.blob.s3.secret_key`) configure static credentials. Unless both are set, the AWS SDK's default credential chain applies (`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`, `AWS_PROFILE`, SSO, IMDS), so most production deployments leave them empty.

```bash
CTXT_BLOB_S3_ACCESS_KEY=GK024bc9cbfaf57489c5ae2d1f
CTXT_BLOB_S3_SECRET_KEY=$(vault kv get -field=blob_secret secret/ctxt)
```

### Config-file only

No env var for these; set them in the config file or with `-c`:

| Config key | Default | Purpose |
|------------|---------|---------|
| `storage.blob.s3.use_path_style` | `false` (auto-true under `backend: garage`) | Path-style addressing (`endpoint/bucket/key`); required for MinIO and Garage |
| `storage.blob.s3.presign_expiry` | `1h` | TTL for presigned GET URLs |
| `storage.blob.s3.max_retries` | `3` | S3 request retries |
| `storage.blob.local.path` | `<data dir>/blobs` | Directory for the `local` backend |

---

## Examples

### Local Development (SQLite)
```bash
CH_STORAGE_TYPE=sqlite
dpkms serve -c storage.path=./data/dev.db
```

### Team Deployment (PostgreSQL)
```bash
CH_STORAGE_TYPE=postgres
PGPASSWORD=$(vault kv get -field=password secret/db)
dpkms serve -c storage.path='postgres://contexthelp@db-staging.internal:5432/contexthelp_staging?sslmode=disable'
```

### S3-compatible blob store (Garage)
```bash
CTXT_BLOB_BACKEND=garage
CTXT_BLOB_S3_ENDPOINT=http://localhost:3900
CTXT_BLOB_S3_BUCKET=ctxt
CTXT_BLOB_S3_ACCESS_KEY=GK024bc9cbfaf57489c5ae2d1f
CTXT_BLOB_S3_SECRET_KEY=$(vault kv get -field=blob_secret secret/ctxt)
```

---

## Related

- [../manual/reference/config-and-permissions.md](../manual/reference/config-and-permissions.md) — Config reference
- [../scaling.md](../scaling.md#storage-scaling) — Storage scaling strategies
- [../dpkms/storage.md](../dpkms/storage.md) — Storage architecture
