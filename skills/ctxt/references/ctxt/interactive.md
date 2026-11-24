# `ctxt` Interactive Surfaces (`setup`, `shell`, `tui`, `ui`)

Onboarding wizard, persistent REPL, full-screen TUI, and web UI browser sign-in.

## Execution Profile

| Command | Side effect | Idempotency | Notes |
|---|---|---|---|
| `ctxt setup` | `interactive` (writes config) | `yes` | Pass `--non-interactive` in CI/agent bootstrapping |
| `ctxt shell` | `interactive` (REPL) | `no` | Reuses service handle; cuts startup latency across queries |
| `ctxt tui` | `interactive` (TUI) | `no` | Human-facing; avoid in headless agents |
| `ctxt ui open` | `interactive` / `read` | `yes` | Mints/opens web UI sign-in link (`--no-browser` prints URL) |

## Usage & Flags

```bash
ctxt setup [--non-interactive]
ctxt shell
ctxt tui [--theme <default|high-contrast|solarized>]
ctxt ui open [--no-browser]
```

| Command | Flag | Default | Purpose |
|---|---|---|---|
| `setup` | `--non-interactive` | `false` | Skip prompts and write defaults (auto-triggered when `CI=true`) |
| `tui` | `--theme <name>` | `default` | `default\|high-contrast\|solarized` |
| `ui open` | `--no-browser` | `false` | Print sign-in link to stdout instead of launching browser |

## Agent Recipes

```bash
# Headless first-time setup + verification
ctxt setup --non-interactive && ctxt config show --format json && ctxt doctor --format json

# Print web UI sign-in link without launching a browser
ctxt ui open --no-browser

# Batch multiple commands or direct RSQL in one REPL session
printf 'find "rate limits"\ntype==url;tag==api\nexit\n' | ctxt shell
```

## Next Steps

- Manage web UI sessions: [auth.md](auth.md), [../dpkms/access-governance.md](../dpkms/access-governance.md)
