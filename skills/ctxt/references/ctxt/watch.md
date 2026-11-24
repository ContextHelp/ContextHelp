# `ctxt watch`

Manage passive background watchers for clipboard changes and configured local directories.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `watch status` | `read` | `yes` |
| `watch start` | `write` (blocks foreground) | `yes` |
| `watch stop` | `write` | `yes` |
| `watch enable <watcher>` | `write` | `yes` |
| `watch disable <watcher>` | `write` | `yes` |

## Subcommands

```bash
ctxt watch status               # show status of all configured watchers
ctxt watch start                # start all configured watchers and block
ctxt watch stop                 # stop all active directory watchers
ctxt watch enable <watcher>     # enable a watcher by name (e.g. clipboard)
ctxt watch disable <watcher>    # disable a watcher by name (e.g. clipboard)
```

No local flags beyond globals ([../../SKILL.md](../../SKILL.md)).

## Agent Recipes

```bash
# Check configured watcher status as JSON
ctxt watch status --format json

# Start directory watchers while unconditionally disabling clipboard capture
CTXT_NO_CLIPBOARD=1 ctxt watch start

# Disable clipboard watcher persistently
ctxt watch disable clipboard && ctxt watch status
```

## Gotchas & Next Steps

- `watch start` blocks the terminal until interrupted; run in a dedicated background process or check `watch status` from another shell.
- Set `CTXT_NO_CLIPBOARD=1` in headless/agent environments to prevent unintentional clipboard capture.
- Next: configure watched directories in config ([config.md](config.md)) or browse captured items ([list.md](list.md)).
