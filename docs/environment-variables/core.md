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
