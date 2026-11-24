# `ctxt uri`

Register the `ctxt://` OS URL scheme or print a static registration snippet for packaging.

Once registered, bare `ctxt ctxt://<objectID>` opens an object (`show`) and `ctxt ctxt://search/<query>` runs `find` (respecting `uri.handler` = `cli` or `tui`).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `uri register` | `write-local` (OS handler registry) | `yes` |
| `uri snippet` | `read` | `yes` |

## Subcommands & Flags

```bash
ctxt uri register
ctxt uri snippet [--platform <macos|ios|linux|windows>]
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--platform <os>` | `snippet` | host OS (`runtime.GOOS`) | Target platform: `macos\|ios\|linux\|windows` |

## Agent Recipes

```bash
# Register ctxt:// scheme on the current OS
ctxt uri register

# Generate a Linux desktop-entry snippet for packaging
ctxt uri snippet --platform linux
```

## Next Steps

- Test dispatch directly: `ctxt ctxt://search/onboarding` ([find.md](find.md), [show.md](show.md))
