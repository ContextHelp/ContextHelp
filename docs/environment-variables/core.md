# Core Environment Variables

Basic runtime and logging configuration.

---

## Environment

### ENV
**Type:** string
**Default:** `development`
**Values:** `development`, `staging`, `production`

Sets the runtime environment.

```bash
ENV=production
```

### DEV_MODE
**Type:** boolean
**Default:** `false`
**Values:** `true`, `false`

Enables development features (verbose logging, profiling, etc.).

```bash
DEV_MODE=true
```

---

## Instance Routing

### CTXT_INSTANCE
**Type:** string
**Default:** *(none)*

Selects the dpkms instance `ctxt` talks to: a named `server.urls` entry,
or a running local instance by name or port. Same step as the
`--instance` flag, which wins over it; both win over the state file
written by `ctxt instance use`. An unknown name exits 70.

```bash
CTXT_INSTANCE=home ctxt status
CTXT_INSTANCE=8081 ctxt log
```

Use `ctxt instance use <name>` for a persistent selection instead. See
[`ctxt instance`](../ctxt/api-cli.md#ctxt-instance) for the full order.

---

## Logging

### LOG_LEVEL
**Type:** string
**Default:** `info`
**Values:** `debug`, `info`, `warn`, `error`

Minimum log level to output.

```bash
LOG_LEVEL=debug
```

### LOG_FORMAT
**Type:** string
**Default:** `console`
**Values:** `console`, `json`

Log output format.

```bash
LOG_FORMAT=json  # For production
```

### LOG_OUTPUT
**Type:** string
**Default:** `stdout`
**Values:** `stdout`, `stderr`, `file`

Where to write logs.

```bash
LOG_OUTPUT=file
```

### LOG_FILE
**Type:** string
**Default:** `./data/logs/contexthelp.log`

Log file path (when `LOG_OUTPUT=file`).

```bash
LOG_FILE=/var/log/contexthelp/app.log
```

---

---

## Multi-Instance Targeting

### CTXT_INSTANCE
**Type:** string
**Default:** _(none — uses the saved selection, else `server.urls` / `server.url`)_

Selects which dpkms instance `ctxt` commands talk to: a named
`server.urls` entry or a running local instance.
Precedence: `--server <url>` > `--instance <name>` flag > `CTXT_INSTANCE` env var > `ctxt instance use` saved state > first `server.urls` entry > `server.url` > `http://127.0.0.1:8080`.

```bash
CTXT_INSTANCE=home ctxt status
CTXT_INSTANCE=personal ctxt log
```

See also: `ctxt instance use`, `ctxt instance list`, `ctxt instance current`.

---

## Examples

### Development
```bash
ENV=development
DEV_MODE=true
LOG_LEVEL=debug
LOG_FORMAT=console
LOG_OUTPUT=stdout
```

### Production
```bash
ENV=production
DEV_MODE=false
LOG_LEVEL=warn
LOG_FORMAT=json
LOG_OUTPUT=file
LOG_FILE=/var/log/contexthelp/app.log
```
