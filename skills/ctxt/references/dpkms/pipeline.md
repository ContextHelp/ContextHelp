# `dpkms pipeline`

Manage ingestion pipelines, enqueue direct pipeline work, install/uninstall external pipeline steps (from local dir or registry), and manage step registries.

## Execution Profile

| Subcommand | Aliases | Side effect | Policy note |
|---|---|---|---|
| `list` / `show <name>` | `ls`,`all` / `view` | `read` | — |
| `create <file>` | `add` | `write` | Accepts `-n/--note` |
| `archive <name>` / `unarchive <name>` | `disable` / `enable` | `write` | `-n/--note` **required** on `archive` by default policy |
| `remove <name>` | `del` | `destructive` | `-n/--note` **required** by default policy |
| `enqueue [input]` | `push` | `write` | Enqueues job directly |
| `step ...` | — | `read` / `write` | Manages external step packages & registries |

## Subcommands & Flags

### Pipeline CRUD & enqueue

```bash
dpkms pipeline list [--name <substr>] [--include-archived] [--only-archived]
dpkms pipeline show <name> [--raw]
dpkms pipeline create <file> [-n|--note <reason>]
dpkms pipeline archive <name> -n <reason>
dpkms pipeline unarchive <name> [-n|--note <reason>]
dpkms pipeline remove <name> -n <reason>
dpkms pipeline enqueue [content|file|-] [--type <t>] [--pipeline <name>] [--wait]
```

### Pipeline steps (`dpkms pipeline step`)

```bash
dpkms pipeline step list [--source <src>]
dpkms pipeline step install <name> (--path <dir> | --registry <url>)
dpkms pipeline step uninstall <name>
```

### Step registries (`dpkms pipeline step registry`)

```bash
dpkms pipeline step registry list
dpkms pipeline step registry add <name> <url>
dpkms pipeline step registry update <url>
dpkms pipeline step registry autoupdate <url> [--enable | --disable]
```

## Agent Recipes

```bash
dpkms pipeline list --format json
dpkms pipeline show text.long --raw
dpkms pipeline archive legacy.notes --note "replaced by text.long"
dpkms pipeline step install my-step --path ./steps/my-step
```

## Next Steps

- Automatic input routing rules: [detector.md](detector.md) · Monitor enqueued jobs: [job.md](job.md)
