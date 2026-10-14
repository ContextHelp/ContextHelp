# Core Environment Variables

Config file, instance routing and logging.

---

## Config File

### CTXT_CONFIG
**Type:** string (path)\
**Default:** _(none — walks the config cascade)_

Loads this one file in place of the system → user → project cascade
(`/etc/contexthelp/<bin>.yaml`, `$XDG_CONFIG_HOME/contexthelp/<bin>.yaml`,
nearest `.contexthelp/<bin>.yaml`). Read by both `ctxt` and `dpkms`. A
missing file is not an error; `ctxt setup` creates it. `-c <file>` still
layers on top.

```bash
CTXT_CONFIG=~/work/contexthelp/ctxt.yaml ctxt status
```

The database location is `storage.path` — see
[storage.md](storage.md#database-location).

---

## Environment

### DEV_MODE
**Type:** boolean\
**Default:** `false`\
**Values:** `true`, `false`

Enables development features (verbose logging, profiling, etc.).

```bash
DEV_MODE=true
```

---

## Instance Routing

### CTXT_INSTANCE
**Type:** string\
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

No env vars. Logs go to stderr at `warn` level; `--verbose` switches to
debug output, on both `ctxt` and `dpkms`.

```bash
dpkms serve --verbose
```

---

## Examples

### Development
```bash
DEV_MODE=true
CTXT_CONFIG=./dev/dpkms.yaml
```

### Production
```bash
DEV_MODE=false
CTXT_CONFIG=/etc/contexthelp/prod/dpkms.yaml
```
