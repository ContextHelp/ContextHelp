# `ctxt upgrade`

Manage `ctxt` binary self-upgrades (`check`, `install`, `notes`, `snooze`) and orchestrate `dpkms` corpus upgrade migrations (`plan`, `run`, `status`).

- **Use `ctxt upgrade`** for the `ctxt` binary and `dpkms` corpus re-ingest upgrades.
- **Use `dpkms upgrade`** for updating the `dpkms` binary itself ([../dpkms/upgrade.md](../dpkms/upgrade.md)).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `check`, `notes`, `plan`, `status` | `read` | `yes` |
| `install`, `snooze` | `write` | `yes` |
| `run` | `write` (re-ingests corpus) | `conditional` |

## Subcommands & Flags

### Binary self-upgrade

```bash
ctxt upgrade check     # report whether a newer release exists
ctxt upgrade notes     # print release notes for the latest version
ctxt upgrade install   # download and replace ctxt binary in place
ctxt upgrade snooze    # defer upgrade prompt for configured duration
```

### Corpus upgrade orchestration (`plan` / `run` / `status`)

```bash
ctxt upgrade plan [--server <url>]
ctxt upgrade run [flags]
ctxt upgrade status [--server <url>] [--watch] [--interval <sec>]
```

| Flag | Subcommand(s) | Default | Purpose |
|---|---|---|---|
| `--server <url>` | `plan`, `run`, `status` | `http://localhost:8080` | Target `dpkms` HTTP URL |
| `--filter <expr>` | `run` | `""` | Narrow affected objects (e.g. `pipeline=text.short@v0`) |
| `--where <sql>` | `run` | `""` | SQL WHERE escape hatch |
| `--rate-limit <n>` | `run` | `0` | Max re-ingests/sec (`0` = unbounded) |
| `--budget <usd>` | `run` | `0` | Cumulative LLM cost ceiling in USD (`0` = unbounded) |
| `--all` | `run` | `false` | Execute a full-corpus re-ingest |
| `--i-understand-the-cost <sha>` | `run` | `""` | Consent SHA from release notes (**required** with `--all`) |
| `--watch` | `status` | `false` | Poll every `--interval` seconds until idle |
| `--interval <sec>` | `status` | `2` | Poll interval for `--watch` |

## Agent Recipes

```bash
# Always preview corpus upgrade impact first (read-only)
ctxt upgrade plan --format json

# Execute a bounded subset upgrade with rate and cost guardrails
ctxt upgrade run --filter "pipeline=text.short@v0" --rate-limit 5 --budget 2.50

# Check in-flight upgrade progress
ctxt upgrade status --format json
```

## Gotchas & Next Steps

- Never run `ctxt upgrade run --all` without inspecting `ctxt upgrade plan` and setting `--budget` / `--rate-limit` as appropriate.
- Next: monitor resulting jobs ([../dpkms/job.md](../dpkms/job.md)).
