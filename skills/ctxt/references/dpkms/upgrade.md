# `dpkms upgrade`

Check release notes, check availability, snooze prompts, or self-upgrade the `dpkms` binary in place.

- **Use `dpkms upgrade`** for the `dpkms` binary itself.
- **Use `ctxt upgrade`** for `ctxt` binary upgrades and corpus migration (`plan`/`run`/`status`) ([../ctxt/upgrade.md](../ctxt/upgrade.md)).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `check`, `notes` | `read` | `yes` |
| `install`, `snooze` | `write` | `yes` |

## Subcommands

```bash
dpkms upgrade check | notes | install | snooze
```

## Agent Recipes

```bash
dpkms upgrade check --format json
dpkms upgrade notes
dpkms upgrade install && dpkms reboot
```

## Next Steps

- Restart running daemon after upgrade: [lifecycle.md](lifecycle.md)
