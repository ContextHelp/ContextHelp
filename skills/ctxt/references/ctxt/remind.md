# `ctxt remind`

Schedule, list, or clear time-based reminders attached to knowledge objects.

- **Use `remind`** for explicit, time-triggered follow-ups on a known object ID.
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

No local flags beyond globals ([../../SKILL.md](../../SKILL.md)).

## Agent Recipes

```bash
# Set a reminder on a decision record
ctxt remind set obj_12345678 "in 7 days"

# List active reminders as JSON
ctxt remind list --format json

# Clear a completed reminder
ctxt remind clear obj_12345678 && ctxt remind list
```

## Gotchas & Next Steps

- Reminder firing requires `dpkms serve` running (polling interval controlled by `dpkms serve --reminder-interval`, see [../dpkms/serve.md](../dpkms/serve.md)).
- Next: inspect the target object ([show.md](show.md)).
