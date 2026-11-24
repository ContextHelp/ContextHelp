# `ctxt import` — Email, Chat & Social (`email`, `slack`, `discord`, `twitter`, `linkedin`)

Import communications archives and mailboxes. See [index.md](index.md) for shared execution profile.

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
| `--max-items <n>` | `0` | Max messages (`0` = all) |
| `--pipeline <name>` | `""` | Override routing for all messages |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` URL |

## `ctxt import slack`

| Flag | Default | Purpose |
|---|---|---|
| `--dir <path>` | `""` | Unpacked Slack workspace export directory |
| `--channel <name>` | `[]` | Include only listed channels (repeatable) |
| `--since <date>` | `""` | Messages on/after RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max messages (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` URL |

## `ctxt import discord`

| Flag | Default | Purpose |
|---|---|---|
| `--file <path>` | `""` | Path to Discord JSON export file |
| `--since <date>` | `""` | Messages on/after RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max messages (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` URL |

## `ctxt import twitter`

| Flag | Default | Purpose |
|---|---|---|
| `--file <path>` | `""` | Path to `tweets.js` from Twitter/X archive (required) |
| `--username <handle>` | `""` | Twitter/X handle for canonical tweet URLs |
| `--since <date>` | `""` | Tweets since RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max tweets (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` URL |

## `ctxt import linkedin`

| Flag | Default | Purpose |
|---|---|---|
| `--posts <path>` | `""` | Path to `Posts.csv` from LinkedIn export |
| `--articles <path>` | `""` | Path to `Articles.csv` from LinkedIn export |
| `--since <date>` | `""` | Content published since RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max items (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` URL |

## Agent Recipes

```bash
# Dry-run an mbox archive with routing rules
ctxt import email --provider file --file archive.mbox --rules rules.yaml --dry-run

# Import two Slack channels from a specific date
ctxt import slack --dir ./slack-export --channel engineering --channel incidents --since 2026-01-01

# Import Twitter archive
ctxt import twitter --file ./twitter-archive/data/tweets.js --username myhandle --since 2025-01-01
```

## Next Steps

- For live recurring email ingestion via `himalaya`, see [../ingest.md](../ingest.md).
- Verify imports: [../list.md](../list.md) · Back to router: [index.md](index.md)
