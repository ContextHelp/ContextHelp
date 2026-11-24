# `dpkms` — Actions & Intents

Find your objective below, then open the linked command reference. Global flags (`--format`, `--output`, `--config`, `--data-dir`, `--server-url`, `--offline`, `--instance`, `--quiet`, `--verbose`) are documented in [../SKILL.md](../SKILL.md).

> **Exit codes**: `0` = success, `1` = general error, `4` = policy denied (`409 POLICY_DENIED`).

## Daemon Lifecycle & Service Management

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Start the background worker and REST/gRPC/MCP API server | `dpkms serve` (`start`) | Required before any async pipeline runs; supports `--daemon`, `--name`, `--port` | [dpkms/serve.md](dpkms/serve.md) |
| Stop or restart a running instance | `dpkms shutdown` (`stop`) / `dpkms reboot` | Graceful shutdown or reload by `--port` or `--server-url` | [dpkms/lifecycle.md](dpkms/lifecycle.md) |
| List running instances or probe `/healthz` | `dpkms ps` / `dpkms healthcheck` | Discover local PIDs/ports or readiness-probe HTTP `/healthz` | [dpkms/lifecycle.md](dpkms/lifecycle.md) |
| Register `dpkms` as a launchd or systemd user service | `dpkms install` | Persistent OS background service (`--launchd`, `--systemd`, `--status`, `--uninstall`) | [dpkms/lifecycle.md](dpkms/lifecycle.md) |

## Pipelines, Detectors & Job Queue

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Manage ingestion pipelines, steps, and step registries | `dpkms pipeline` | Pipeline CRUD, `enqueue`, `step {list,install,uninstall,registry}` | [dpkms/pipeline.md](dpkms/pipeline.md) |
| Route inputs automatically to pipelines by extension/URL/content | `dpkms detector` | `add`, `list`, `enable`, `disable`, `remove` | [dpkms/detector.md](dpkms/detector.md) |
| Inspect, trace logs, retry, or cancel async ingestion jobs | `dpkms job` | `list`, `status`, `log`, `retry`, `cancel` | [dpkms/job.md](dpkms/job.md) |

## Data Durability & Maintenance

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Create a backup archive or restore from one | `dpkms backup` / `dpkms restore` | Tarball/directory backups (`--include-blobs`, `--include-configs`, `--dry-run`) | [dpkms/backup-restore.md](dpkms/backup-restore.md) |
| Reclaim space, rebuild indexes, compact DB, prune old data | `dpkms housekeeping` | `vacuum`, `reindex`, `compact`, `prune`, `run` | [dpkms/housekeeping.md](dpkms/housekeeping.md) |

## Security & Credentials

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Get, list, or securely set secrets | `dpkms secret` | Always feed values via stdin, `--from-file`, or `--prompt` — never CLI args | [dpkms/secret.md](dpkms/secret.md) |
| Initialize or rotate Ed25519 signing keys | `dpkms key` | `init`, `rotate` (used for signed config bundles and provenance) | [dpkms/key.md](dpkms/key.md) |

## Development & System

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Re-embed vectors, lint registries, scaffold plugins, gen CLI docs | `dpkms dev` | `reindex-vectors`, `validate-registry`, `init-plugin`, `gen-docs` | [dpkms/dev.md](dpkms/dev.md) |
| Resolve effective `dpkms` config paths | `dpkms config` | `path` and `paths` (4-layer cascade with `--from`) | [dpkms/config.md](dpkms/config.md) |
| Check for or install `dpkms` binary upgrades | `dpkms upgrade` | `check`, `install`, `notes`, `snooze` | [dpkms/upgrade.md](dpkms/upgrade.md) |
| Install optional media/runtime dependencies | `dpkms install-deps` | Installs external runtime binaries (e.g. `pdftotext`, `whisper`, `pyannote`) | *(inline)* |
| Print version or generate shell completions | `dpkms version` / `dpkms completion` | `dpkms version [--check]`; `dpkms completion [bash\|zsh\|fish\|powershell]` | *(inline)* |
