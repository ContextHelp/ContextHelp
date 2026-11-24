# `dpkms detector`

Manage automatic input-to-pipeline detectors matching by file extension, URL pattern, or content test.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `detector list` | `read` | `yes` |
| `detector add` | `write` | `no` |
| `detector enable <id>` / `disable <id>` | `write` | `yes` |
| `detector remove <id>` | `destructive` | `yes` |

## Subcommands & Flags

```bash
dpkms detector add --kind <extension|url_pattern|content_test> --pattern <pat> --pipeline <name> [--name <label>] [--priority <n>]
dpkms detector list [--kind <k>] [--enabled] [--disabled]
dpkms detector enable <id>
dpkms detector disable <id>
dpkms detector remove <id>
```

| Flag | Subcommand(s) | Default | Purpose |
|---|---|---|---|
| `--kind <k>` | `add`, `list` | `""` | `extension`, `url_pattern`, or `content_test` (**required** on `add`) |
| `--pattern <pat>` | `add` | `""` | Extension (`.pdf`) or regexp |
| `--pipeline <name>` | `add` | `""` | Target pipeline (**required**) |
| `--name <label>` | `add` | `""` | Human-readable label |
| `--priority <n>` | `add` | `100` | Evaluation priority (lower = higher priority) |
| `--enabled` / `--disabled` | `list` | `false` | Filter by state |

## Agent Recipes

```bash
dpkms detector add --kind url_pattern --pattern "^https://github\\.com/.*/issues/" --pipeline github.issue --priority 20
dpkms detector list --enabled --format json
```

## Next Steps

- Manage target pipelines: [pipeline.md](pipeline.md)
