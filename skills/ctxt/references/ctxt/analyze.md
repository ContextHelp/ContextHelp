# `ctxt analyze`

Enqueue content into the ingestion pipeline. Default verb when running bare `ctxt [content]`.

- **Use `analyze`** when attaching structured `--data` JSON metadata, `--note @file`, `--language`, `--translate`, or `-f/--file`.
- **Use `capture`** for expiry (`--ttl`/`--expires`), encryption (`--encrypt`), `--inbox`, `--every`, browser tabs/history, or `--ambient` ([capture.md](capture.md)).

## Execution Profile

- **Side effect**: `write` (enqueues async job for `dpkms serve`)
- **Idempotency**: `conditional` — supplying `--source-key` updates the existing object's `note` and `data` instead of duplicating.

## Usage

```bash
ctxt analyze [content] [flags]
ctxt [content] [flags]            # shorthand; bare ctxt delegates to analyze
```

Input resolution: positional `[content]` → stdin pipe → `-f/--file` → clipboard fallback.

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--type <kind>` | `text` | `text\|url\|image\|audio\|video\|feed\|auto` |
| `-f`, `--file <path>` | `""` | Read input from file |
| `--hint <h>` | `[]` | Tag hints; repeatable, comma- or space-separated, leading `#` optional |
| `--mention <@slug>` | `[]` | Mentions as `@namespace.slug`; repeatable, comma- or space-separated |
| `--note <text>` | `""` | Free-form text stored with object (e.g. transcript; `@file` reads a file, `@@` literal `@`) |
| `--data <json>` | `""` | Structured JSON object stored as object metadata, inline or `@file` (max 256 KiB) |
| `--pipeline <name>` | `""` | Force specific pipeline |
| `--language <code>` | `""` | Input language override |
| `--translate <mode>` | `""` | Translation mode (`none` to skip) |
| `--raw` | `false` | Disable AI; store raw knowledge object |
| `--no-fanout` | `false` | Skip post-ingest fan-out enrichment |
| `--no-dedup` | `false` | Skip duplicate detection |
| `--source-key <key>` | `""` | Stable source identity (`youtube:<id>`, Slack `ts`); repeat updates `note` and `data` |
| `--wait` | `false` | Block until job completes |

## Agent Recipes

```bash
# Stdin text with hints, mention, and structured JSON metadata
echo "Fix signup flow" | ctxt analyze --type text \
  --hint "ux,onboarding" --mention "@product.auth" \
  --data '{"severity":"p1","area":"signup"}' --wait --format json

# Attach a transcript file via --note @file with stable --source-key
ctxt analyze https://youtu.be/abc123 --type url \
  --source-key "youtube:abc123" --note @transcript.txt --wait

# Skip translation on non-English file
ctxt analyze --file notes-fr.md --language fr --translate none --wait
```

## Gotchas & Next Steps

- Bare `ctxt` rejects mistyped subcommands (e.g. `ctxt fnd`) rather than capturing them as text.
- Next: poll async job ([job.md](job.md)), inspect ([show.md](show.md), [resolve.md](resolve.md)), or search ([find.md](find.md)).
