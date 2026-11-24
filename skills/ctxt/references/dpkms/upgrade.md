# `dpkms upgrade`

Check release notes, inspect availability, snooze prompts, or self-upgrade the `dpkms` binary in place.

- **Use `dpkms upgrade`** to update the `dpkms` binary itself.
- **Use `ctxt upgrade`** to update `ctxt` or orchestrate corpus re-ingest migrations (`plan`/`run`/`status`) ([../ctxt/upgrade.md](../ctxt/upgrade.md)).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `upgrade check` | `read` | `yes` |
| `upgrade notes` | `read` | `yes` |
| `upgrade install` | `write` (replaces binary) | `yes` |
| `upgrade snooze` | `write` | `yes` |

## Subcommands

```bash
dpkms upgrade check     # report whether a newer release exists
dpkms upgrade notes     # print release notes for the latest version
dpkms upgrade install   # download and replace dpkms binary in place
dpkms upgrade snooze    # defer upgrade prompt for configured duration
```

No local flags beyond globals ([../../SKILL.md](../../SKILL.md)).

## Agent Recipes

```bash
# Check if an upgrade is available and inspect release notes before installing
dpkms upgrade check --format json
dpkms upgrade notes

# Upgrade binary, then restart running daemon
dpkms upgrade install && dpkms reboot
```

## Next Steps

- Restart running daemon after upgrade: [lifecycle.md](lifecycle.md) · Run any corpus migrations: [../ctxt/upgrade.md](../ctxt/upgrade.md)
