# `ctxt remind`

Set, list, or clear scheduled reminders on knowledge objects.

- **Use `remind`** for explicit time-triggered reminders on known object IDs.
- **Use `resurface`** for algorithmic relevance resurfacing across a focus profile ([resurface.md](resurface.md)).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `remind list` | `read` | `yes` |
| `remind set <id> <time-expression>` | `write` | `yes` |
| `remind clear <id>` | `write` | `yes` |

## Subcommands

```bash
ctxt remind set <id> <time-expression>
ctxt remind list
ctxt remind clear <id>
```

## Agent Recipes

```bash
ctxt remind set obj_12345678 "in 7 days"
ctxt remind list --format json
ctxt remind clear obj_12345678
```

## Gotchas & Next Steps

- Reminder firing requires `dpkms serve` running (`--reminder-interval`, see [../dpkms/serve.md](../dpkms/serve.md)).
- Next: inspect target object ([show.md](show.md)).
