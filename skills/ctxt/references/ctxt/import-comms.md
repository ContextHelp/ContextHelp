# `ctxt import` — Email, Chat & Social (`email`, `slack`, `discord`, `twitter`, `linkedin`)

Import communications archives and mailboxes. Use global `--server <url>` for non-default instances and global `--dry-run` to preview.

## Execution Profile

- **Side effect**: `write` (enqueues jobs onto `dpkms`)
- **Idempotency**: `conditional`

## `ctxt import email`

Import from an IMAP mailbox (`--provider imap`) or local `.eml`/`.mbox` files (`--provider file`).

| Flag | Default | Purpose |
|---|---|---|
| `--provider <p>` | `file` | `imap\|file` |
| `--file <path>` | `""` | `.eml` file or `.mbox` archive (`file` provider) |
| `--host <host>` | `""` | IMAP hostname or `host:port` (`imap` provider) |
| `--user <email>` | `""` | IMAP username/email |
| `--password <pw>` | `""` | IMAP password or app password (avoid shell history) |
| `--tls` | `false` | Implicit TLS (port 993); default is STARTTLS (port 143) |
| `--port <n>` | `0` | Port override (993 for `--tls`, 143 otherwise) |
| `--folder <name>` | `INBOX` | IMAP folder to sync |
| `--rules <yaml>` | `""` | YAML routing rules file |
| `--since <date>` | `""` | Messages after RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max messages (`0` = unlimited) |
| `--pipeline <name>` | `""` | Override routing for all messages |

## `ctxt import slack`

| Flag | Default | Purpose |
|---|---|---|
| `--dir <path>` | `""` | Unpacked Slack workspace export directory |
| `--channel <name>` | `[]` | Include only listed channels (repeatable) |
| `--since <date>` | `""` | Messages on/after RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max messages (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |

## `ctxt import discord`

| Flag | Default | Purpose |
|---|---|---|
| `--file <path>` | `""` | Path to Discord JSON export file |
| `--since <date>` | `""` | Messages on/after RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max messages (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |

## `ctxt import twitter`

| Flag | Default | Purpose |
|---|---|---|
| `--file <path>` | `""` | Path to Twitter/X archive tweets JS file (required) |
| `--username <handle>` | `""` | Twitter/X username (for canonical tweet URLs) |
| `--since <date>` | `""` | Tweets created since RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max tweets (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |

## `ctxt import linkedin`

| Flag | Default | Purpose |
|---|---|---|
| `--posts <path>` | `""` | Path to LinkedIn `Posts.csv` export |
| `--articles <path>` | `""` | Path to LinkedIn `Articles.csv` export |
| `--since <date>` | `""` | Published since RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max items (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |

## Agent Recipes

```bash
# Dry-run an mbox archive with custom routing rules
ctxt import email --provider file --file archive.mbox --rules rules.yaml --dry-run

# Import specific Slack channels since a date
ctxt import slack --dir ./slack-export --channel engineering --channel incidents --since 2026-01-01
```

## Next Steps

- For live recurring email ingestion via `himalaya`, see [ingest.md](ingest.md).
- Verify imports: [list.md](list.md) · Search: [find.md](find.md)
