# CLI Flags, Environment Variables and Config Precedence

How `ctxt` and `dpkms` resolve a setting that can come from a config file,
an environment variable, a `-c` override or a command flag.

Source of truth: `internal/config/config.go` (`LoadWithOverrides`,
`envBindings`) on top of kit's layered loader (`hop.top/kit/go/core/config`).

---

## Table of Contents

1. [Precedence](#precedence)
2. [Config Files](#config-files)
3. [The `-c` / `--config` Flag](#the--c----config-flag)
4. [Environment Variables Bound to Config Keys](#environment-variables-bound-to-config-keys)
5. [Other Environment Variables Read at Startup](#other-environment-variables-read-at-startup)
6. [Flags That Override Config](#flags-that-override-config)
7. [Profiles](#profiles)
8. [Examples](#examples)
9. [Debugging Resolution](#debugging-resolution)

---

## Precedence

Highest wins:

| # | Layer | Example |
|---|-------|---------|
| 1 | Command flag, when passed (only on commands that read it over config) | `dpkms serve --port 8181` |
| 2 | `-c key=value` overrides | `dpkms serve -c server.port=8181` |
| 3 | Bound environment variables | `CH_SERVER_PORT=8181` |
| 4 | `-c <path>` files, later paths win | `-c ./extra.yaml` |
| 5 | Project file | `.contexthelp/dpkms.yaml` |
| 6 | User file | `$XDG_CONFIG_HOME/contexthelp/dpkms.yaml` |
| 7 | System file | `/etc/contexthelp/dpkms.yaml` |
| 8 | Built-in defaults | `server.port` = `8080` |

Notes:

- Env sits **above every file**, including `-c <path>` files. A file with
  `server.port: 4242` does not beat `CH_SERVER_PORT=9999`.
- `-c key=value` sits **above env**.
- A flag beats config only where the command is written to prefer it
  (see [Flags That Override Config](#flags-that-override-config)). Most
  flags are per-invocation options with no config key behind them.
- Files merge as YAML: scalars and lists are replaced, maps are merged.

---

## Config Files

Each binary reads its own file name in the shared `contexthelp/`
namespace: `ctxt` reads `ctxt.yaml`, `dpkms` reads `dpkms.yaml`.

| Slot | Path | Missing file |
|------|------|--------------|
| System | `/etc/contexthelp/<bin>.yaml` | skipped |
| User | `$XDG_CONFIG_HOME/contexthelp/<bin>.yaml` (`~/.config/...` by default) | skipped |
| Project | nearest `.contexthelp/<bin>.yaml`, walking up from the working directory, stopping at `$HOME` or `/` | skipped |
| `-c <path>` | each path given, in order | error |

`CTXT_CONFIG=<path>` replaces the system, user and project slots with that
single file. It is tolerated when missing (first run, before `ctxt setup`
creates it). `-c <path>` files still layer on top of it.

---

## The `-c` / `--config` Flag

Global on both binaries, repeatable. Each token is one of:

- **`key=value`**: overrides one config key. Dotted keys address nested
  fields; the value parses as YAML, falling back to a literal string.
- **a bare path**: loads one more config file after the discovered ones.
  The file must exist.

```bash
ctxt -c ./work.yaml find "onboarding"
dpkms serve -c server.port=8181 -c jobs.max_retries=5
dpkms housekeeping prune -c /srv/dpkms/dpkms.yaml
```

An invalid `-c` token, or a file that fails to load, aborts the command.
There is no fallback to the default config.

---

## Environment Variables Bound to Config Keys

Only the variables below map to config keys. There is no automatic
prefix-to-key translation: an unlisted `CH_<SOMETHING>` is not read as
config. An empty value is ignored (the lower layer stays in effect).

| Env var | Config key | Default |
|---------|------------|---------|
| `CH_STORAGE_TYPE` | `storage.type` | `sqlite` |
| `CTXT_DATA_DIR` | `storage.data_dir` | `$XDG_DATA_HOME/contexthelp` |
| `CTXT_BLOB_BACKEND` | `storage.blob.backend` | `local` |
| `CTXT_BLOB_THRESHOLD` | `storage.blob.threshold` | `65536` |
| `CTXT_BLOB_S3_ENDPOINT` | `storage.blob.s3.endpoint` | _(empty)_ |
| `CTXT_BLOB_S3_REGION` | `storage.blob.s3.region` | `us-east-1` |
| `CTXT_BLOB_S3_BUCKET` | `storage.blob.s3.bucket` | _(empty)_ |
| `CTXT_BLOB_S3_PREFIX` | `storage.blob.s3.prefix` | _(empty)_ |
| `CTXT_BLOB_S3_ACCESS_KEY` | `storage.blob.s3.access_key` | _(empty)_ |
| `CTXT_BLOB_S3_SECRET_KEY` | `storage.blob.s3.secret_key` | _(empty)_ |
| `CH_SERVER_PORT` | `server.port` | `8080` |
| `CH_GRPC_PORT` | `server.grpc_port` | `9090` |
| `CH_COOKIE_BRIDGE_PORT` | `server.cookie_bridge_port` | `9377` |
| `CH_PUBLIC` | `server.public` | `false` |
| `CTXT_PROFILE` | `profile.default` | _(empty)_ |
| `DPKMS_POLL_INTERVAL` | `jobs.poll_interval` | `500ms` |
| `DPKMS_STALE_TIMEOUT` | `jobs.stale_timeout` | `30m` |
| `DPKMS_MAX_RETRIES` | `jobs.max_retries` | `3` |
| `DPKMS_MAX_HOPS` | `jobs.max_hops` | `5` |
| `CTXT_SECRETS_BACKEND` | `secrets.backend` | `env` |
| `CTXT_AGE_FILE` | `secrets.age_file` | _(empty)_ |
| `CTXT_AGE_IDENTITY` | `secrets.age_identity_file` | _(empty)_ |
| `CTXT_BROWSER_ENABLED` | `browser.enabled` | `false` |
| `CTXT_BROWSER_BINARY` | `browser.binary` | `ibr` |
| `CTXT_BROWSER_PORT` | `browser.port` | `0` |

`CTXT_DATA_DIR` is a directory. It is the base for runtime files
(`<data dir>/run/`: pidfiles and the `ctxt instance use` selection) and
supplies the defaults `storage.path` = `<data dir>/db.sqlite` and
`storage.blob.local.path` = `<data dir>/blobs`. An explicit
`storage.path` or `storage.blob.local.path` from any layer is kept. Both
binaries create the data directory at startup. `storage.path` has no
environment variable.

`server.public` / `CH_PUBLIC` selects public access only while
`server.access` is unset; an explicit `server.access` wins.

---

## Other Environment Variables Read at Startup

These are not config keys and do not take part in the layering above.

| Env var | Effect |
|---------|--------|
| `CTXT_CONFIG` | Replaces the system/user/project file cascade with one file (see [Config Files](#config-files)) |
| `CTXT_INSTANCE` | `ctxt` instance selection, below `--server` and `--instance` (see [Instance selection](#instance-selection-ctxt)) |
| `CH_DISABLE_TELEMETRY` | `true`, `1` or `yes` forces telemetry off, whatever `privacy.telemetry` says |

The full list of variables the binaries read lives in
[../environment-variables/README.md](../environment-variables/README.md).

---

## Flags That Override Config

### `dpkms serve` listener ports

| Flag | Config key | Env var | Default |
|------|------------|---------|---------|
| `--port` | `server.port` | `CH_SERVER_PORT` | `8080` |
| `--grpc-port` | `server.grpc_port` | `CH_GRPC_PORT` | `9090` |
| `--cookie-bridge-port` | `server.cookie_bridge_port` | `CH_COOKIE_BRIDGE_PORT` | `9377` |

Each port is the flag when passed, else the config value resolved through
the [precedence](#precedence) table. A port set by any layer (flag, `-c`,
env or file) is explicit: serve binds exactly that port or fails. Only an
untouched built-in default falls back to another port when busy. Port `0`
binds an OS-assigned port. The cookie bridge always binds `127.0.0.1`.

### `dpkms --data-dir`

`--data-dir <dir>` is a `dpkms` global for `storage.data_dir`. When
passed it wins over `-c storage.data_dir=...`, `CTXT_DATA_DIR` and every
config file; the run dir and the `storage.path` / `storage.blob.local.path`
defaults follow it. `ctxt` has no `--data-dir`: use `CTXT_DATA_DIR` or
`storage.data_dir`.

### `dpkms serve --public`

`--public` is OR-ed with `server.public` (`CH_PUBLIC`): either one selects
public access when `server.access` is unset. `--public=false` cannot turn
off a `server.public: true` from config. A non-private instance needs
inbound auth (`server.auth`) or serve refuses to start.

### Instance selection (`ctxt`)

Commands that talk to dpkms pick one endpoint, first match wins:

1. `--server <url>`
2. `--instance <name|port>`
3. `CTXT_INSTANCE`
4. the selection saved by `ctxt instance use`
5. first entry of `server.urls`
6. `server.url`
7. `http://127.0.0.1:8080`

`dpkms` client commands take `--server-url`, which overrides
`server.url` and `server.urls`.

### Global flags

Both binaries inherit kit's global flags (`-c/--config`, `--format`,
`-o/--output`, `--quiet`, `--no-color`, `-V/--verbose`, `-C/--chdir`,
`--dry-run`, `--confirm`, `--offline`, ...). Apart from `-c`, these are
per-invocation options with no config key behind them. `ctxt` adds
`--profile` and `--instance`; `dpkms` adds `--data-dir`. Run
`ctxt --help-all` or `dpkms --help-all` for the full list.

`--offline` is enforced by kit: requests through `http.DefaultTransport`
made on the command's context fail with `network disabled by --offline`
unless they target loopback. It has no env var or config key.

---

## Profiles

Focus profiles live under `profile` in the config file:

```yaml
# ~/.config/contexthelp/ctxt.yaml
profile:
  default: engineer          # CTXT_PROFILE overrides this
  profiles:
    engineer:
      description: Engineering focus
      tags: ["technical", "code"]
      mention_namespaces: ["repo"]
    founder:
      description: Business focus
      tags: ["business", "growth"]
```

- `profile.default` names the default profile. `CTXT_PROFILE` and
  `-c profile.default=<name>` override it like any bound key.
- A profile entry may carry `default: true` instead. At most one may;
  when `profile.default` is empty it is filled from that entry, and when
  both are set they must agree or loading fails.
- `--profile <name>` selects a profile for one invocation on commands that
  read it.
- `ctxt profile set <name>` / `ctxt profile unset <name>` change the
  default; `ctxt profile list` / `ctxt profile view <name>` inspect them.

---

## Examples

### Listener port through every layer

```yaml
# ~/.config/contexthelp/dpkms.yaml
server:
  port: 8181
```

| Command | HTTP port |
|---------|-----------|
| `dpkms serve` | `8181` (user file) |
| `CH_SERVER_PORT=8282 dpkms serve` | `8282` (env beats file) |
| `CH_SERVER_PORT=8282 dpkms serve -c server.port=8383` | `8383` (`-c` beats env) |
| `CH_SERVER_PORT=8282 dpkms serve -c server.port=8383 --port 8484` | `8484` (flag beats all) |

With none of these set, serve uses `8080` and, if busy, falls back to
another port.

### Env beats an explicit `-c` file

```bash
# extra.yaml sets storage.type: postgres
CH_STORAGE_TYPE=sqlite dpkms serve -c ./extra.yaml
# → storage.type = sqlite
```

### `CTXT_CONFIG` vs `-c`

```bash
CTXT_CONFIG=~/alt/ctxt.yaml ctxt -c jobs.max_hops=8 find "query"
# Files: only ~/alt/ctxt.yaml (system/user/project skipped)
# jobs.max_hops = 8 from -c, whatever the file says
```

---

## Debugging Resolution

```bash
# Which files are in play, in order
ctxt config paths
dpkms config paths

# Highest-precedence existing file
ctxt config path

# Merged configuration ctxt sees (files + env + -c)
ctxt config show --format json

# Validate the merged configuration
ctxt config validate

# Bound variables currently set
env | grep -E '^(CH|CTXT|DPKMS)_'
```

### Related Documentation

- [configuration.md](./configuration.md) - Configuration reference
- [api-cli.md](./api-cli.md) - CLI command reference
- [../environment-variables/README.md](../environment-variables/README.md) - All environment variables
- [../configuration-structure.md](../configuration-structure.md) - Config file organization
