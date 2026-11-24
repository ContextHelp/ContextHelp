# `ctxt capture`

Canonical verb for explicit capture (URL, file, stdin, clipboard), opt-in expiry and encryption, Chromium browser tabs/history/scheduled capture, and the ambient capture daemon (`--ambient`).

- **Use `capture`** for standard ingestion, expiry (`--ttl`/`--expires`/`--expires-scope`), per-capture encryption (`--encrypt`/`--encrypt-scope`), browser tabs/history, `--inbox`, `--every`, or `--ambient`.
- **Use `analyze`** if you need `--data` (structured JSON metadata), `--language`, or `--translate` ([analyze.md](analyze.md)).

## Execution Profile

- **Side effect**: `write` (enqueues async ingestion job; `dpkms serve` executes pipelines)
- **Idempotency**: `conditional` — pass `--source-key` or global `--idempotency-key` for replay-safe dedup.

## Usage & Subcommands

```bash
ctxt capture [source] [flags]
ctxt capture browsers [--browser <name>] [--browser-profile <name>]
ctxt capture tabs     [--browser <name>] [--browser-profile <name>]
ctxt capture history  [--browser <name>] [--browser-profile <name>] [--since <t>] [--until <t>] [--range <from..to>] [--tz <iana>] [--reset-position]
ctxt capture schedule install   [--history-every <dur>] [--tabs-every <dur>]
ctxt capture schedule list
ctxt capture schedule uninstall
```

Source resolution: positional `<source>` → `--stdin` → clipboard fallback (`CTXT_NO_CLIPBOARD=1` disables).

## Key Flags (`ctxt capture`)

### Source, routing & metadata

| Flag | Default | Purpose |
|---|---|---|
| `--source <val>` | `""` | Override auto-detection on positional source |
| `--stdin` | `false` | Read content from stdin |
| `--type <kind>` | `text` | `text\|url\|image\|audio\|video\|feed\|auto` |
| `--pipeline <name>` | `""` | Override server-selected pipeline |
| `--inbox` | `false` | Route through inbox instead of running pipeline now |
| `--every <dur>` | `0` | Continuous polling cadence (one-shot when absent) |
| `--hint <h>` | `[]` | Tag hints; repeatable, comma- or space-separated, leading `#` optional |
| `--mention <@slug>` | `[]` | Mentions as `@namespace.slug`; repeatable, comma- or space-separated |
| `--note <text>` | `""` | Audit note explaining the capture |
| `--raw` | `false` | Disable AI enrichment; store raw object |
| `--no-fanout` | `false` | Skip post-ingest fan-out enrichment |
| `--no-dedup` | `false` | Skip duplicate detection |
| `--source-key <key>` | `""` | External dedup key (e.g. Slack `ts`, tweet ID) |
| `--wait` | `false` | Block until job completes |

### Automatic expiration & selective redaction

| Flag | Default | Purpose |
|---|---|---|
| `--ttl <dur>` | `""` | Auto-expire after duration (e.g. `30m`, `24h`, `7d`, `2w`) |
| `--expires <expr>` | `""` | Auto-expire at time (`RFC3339`, date, or `"in 7d"`, `"tomorrow 9am"`) |
| `--expires-scope <nl>` | `""` | Optional natural-language scope of what to expire (e.g. `"PII and phone numbers"`, `"raw content, keep summary"`); default expires whole object |

> Expired objects are erased or selectively redacted in place by `dpkms serve`'s background sweeper, skipping any object under an active hold ([hold.md](hold.md)). Manage expiry later via [expire.md](expire.md).

### Per-capture encryption at rest

| Flag | Default | Purpose |
|---|---|---|
| `--encrypt` | `false` | Encrypt this capture's data at rest using the configured key provider |
| `--encrypt-scope <nl>` | `""` | Optional natural-language scope to seal selectively (e.g. `"PII and phone numbers"`, `"raw content, keep summary"`); implies `--encrypt` |

> Fails closed when no key provider (or span locator for free-form scopes) is configured. For whole-database encryption, see [../dpkms/storage.md](../dpkms/storage.md).

### Ambient capture daemon (`--ambient`)

| Flag | Default | Purpose |
|---|---|---|
| `--ambient` | `false` | Manage or start the ambient capture daemon (`ctxd`) |
| `--foreground` | `false` | Run ambient daemon attached to terminal (local runner only) |
| `--runner <kind>` | config | Daemon runner: `local\|pod` |
| `--pod <name>` | config | Existing pod name for `--runner pod` |
| `--stop-pod` | `false` | With `--ambient stop --runner pod`, also stop the pod |
| `--input <sensors>` | `[]` | Restrict ambient capture to listed sensors |
| `--skip <sensors>` | `[]` | Exclude listed sensors from ambient capture |
| `--window <dur>` | `0` | Capture state from the last duration |

## Agent Recipes

```bash
# Capture with 7-day TTL and selective PII expiration
ctxt capture ./call-notes.md --ttl 7d --expires-scope "emails and phone numbers" --wait --format json

# Capture with selective encryption at rest
ctxt capture "Customer SSN/phone draft..." --encrypt-scope "PII and phone numbers" --wait --format json

# Capture URL idempotently with hints and mention
ctxt capture https://example.com/post --type url --source-key "post:example-2026" --hint "research" --mention "@project.alpha" --wait --format json

# Inspect Chromium profiles and backfill 7 days of browsing history
ctxt capture browsers --format json
ctxt capture history --browser chrome --browser-profile "Work" --since 7d
```

## Gotchas & Next Steps

- Without `--wait`, capture returns a job ID immediately; confirm completion before composing.
- Next: inspect ([show.md](show.md), [resolve.md](resolve.md)), adjust expiry ([expire.md](expire.md)), place holds ([hold.md](hold.md)), track jobs ([job.md](job.md)).
