# Configuration

`ctxt` and `dpkms` read YAML config files that share one schema: the
`Config` type in `internal/config/config.go`. Loading a config never writes
a file. This page covers the layers, the template, and the sections that
need more than a one-line comment. File locations, which binary reads which
key, and the full key list: [../configuration-structure.md](../configuration-structure.md).

---

# Configuration Layers

Each binary reads its own file: `ctxt` reads `ctxt.yaml`, `dpkms` reads
`dpkms.yaml`. Lowest to highest:

1. **Defaults** (compiled into the binary)
2. **System config** (`/etc/contexthelp/<bin>.yaml`)
3. **User config** (`$XDG_CONFIG_HOME/contexthelp/<bin>.yaml`)
4. **Project config** (nearest `.contexthelp/<bin>.yaml`)
5. **`-c <path>` files**
6. **Bound environment variables** (e.g., `CH_STORAGE_TYPE=sqlite`)
7. **`-c key=value` overrides**
8. **Command flags**, on commands that prefer them (e.g., `dpkms serve --port 8181`)

`CTXT_CONFIG=<path>` replaces layers 2–4 with that one file. Values are
literal: `${VAR}` is not expanded. Full detail:
[cli-flag-binding.md](cli-flag-binding.md).

Only the variables listed in
[cli-flag-binding.md](cli-flag-binding.md#environment-variables-bound-to-config-keys)
map to config keys; every env var is in
[../environment-variables/README.md](../environment-variables/README.md).
Registry tokens are not environment variables: `ctxt registry login <name>`
stores them in the OS keychain.

Keys the schema does not declare are ignored on load, so a misspelled key
leaves its setting at the default. `ctxt config validate` lists them as
warnings.

---

# Template

[`config/config.example.yaml`](../../config/config.example.yaml) lists the
keys either binary acts on, each commented out with its default and, for
enum keys, the accepted values. Copy it to `ctxt.yaml`, `dpkms.yaml` or
both, and uncomment what you need:

```bash
cp config/config.example.yaml ~/.config/contexthelp/dpkms.yaml
$EDITOR ~/.config/contexthelp/dpkms.yaml
CTXT_CONFIG=~/.config/contexthelp/dpkms.yaml ctxt config validate
```

[`config/config.minimal.yaml`](../../config/config.minimal.yaml) holds the
few settings people change first. A test decodes every `config/*.yaml`
strictly and validates it, so the templates hold only real keys.

Declared but not read by any command: `plugins`, `retrieval`,
`conventions`, `inbox`, `audit`, and `i18n` (printed by `ctxt config show`,
used nowhere else). Setting them has no effect.

---

# Storage Configuration

```yaml
storage:
  type: sqlite                          # sqlite | postgres
  data_dir: /srv/contexthelp            # default: $XDG_DATA_HOME/contexthelp
  path: /srv/contexthelp/db.sqlite      # default: <data_dir>/db.sqlite
```

- `type`: `sqlite` (default) or `postgres`. There is no other backend.
- `data_dir`: holds `run/` (pidfiles, `ctxt instance use` selection,
  upgrade state) and the default database and blob paths. Set it with
  `CTXT_DATA_DIR` or `dpkms --data-dir` too. `ctxt` finds local instances
  in its own `<data_dir>/run`, so `ctxt` and `dpkms serve` must share it.
- `path`: the SQLite file, or the PostgreSQL connection string.

Paths are used as written; `~` is not expanded.

---

# Blob Storage Configuration

Externalizes oversized object content out of the database into a blob
store. Above the threshold, the object's raw content is replaced with a
`blob://<hash>` reference and read back on demand; at or below it,
content stays inline.

The keys live under `storage.blob`. `dpkms serve` and every command that
opens the store use this blob store. With nothing configured it is the
`local` backend in `blobs/` under the data directory. An unknown backend,
or `local` with an empty `local.path`, stops the command at startup.

```yaml
storage:
  blob:
    backend: local                 # local | s3 | garage
    threshold: 65536               # bytes; 0 disables externalization
    local:
      path: /srv/contexthelp/blobs # default: <data_dir>/blobs
    s3:
      endpoint: ""                 # custom endpoint for non-AWS providers
      region: us-east-1
      bucket: ""                   # required for s3 and garage
      prefix: ""                   # key prefix within the bucket
      use_path_style: false        # true for MinIO and many self-hosted stores
      presign_expiry: 1h
      max_retries: 3
```

## Backends

| Backend | When to use |
|---------|-------------|
| `local` | Default. Single machine; sharded directory under `local.path`. |
| `s3` | AWS S3 or any S3-compatible store (R2, B2, Spaces, MinIO, ...), with an optional custom `endpoint`. |
| `garage` | [Garage](https://garagehq.deuxfleurs.fr), a self-hosted S3-compatible store. Sets `use_path_style: true` and, when `region` is empty, `region: garage`. Requires `s3.endpoint`. |

## Credentials

Config files do not expand `${VAR}`: `s3.access_key` and `s3.secret_key`
would be the literal credentials. Leave both unset and export
`CTXT_BLOB_S3_ACCESS_KEY` and `CTXT_BLOB_S3_SECRET_KEY`, which override the
file. Container deployments: [../deployment/docker.md#blob-storage-and-providers](../deployment/docker.md#blob-storage-and-providers).

```yaml
storage:
  blob:
    backend: s3
    s3:
      endpoint: http://minio:9000
      bucket: ctxt-blobs
      use_path_style: true
```

To stand up a single-node Garage for development, see
[`test/integration/blob_garage_test.go`](../../test/integration/blob_garage_test.go),
which embeds a complete `garage.toml` and bootstrap sequence.

## Key layout

`{prefix}/{hash[0:2]}/{hash[2:4]}/{hash}`, with a `.meta.json` sidecar per
blob holding its content type, size and content hash. The hash covers the
content and its source.

## Behavior at the threshold

- Content of `threshold` bytes or less is stored inline.
- Larger content goes to the blob store (idempotent: the same hash is
  stored once); the object keeps `blob://<hash>` and the metadata keys
  `blob_key`, `blob_original_size`, `blob_content_type`.
- `threshold: 0` disables externalization: all content stays in the
  database.

Reads (API, MCP, direct store) return the content, not the reference: a
single object whole, list and search results at most `threshold` bytes
with `metadata.content_truncated` set.

---

# Server Configuration

`dpkms serve` settings (listener ports, `workers`, `access`, `auth`,
`allowed_hosts`, `ui.session`, `quotas`) are covered in
[../manual/reference/config-and-permissions.md](../manual/reference/config-and-permissions.md#server-settings).
A `protected` or `public` instance needs `server.auth`, or serve refuses to
start.

## Client endpoints (ctxt)

`ctxt` talks to exactly one dpkms instance per command. `server.urls`
lists the instances it knows; `server.token` is the default bearer token.

```yaml
server:
  token: <default token>
  urls:
    - name: home                         # optional, unique; select with --instance home
      url: https://dpkms.example.ts.net:8080
      token: <token for home>            # overrides server.token for this entry
    - http://127.0.0.1:8080              # bare URL: no name, no own token
```

Without `--server`, `--instance`, `CTXT_INSTANCE` or `ctxt instance use`,
commands use the first entry and never fall back to another. See
[`ctxt instance`](api-cli.md#ctxt-instance) for the resolution order.
Tokens come only from config, never from flags.

---

# Federation Configuration

Push replication between dPKMS instances.

### Outbound peers (`federations`)

Each entry describes one remote dPKMS instance to push to.

```yaml
federations:
  - name: home-server
    url: https://home.example.com:8080
    token: <home-server-token>   # literal; keep this file 0600
    sync_mode: async
    interval: 5m

  - name: local-replica
    url: file:///mnt/backup/dpkms.db
    sync_mode: inline
    blob:                        # optional; file targets only
      backend: local
      local:
        path: /mnt/backup/blobs
```

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique local identifier |
| `url` | string | Remote HTTP endpoint or `file://` path |
| `token` | string | Bearer token sent to the remote instance, written literally (config files do not expand `${VAR}`) |
| `sync_mode` | string | `async` (scheduled) or `inline` (immediate) |
| `interval` | duration | How often to sync; required for `async` |
| `blob` | object | File targets only: the target instance's blob store, same keys as `storage.blob` (`backend`, `threshold`, `local.path`, `s3.*`). A local push copies each pushed object's blob into it. Unset, or `local` without `local.path`: `blobs/` beside the target database. `ctxt config validate` checks it like `storage.blob`; set on an `http(s)` target it is an error |

Each `async` tick pushes only the objects changed since the target's
watermark: the change sequence of the last object the target
acknowledged. The store stamps every object insert and update with the
next change sequence, so an import, a restore or an object a receiver
forwards on is pushed whatever `updated_at` it carries. A `file://` target keeps its watermark in its own database,
an `http(s)` target's watermark lives in the source's, both under `name`.
Objects go 100 per batch; to an `http(s)` target, in requests of at most
4 MiB of objects, content inline. A refused batch leaves the watermark
where it was and is re-sent on the next tick. Renaming an entry starts its
watermark over.

### Inbound auth (`federation`)

```yaml
federation:
  token: <accept-token>        # literal; keep this file 0600
```

Set `token` to require a Bearer token on incoming push requests.
Empty = accept any token (use only in trusted network environments).

---

# Validation Tools

```bash
ctxt config paths              # files ctxt reads, in order
ctxt config show               # merged config (files + env + -c)
ctxt config validate           # every validation error, unknown-key warnings, secret scan
ctxt config doctor             # schema, secrets, permissions, deprecated keys
```

`ctxt config validate` checks ports, `server.access` and auth roles and
tokens, quotas, `storage.blob`, `duplicates`, `federations`, `embeddings`,
`secrets.backend`, registry auth, and rejects `${VAR}` values; it exits 1
on any error. `ctxt config` commands read `ctxt.yaml`; point `CTXT_CONFIG`
at `dpkms.yaml` to check it. `dpkms config` offers only `path` and `paths`.
