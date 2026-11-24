# `ctxt analyze`

Enqueue content into the ingestion pipeline. Also the default verb when running bare `ctxt [content]`.

- **Use `analyze` over `capture`** when you need `--language`, `--translate`, or `-f/--file` input flags.
- **Use `capture`** for canonical URL/file/stdin capture, `--inbox`, `--every`, or `--note` ([capture.md](capture.md)).

## Execution Profile

- **Side effect**: `write` (enqueues async job for `dpkms serve`)
- **Idempotency**: `conditional` — pass `--source-key` or global `--idempotency-key` for replay-safe dedup.

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
| `--hint <h>` | `[]` | Tagging hints (repeatable or CSV) |
| `--mention <@slug>` | `""` | Explicit mentions to attach (e.g. `"@ui.best-practice"`) |
| `--pipeline <name>` | `""` | Force specific pipeline |
| `--language <code>` | `""` | Input language override |
| `--translate <mode>` | `""` | Translation mode (`none` to skip) |
| `--raw` | `false` | Disable AI enrichment; store raw object |
| `--no-fanout` | `false` | Skip post-ingest fan-out enrichment |
| `--no-dedup` | `false` | Skip duplicate detection |
| `--source-key <key>` | `""` | External dedup key |
| `--wait` | `false` | Block until job completes |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` HTTP URL |

## Agent Recipes

```bash
# Stdin text with hints and mention, blocking until indexed
echo "Fix signup flow" | ctxt analyze --type text \
  --hint "ux,onboarding" --mention "@product.auth" --wait --format json

# Image file capture
ctxt analyze --file screenshot.png --type image --mention "@ui.layout" --wait

# Skip translation on non-English source
ctxt analyze --file notes-fr.md --language fr --translate none --wait

# Fast raw capture (no LLM call)
ctxt "raw scratch note" --raw --wait
```

## Gotchas & Next Steps

- Bare `ctxt ctxt://<id>` or `ctxt ctxt://search/<query>` dispatches the URI handler instead of `analyze`.
- Next: poll async job ([../dpkms/job.md](../dpkms/job.md)), inspect object ([show.md](show.md)), or search ([find.md](find.md)).
