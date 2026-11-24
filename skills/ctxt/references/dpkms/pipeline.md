# `dpkms pipeline`

Manage ingestion pipelines, enqueue direct pipeline work, install/uninstall external pipeline steps, and manage step registries.

## Execution Profile

| Subcommand | Aliases | Side effect | Policy note |
|---|---|---|---|
| `pipeline list` | `ls`, `all` | `read` | — |
| `pipeline show <name>` | `view` | `read` | — |
| `pipeline create <file>` | `add` | `write` | Accepts `-n/--note` for audit/policy |
| `pipeline archive <name>` | `disable` | `write` | `-n/--note` **required** by default policy |
| `pipeline unarchive <name>` | `enable` | `write` | Accepts `-n/--note` |
| `pipeline remove <name>` | `del` | `destructive` | `-n/--note` **required** by default policy |
| `pipeline enqueue [input]` | `push` | `write` | Enqueues job directly |
| `pipeline step ...` | — | `read` / `write` | Manages external step packages & registries |

> Policy rejections exit with code **4** (`409 POLICY_DENIED`).

## Subcommands & Flags

### Pipeline CRUD & direct enqueue

```bash
dpkms pipeline list [--name <substr>] [--include-archived] [--only-archived]
dpkms pipeline show <name> [--raw]
dpkms pipeline create <file> [-n|--note <reason>]
dpkms pipeline archive <name> -n <reason>
dpkms pipeline unarchive <name> [-n|--note <reason>]
dpkms pipeline remove <name> -n <reason>
dpkms pipeline enqueue [content|file|-] [--type <t>] [--pipeline <name>] [--wait]
```

| Flag | Subcommand(s) | Default | Purpose |
|---|---|---|---|
| `--name <substr>` | `list` | `""` | Filter pipelines by name |
| `--include-archived` | `list` | `false` | Include archived pipelines |
| `--only-archived` | `list` | `false` | Show only archived pipelines |
| `--raw` | `show` | `false` | Output raw JSON |
| `-n`, `--note <text>` | `create`, `archive`, `unarchive`, `remove` | `""` | Audit/policy note (required on `archive` and `remove`) |
| `--type <kind>` | `enqueue` | `text` | Content type |
| `--pipeline <name>` | `enqueue` | `""` | Target pipeline name |
| `--wait` | `enqueue` | `false` | Wait for job completion |

### Pipeline steps (`dpkms pipeline step`)

```bash
dpkms pipeline step list [--source <src>]
dpkms pipeline step install <name> --registry <url>
dpkms pipeline step uninstall <name>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--source <src>` | `step list` | `""` | Filter available steps by source |
| `--registry <url>` | `step install` | `""` | Step registry URL |

### Step registries (`dpkms pipeline step registry`)

```bash
dpkms pipeline step registry list
dpkms pipeline step registry add <name> <url>
dpkms pipeline step registry update <url>
dpkms pipeline step registry autoupdate <url> [--enable | --disable]
```

## Agent Recipes

```bash
# List active pipelines and inspect one as raw JSON
dpkms pipeline list --format json
dpkms pipeline show text.long --raw

# Create a custom pipeline and archive an obsolete one with required audit note
dpkms pipeline create ./my-pipeline.yaml --note "add meeting transcript pipeline"
dpkms pipeline archive legacy.notes --note "replaced by text.long"

# Install an external step from a registered step registry
dpkms pipeline step registry add core https://registries.context.help/steps
dpkms pipeline step install pdf-ocr --registry https://registries.context.help/steps
```

## Gotchas & Next Steps

- Always supply `--note` on `archive` and `remove` or the default policy returns exit code `4`.
- For automatic input-to-pipeline routing rules, configure detectors ([detector.md](detector.md)).
- Next: monitor enqueued jobs ([job.md](job.md)) or scaffold a plugin ([dev.md](dev.md)).
