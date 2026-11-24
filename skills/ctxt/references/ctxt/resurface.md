# `ctxt resurface`

Show top resurfacing candidates for the active focus profile, refresh queue scores, or dismiss entries.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `resurface` / `refresh` / `dismiss <entry-id>` | `write` | `yes` |

## Subcommands & Flags

```bash
ctxt resurface [-n|--limit <n>] [--min-score <f>]
ctxt resurface refresh
ctxt resurface dismiss <entry-id>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `-n`, `--limit <n>` | root | `0` | Max items (`0` = config default) |
| `--min-score <f>` | root | `0` | Minimum score threshold (`0` = config default) |

## Agent Recipes

```bash
ctxt resurface --profile founder --limit 10 --format json
ctxt resurface refresh && ctxt resurface --format json
ctxt resurface dismiss entry_abc123
```

## Next Steps

- Inspect a surfaced object: [show.md](show.md) · Manage profiles: [profile.md](profile.md)
