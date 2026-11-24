# `ctxt capture`

Canonical verb for explicit content capture from a URL, file, stdin, or clipboard.

- **Use `capture` over `analyze`** for standard URL/file/stdin/clipboard ingestion, continuous polling (`--every`), inbox routing (`--inbox`), or attaching an audit `--note`.
- **Use `analyze`** if you need `--language` or `--translate` overrides or `-f/--file` syntax ([analyze.md](analyze.md)).

## Execution Profile

- **Side effect**: `write` (enqueues an async ingestion job; `dpkms serve` runs the pipeline)
- **Idempotency**: `conditional` — replays create duplicate jobs unless `--source-key` or global `--idempotency-key` is passed.

## Usage

```bash
ctxt capture [source] [flags]
```

Source resolution order:
1. Positional `<source>` (URL, file path, or quoted text)
2. `--stdin` (read from standard input)
3. Clipboard fallback when omitted (disable with `CTXT_NO_CLIPBOARD=1`)

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--source <val>` | `""` | Override auto-detection on positional source |
| `--stdin` | `false` | Read content from stdin |
| `--type <kind>` | `text` | `text\|url\|image\|audio\|video\|feed\|auto` |
| `--pipeline <name>` | `""` | Override server-selected pipeline |
| `--inbox` | `false` | Stage in inbox instead of running pipeline immediately |
| `--every <duration>` | `0` | Continuous polling cadence (e.g. `15m`; Ctrl-C stops) |
| `--hint <h>` | `[]` | Tagging hints (repeatable or CSV, e.g. `--hint "#ux,#p1"`) |
| `--mention <@slug>` | `[]` | Mention targets (repeatable or CSV) |
| `--note <text>` | `""` | Audit note explaining why this was captured |
| `--raw` | `false` | Bypass AI enrichment; store raw object |
| `--no-fanout` | `false` | Skip post-ingest fan-out enrichment |
| `--no-dedup` | `false` | Skip duplicate detection |
| `--source-key <key>` | `""` | External dedup key (e.g. Slack `ts`, tweet ID) |
| `--wait` | `false` | Block until ingestion job completes |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` HTTP URL |

> Reserved (not yet implemented): `--ambient`, `--input`, `--skip`, `--window`.

## Agent Recipes

```bash
# Capture inline text, wait for completion, emit JSON
ctxt capture "API rate limit is 1000 req/min" \
  --hint "#api,#limits" --mention "@stripe.api" --wait --format json

# Capture a URL idempotently using an external dedup key
ctxt capture https://example.com/post \
  --type url --source-key "post:example-2026" --wait --format json

# Pipe file or command output via stdin
cat README.md | ctxt capture --stdin --hint "#docs" --wait

# Stage a large recording in inbox for later triage
ctxt capture ./meeting.m4a --type audio --inbox --note "weekly sync"
```

## Gotchas & Next Steps

- Without `--wait`, capture returns a job ID immediately; never run `ctxt compose` until the job finishes.
- Next: inspect captured object ([show.md](show.md)), track async job ([../dpkms/job.md](../dpkms/job.md)), triage inbox ([inbox.md](inbox.md)), or search ([find.md](find.md)).
