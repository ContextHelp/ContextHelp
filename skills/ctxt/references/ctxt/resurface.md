# `ctxt resurface`

Surface top resurfacing candidates relevant to the active focus profile, refresh queue scores, or dismiss entries.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `resurface` | `write` (records surfacing state) | `yes` |
| `resurface refresh` | `write` (re-scores queue) | `yes` |
| `resurface dismiss <entry-id>` | `write` | `yes` |

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
# Surface top 10 candidates under a specific profile
ctxt resurface --profile founder --limit 10 --format json

# Re-score queue when empty or stale, then fetch
ctxt resurface refresh && ctxt resurface --format json

# Dismiss an irrelevant entry
ctxt resurface dismiss entry_abc123
```

## Next Steps

- Inspect a surfaced object: [show.md](show.md) · Configure focus profiles: [profile.md](profile.md)
