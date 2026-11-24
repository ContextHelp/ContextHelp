# `ctxt uri`

Register the `ctxt://` OS URL scheme or print a static registration snippet for bundled packaging.

Bare `ctxt ctxt://<objectID>` opens an object (`show`) and `ctxt ctxt://search/<query>` runs `find`.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `uri register` | `write-local` | `yes` |
| `uri snippet` | `read` | `yes` |

## Subcommands & Flags

```bash
ctxt uri register
ctxt uri snippet [--platform <macos|ios|linux|windows>]
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--platform <os>` | `snippet` | host OS | `macos\|ios\|linux\|windows` |

## Agent Recipes

```bash
ctxt uri register
ctxt uri snippet --platform linux
```
