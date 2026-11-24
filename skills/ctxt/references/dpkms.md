# `dpkms` — Actions & Intents

Find your objective below and open the linked command sheet (strictly 1 hop from here). Global flags (`--format`, `--output`, `--config`, `--data-dir`, `--server-url`, `--offline`, `--instance`, `--quiet`, `--verbose`) are documented in [../SKILL.md](../SKILL.md).

## Daemon Lifecycle & Hosting

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Start background worker and REST/gRPC/MCP API server (local or pod runner) | `dpkms serve` (`start`) | Required before async pipelines run; supports `--runner pod`, `--daemon`, `--strict-permissions`, `--strict-sharing` | [dpkms/serve.md](dpkms/serve.md) |
| Stop, restart, list running instances, probe `/healthz`, or install launchd/systemd service | `shutdown` / `reboot` / `ps` / `healthcheck` / `install` | Graceful stop (`stop`), pod-aware `ps`/`shutdown`, `/healthz` probe, OS service setup | [dpkms/lifecycle.md](dpkms/lifecycle.md) |

## Data, Storage Encryption & Maintenance

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Encrypt plaintext SQLite DB & blobs at rest, or rotate KEK/DEK keys | `dpkms storage` | `encrypt` (plaintext → encrypted), `rekey` (`--rewrap-only`, `--to-key`) | [dpkms/storage.md](dpkms/storage.md) |
| Create a backup archive or restore from one | `dpkms backup` / `dpkms restore` | Archive/dir backups (encrypted when key provider configured); `restore` needs `--confirm-token` | [dpkms/backup-restore.md](dpkms/backup-restore.md) |
| Reclaim space, rebuild indexes, compact DB, prune old data, reap zombies/blobs | `dpkms housekeeping` | `vacuum`, `reindex`, `compact`, `prune`, `run` (`--blob-grace`, `--zombie-threshold`) | [dpkms/housekeeping.md](dpkms/housekeeping.md) |

## Pipelines, Detectors & Job Queue

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Manage ingestion pipelines, direct enqueue, external steps & step registries | `dpkms pipeline` | CRUD, `enqueue`, `step {list,install (--path|--registry),uninstall,registry}` | [dpkms/pipeline.md](dpkms/pipeline.md) |
| Route inputs automatically to pipelines by extension, URL pattern, or content test | `dpkms detector` | `add`, `list`, `enable`, `disable`, `remove` | [dpkms/detector.md](dpkms/detector.md) |
| Inspect, read logs, retry, or cancel async ingestion jobs | `dpkms job` | `list`, `status`, `log`, `retry`, `cancel` | [dpkms/job.md](dpkms/job.md) |

## Security, Access & Host Governance

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Read, list, or securely write secrets (stdin / `--from-file` / `--prompt`) | `dpkms secret` | Never pass secret values as positional CLI args | [dpkms/secret.md](dpkms/secret.md) |
| Initialize or rotate Ed25519 signing keys | `dpkms key` | `init`, `rotate` | [dpkms/key.md](dpkms/key.md) |
| Host-level API keys, namespace entitlements, UI sessions, access class, posture doctor, and steward audit chain verification | `auth` / `entitlement` / `session` / `instance` / `doctor` / `audit` | Direct host admin ops (`auth key create`, `entitlement`, `session`, `instance set-access`, `doctor --strict`, `audit verify`) | [dpkms/access-governance.md](dpkms/access-governance.md) |

## Development & System

| Objective | Command | When to pick this over siblings | Reference |
|---|---|---|---|
| Lint registry YAML schemas or generate Cobra CLI docs | `dpkms dev` | `validate-registry`, `gen-docs` | [dpkms/dev.md](dpkms/dev.md) |
| Resolve config cascade paths or validate `dpkms` config | `dpkms config` | `path`, `paths`, `validate` | [dpkms/config.md](dpkms/config.md) |
| Check for or install `dpkms` binary self-upgrades | `dpkms upgrade` | `check`, `install`, `notes`, `snooze` | [dpkms/upgrade.md](dpkms/upgrade.md) |
| Install optional runtime dependencies | `dpkms install-deps` | Installs external media/runtime binaries | *(inline)* |
| Print version or generate shell completions | `dpkms version` / `dpkms completion` | `dpkms version [--check]`; `dpkms completion [bash\|zsh\|fish\|powershell]` | *(inline)* |
