# `dpkms detector`

Manage automatic input-to-pipeline detectors that route captured inputs by file extension, URL pattern, or content test. Replaces deprecated `ctxt detector`.

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

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--kind <k>` | `add`, `list` | `""` | `extension`, `url_pattern`, or `content_test` (**required** on `add`) |
| `--pattern <pat>` | `add` | `""` | Match pattern (e.g. `.pdf` or regexp) |
| `--pipeline <name>` | `add` | `""` | Target pipeline name (**required**) |
| `--name <label>` | `add` | `""` | Human-readable label |
| `--priority <n>` | `add` | `100` | Evaluation priority (lower number = higher priority) |
| `--enabled` | `list` | `false` | Show only enabled detectors |
| `--disabled` | `list` | `false` | Show only disabled detectors |

## Agent Recipes

```bash
# Route GitHub issue URLs to a dedicated pipeline with high priority
dpkms detector add \
  --kind url_pattern \
  --pattern "^https://github\\.com/.*/issues/" \
  --pipeline github.issue \
  --name "GitHub Issues" \
  --priority 20

# Inspect enabled detectors as JSON
dpkms detector list --enabled --format json

# Temporarily disable a noisy detector
dpkms detector disable <detector-id>
```

## Next Steps

- Manage target pipelines: [pipeline.md](pipeline.md) · Test capture routing: [../ctxt/capture.md](../ctxt/capture.md)
