# `ctxt profile`

Manage focus profiles (domain/role lenses that scope ranking, surfacing, and pipelines) and per-profile schema vocabularies (`profile schema`).

## Execution Profile

| Subcommand | Side effect | Idempotency | Confirmation |
|---|---|---|---|
| `profile list` / `show` / `schema show` | `read` | `yes` | — |
| `profile create` / `default` | `write` | `yes` | — |
| `profile delete` | `destructive` | `yes` | `--confirm=yes --confirm-token=<sha>` |
| `profile schema add-type` / `add-topic` / `add-rule` | `write` | `no` | — |
| `profile schema evolve` | `write` | `yes` | — |
| `profile schema remove-type` / `remove-topic` | `destructive` | `yes` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands

### Profile CRUD

```bash
ctxt profile list
ctxt profile show <name>
ctxt profile create <name>
ctxt profile default [name]                                     # get or set default profile
ctxt profile delete <name> --confirm=yes --confirm-token=<sha>
```

### Per-profile schema vocabulary (`ctxt profile schema`)

```bash
ctxt profile schema show <profile>
ctxt profile schema add-type <profile> <type>
ctxt profile schema remove-type <profile> <type> --confirm=yes --confirm-token=<sha>
ctxt profile schema add-topic <profile> <topic>
ctxt profile schema remove-topic <profile> <topic> --confirm=yes --confirm-token=<sha>
ctxt profile schema add-rule <profile> <pattern> <type>         # regex pattern -> entity type
ctxt profile schema evolve <profile>                            # suggest schema updates from recent ingestions
```

No local flags beyond globals ([../../SKILL.md](../../SKILL.md)).

## Agent Recipes

```bash
# Create a focus profile and set it as default
ctxt profile create research
ctxt profile default research

# Inspect and evolve a profile's schema vocabulary
ctxt profile schema show research --format json
ctxt profile schema evolve research --format json

# Apply a suggested type, topic, and regex classification rule
ctxt profile schema add-type research paper
ctxt profile schema add-topic research retrieval
ctxt profile schema add-rule research "arxiv\\.org" paper
```

## Gotchas & Next Steps

- Deleting the active default profile clears the default; pin a replacement with `ctxt profile default <name>`.
- Scope any `ctxt` command to a profile via global `--profile <name>` or `CTXT_PROFILE=<name>`.
- Next: search under a profile ([find.md](find.md)), resurface candidates ([resurface.md](resurface.md)), or reprocess objects ([reprocess.md](reprocess.md)).
