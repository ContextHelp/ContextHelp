# `ctxt lateral`

Run the opt-in lateral discovery daemon, inspect effective `lateral.*` configuration, and evaluate discovery strategies offline against JSONL fixtures.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `lateral` / `lateral start` | `interactive` (foreground daemon) | `no` |
| `lateral status` / `config show` / `eval replay` / `eval metrics` | `read` | `yes` |

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
| `--worker-id <id>` | `start` | `lateral-daemon-default` | Worker ID for cold-cycle poller |
| `--poll-interval <dur>` | `start` | `5s` | Cold-cycle poll interval |
| `--json` | `eval metrics` | `false` | Emit JSON instead of text table |

## Agent Recipes

```bash
ctxt lateral config show
ctxt lateral eval replay ./fixtures.jsonl
ctxt lateral eval metrics ./fixtures.jsonl --json
```

## Next Steps

- Inspect overall layered config: [config.md](config.md)
