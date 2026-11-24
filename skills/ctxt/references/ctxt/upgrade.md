# `ctxt upgrade`

Manage `ctxt` binary self-upgrades (`check`, `install`, `notes`, `snooze`) and orchestrate `dpkms` corpus upgrade work (`plan`, `run`, `status`). Use global `--server <url>` for non-default instances.

- **Use `ctxt upgrade`** for the `ctxt` binary and corpus re-ingest upgrades.
- **Use `dpkms upgrade`** for updating the `dpkms` binary itself ([../dpkms/upgrade.md](../dpkms/upgrade.md)).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `check`, `notes`, `plan`, `status` | `read` | `yes` |
| `install`, `snooze` | `write` | `yes` |
| `run` | `write` (corpus re-ingest) | `conditional` |

## Subcommands & Flags

```bash
ctxt upgrade check | notes | install | snooze
ctxt upgrade plan
ctxt upgrade run [--filter <expr>] [--where <sql>] [--rate-limit <n>] [--budget <usd>] [--all --i-understand-the-cost <sha>]
ctxt upgrade status [--watch] [--interval <sec>]
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--filter <expr>` | `run` | `""` | Narrow affected objects (e.g. `pipeline=text.short@v0`) |
| `--where <sql>` | `run` | `""` | SQL WHERE escape hatch |
| `--rate-limit <n>` | `run` | `0` | Max re-ingests/sec (`0` = unbounded) |
| `--budget <usd>` | `run` | `0` | Cumulative LLM cost ceiling in USD (`0` = unbounded) |
| `--all` | `run` | `false` | Execute bucket-3 full-corpus re-ingest |
| `--i-understand-the-cost <sha>` | `run` | `""` | Consent SHA from release notes (**required** with `--all`) |
| `--watch` / `--interval <sec>` | `status` | `false` / `2` | Poll until idle |

## Agent Recipes

```bash
ctxt upgrade plan --format json
ctxt upgrade run --filter "pipeline=text.short@v0" --rate-limit 5 --budget 2.50
ctxt upgrade status --format json
```

## Next Steps

- Track spawned re-ingest jobs: [job.md](job.md)
