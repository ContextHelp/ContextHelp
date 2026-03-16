# Version Output Convention

All ecosystem CLI tools (`ctxt`, `dpkms`, `exo`, `xray`) follow this convention.

## Flag

- Short: `-v`
- Long: `--version`
- Location: root command flag (not a subcommand)

## Text Format

```
<binary> version <semver> (<YYYY-MM-DD>)
```

Examples:

```
ctxt version 0.1.0-dirty (2026-03-16)
dpkms version 0.1.0-dirty (2026-03-16)
```

## JSON Format (`--output json`)

```json
{"name":"ctxt","version":"0.1.0-dirty","date":"2026-03-16","git_commit":"1b3ec3e"}
```

Fields:

| Field | Source |
|---|---|
| `name` | binary name |
| `version` | `git describe --tags --always --dirty` (via `VERSION` ldflag) |
| `date` | date portion of `BUILD_TIME` ldflag (`YYYY-MM-DD`) |
| `git_commit` | `git rev-parse --short HEAD` (via `GIT_COMMIT` ldflag) |

## Build Injection

Version variables are set via ldflags in the Makefile:

```makefile
VERSION    := $(shell git describe --tags --always --dirty)
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD)

LDFLAGS := -ldflags "\
    -X main.Version=$(VERSION) \
    -X main.BuildTime=$(BUILD_TIME) \
    -X main.GitCommit=$(GIT_COMMIT)"
```

The date portion is extracted from `BUILD_TIME` by splitting on `_`.
