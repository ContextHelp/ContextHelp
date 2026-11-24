# `ctxt lateral`

Run the opt-in lateral discovery daemon, inspect its effective `lateral.*` config, and evaluate discovery strategies offline against JSONL fixtures.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `lateral` / `lateral start` | `interactive` (foreground daemon) | `no` |
| `lateral status` | `read` | `yes` |
| `lateral config show` | `read` | `yes` |
| `lateral eval replay <fixtures.jsonl>` | `read` | `yes` |
| `lateral eval metrics <fixtures.jsonl>` | `read` | `yes` |

## Subcommands & Flags

```bash
ctxt lateral [start] [--worker-id <id>] [--poll-interval <dur>]
ctxt lateral status
ctxt lateral config show
ctxt lateral eval replay <fixtures.jsonl>
ctxt lateral eval metrics <fixtures.jsonl> [--json]
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--worker-id <id>` | `start` | `lateral-daemon-default` | Worker ID for the cold-cycle poller |
| `--poll-interval <dur>` | `start` | `5s` | Cold-cycle poll interval |
| `--json` | `eval metrics` | `false` | Emit JSON instead of text table |

## Agent Recipes

```bash
# Inspect which lateral strategies are enabled in layered config
ctxt lateral config show

# Run offline evaluation over fixture JSONL
ctxt lateral eval replay ./fixtures.jsonl
ctxt lateral eval metrics ./fixtures.jsonl --json

# Launch foreground daemon with custom poll interval
ctxt lateral start --worker-id worker-1 --poll-interval 10s
```

## Gotchas & Next Steps

- All strategies are opt-in via `lateral.strategies.<name>.enabled` in config (env prefix: `CTXT_LATERAL`).
- `ctxt lateral status` is currently a stub returning `ErrNotWired`.
- Next: inspect overall config ([config.md](config.md)).
