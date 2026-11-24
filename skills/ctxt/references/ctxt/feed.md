# `ctxt feed`

Subscribe to, list, sync, and remove RSS/Atom feeds managed by `dpkms`.

- **Use `feed`** for ongoing RSS/Atom subscriptions tracked by `dpkms`.
- **Use `capture <url> --every <dur>`** for ad-hoc polling of a single URL ([capture.md](capture.md)).

## Execution Profile

| Subcommand | Side effect | Confirmation |
|---|---|---|
| `feed list` | `read` | — |
| `feed add <url>` | `write` | — |
| `feed sync` | `write` (spawns sync job) | — |
| `feed delete <url-or-id>` | `destructive` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt feed add <url> [--server <url>]
ctxt feed list [--status <active|paused|error>] [--server <url>]
ctxt feed sync [--id <feed-id> | --url <feed-url>] [--server <url>]
ctxt feed delete <url-or-id> [--server <url>] --confirm=yes --confirm-token=<sha>
```

| Flag | Subcommand(s) | Default | Purpose |
|---|---|---|---|
| `--server <url>` | all | `http://localhost:8080` | Target `dpkms` HTTP URL |
| `--status <st>` | `list` | `""` | Filter by `active\|paused\|error` |
| `--id <feed-id>` | `sync` | `""` | Sync a specific feed by ID |
| `--url <feed-url>` | `sync` | `""` | Sync a specific feed by URL |

## Agent Recipes

```bash
# Subscribe and immediately sync
ctxt feed add https://hnrss.org/frontpage --format json
ctxt feed sync --url https://hnrss.org/frontpage

# Audit errored feeds
ctxt feed list --status error --format json

# Remove a feed
ctxt feed delete https://hnrss.org/frontpage --confirm=yes --confirm-token=<sha>
```

## Gotchas & Next Steps

- `feed sync` spawns an async job; follow completion with `dpkms job status <sync-job-id>` ([../dpkms/job.md](../dpkms/job.md)).
- Next: browse synced articles ([list.md](list.md)).
