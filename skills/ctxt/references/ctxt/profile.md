# `ctxt profile`

Manage focus profiles (domain/role lenses scoping ranking, surfacing, and pipelines) and per-profile schema vocabularies (`profile schema`).

## Execution Profile

| Subcommand | Side effect | Idempotency | Confirmation |
|---|---|---|---|
| `list` / `view` / `schema show` | `read` | `yes` | — |
| `create` / `set` / `unset` | `write` | `yes` | — |
| `rm` | `destructive` | `yes` | `--confirm=yes --confirm-token=<sha>` |
| `schema add-type` / `add-topic` / `add-rule` | `write` | `no` | — |
| `schema evolve` | `write` | `yes` | — |
| `schema remove-type` / `remove-topic` | `destructive` | `yes` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands

### Profile CRUD & default lens

```bash
ctxt profile list
ctxt profile view <name>
ctxt profile create <name>
ctxt profile set <name>                                         # set default profile
ctxt profile unset                                              # clear default profile
ctxt profile rm <name> --confirm=yes --confirm-token=<sha>
```

### Per-profile schema vocabulary (`ctxt profile schema`)

```bash
ctxt profile schema show <profile>
ctxt profile schema add-type <profile> <type>
ctxt profile schema remove-type <profile> <type> --confirm=yes --confirm-token=<sha>
ctxt profile schema add-topic <profile> <topic>
ctxt profile schema remove-topic <profile> <topic> --confirm=yes --confirm-token=<sha>
ctxt profile schema add-rule <profile> <pattern> <type>         # regex pattern -> entity type
ctxt profile schema evolve <profile>                            # suggest improvements from recent ingestions
```

## Agent Recipes

```bash
ctxt profile create research && ctxt profile set research
ctxt profile schema show research --format json
ctxt profile schema evolve research --format json
ctxt profile schema add-rule research "arxiv\\.org" paper
```

## Gotchas & Next Steps

- Scope any `ctxt` invocation without changing the default via global `--profile <name>` or `CTXT_PROFILE=<name>`.
- Next: search under profile ([find.md](find.md)), resurface ([resurface.md](resurface.md)), or reprocess ([reprocess.md](reprocess.md)).
