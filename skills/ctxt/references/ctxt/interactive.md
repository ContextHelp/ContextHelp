# `ctxt` Interactive Surfaces (`setup`, `shell`, `tui`)

Three interactive/session commands for onboarding, low-overhead multi-query REPLs, and terminal browsing.

## Execution Profile

| Command | Side effect | Idempotency | Notes |
|---|---|---|---|
| `ctxt setup` | `interactive` (writes initial config) | `yes` | Use `--non-interactive` in CI/agent bootstrapping |
| `ctxt shell` | `interactive` (persistent REPL) | `no` | Reuses SQLite/service handle; eliminates ~50ms startup per command |
| `ctxt tui` | `interactive` (full-screen TUI) | `no` | Human-facing; avoid in headless agents |

## Usage & Flags

```bash
ctxt setup [--non-interactive]
ctxt shell
ctxt tui [--theme <default|high-contrast|solarized>]
```

| Command | Flag | Default | Purpose |
|---|---|---|---|
| `setup` | `--non-interactive` | `false` | Skip prompts and write defaults (auto-enabled when `CI` env is set) |
| `tui` | `--theme <name>` | `default` | Color theme: `default\|high-contrast\|solarized` |

## Agent Recipes

```bash
# Headless first-time config bootstrap, then verify
ctxt setup --non-interactive && ctxt config show --format json && ctxt doctor --format json

# Batch multiple queries in one REPL session (accepts ctxt verbs and direct RSQL)
printf 'find "rate limits"\ntype==url;tag==api\nexit\n' | ctxt shell
```

## Gotchas & Next Steps

- Inside `ctxt shell`, lines containing `==` or `=in=` automatically dispatch to `ctxt list --q <line>`.
- Never invoke `ctxt tui` or interactive `ctxt setup` (without `--non-interactive`) in a non-TTY agent runner.
- Next: validate config ([config.md](config.md)) or run health checks ([doctor.md](doctor.md)).
