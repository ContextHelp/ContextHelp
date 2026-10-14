# `ctxt capture`

`ctxt capture` sends one piece of content — a URL, a file, a string, stdin or the
clipboard — to the dpkms instance for ingestion. It is a sibling of
`ctxt analyze` (and of bare `ctxt`, which runs `analyze`); both stay available.
Its subcommands read the local browser: `tabs`, `history`, `schedule`,
`browsers`.

Source: `cmd/ctxt/cmd/capture.go`.

## Synopsis

```
ctxt capture [<source>] [flags]
ctxt capture --stdin [flags]
ctxt capture tabs|history|browsers [flags]
ctxt capture schedule install|uninstall|list
```

## Source resolution

| Input | Sent as |
|---|---|
| `http(s)://` URL | content = the URL, source = the URL |
| path to an existing file | content = the file, source = its absolute path |
| any other argument(s) | content = the arguments joined by spaces, source `argument` |
| `--stdin` | content = stdin, source `stdin`; cannot be combined with a positional source |
| nothing | the system clipboard, source `clipboard`; fails when the clipboard is empty, unsupported, or `CTXT_NO_CLIPBOARD` is set to any value |

The server's pipeline detector chain picks a pipeline from the type and source
(file extension, URL pattern, path prefix) unless `--pipeline` names one.

## Flags

| Flag | Effect |
|---|---|
| `--source <s>` | Replaces the source string sent to the server, e.g. to give stdin content a path the detector chain recognises. |
| `--type <t>` | `text` (default), `url`, `image`, `audio`, `video`, `feed`, `auto`. |
| `--pipeline <name>` | Skips detection and runs this pipeline. |
| `--inbox` | Stores the object in the inbox (`POST /api/v1/inbox`) instead of enqueueing a job; `--wait` does not apply. |
| `--every <duration>` | Repeats the capture on this cadence until Ctrl-C. Per-tick errors are printed and the loop continues. |
| `--hint <v>` | Hints attached to the object (repeatable, CSV). |
| `--mention <v>` | Mention targets (repeatable, CSV). |
| `--note <text>` | Audit note; sent as `inbox_note` with `--inbox`. |
| `--profile <name>` | Focus profile (inherited global flag). |
| `--raw` | Store the raw object; no AI steps. |
| `--no-fanout` | Skip post-ingest fan-out enrichment. |
| `--no-dedup` | Skip duplicate detection. |
| `--source-key <k>` | External dedup key (Slack ts, tweet ID, …). |
| `--wait` | Poll `GET /api/v1/jobs/<id>` until the job completes or fails. |
| `--server`, `--instance` | Pick the dpkms instance — see [`ctxt instance`](api-cli.md#ctxt-instance). |

Each request times out after 30 s. Without `--inbox` the command posts to
`POST /api/v1/analyze` and prints `Job ID: <id>`.

### Not implemented

`--ambient`, `--input`, `--skip` and `--window` are registered but exit 1 with
`ambient mode not yet implemented` (or `flag --<name> is part of ambient mode`).
No code reads `policy/ambient.yaml`, and there is no ambient daemon (`ctxd`).
The design is in [ADR-066](../decisions/ADR-066-ambient-capture-substrate.md)
and [../architecture/ambient-capture.md](../architecture/ambient-capture.md).

## Examples

```bash
ctxt capture https://example.com/post
ctxt capture https://hnrss.org/frontpage.rss --every 15m
ctxt capture ./notes.md --hint research --mention @project.alpha
ctxt capture ./meeting.m4a --inbox --note "client kickoff"
cat README.md | ctxt capture --stdin --source /vault/notes/readme.md
ctxt capture "thought I had" --wait
```

## Browser subcommands

| Command | What it does | Doc |
|---|---|---|
| `ctxt capture tabs` | Captures the open tabs of one Chromium profile | [browser-tab-capture.md](../manual/workflows/browser-tab-capture.md) |
| `ctxt capture history` | Captures new history entries since the last run | [browser-history-capture.md](../manual/workflows/browser-history-capture.md) |
| `ctxt capture schedule install` | Runs tabs + history on a timer (macOS LaunchAgent) | [browser-capture-schedule.md](../manual/workflows/browser-capture-schedule.md) |
| `ctxt capture browsers` | Lists installed Chromium browsers and profiles | `ctxt capture browsers --help` |

For clipboard and directory watching, see `ctxt watch --help` and
[../manual/workflows/ambient-capture.md](../manual/workflows/ambient-capture.md).

## See also

- [api-cli.md](api-cli.md) — `ctxt analyze`, `ctxt inbox`, `ctxt ingest`
- [policies.md](policies.md) — `policy/ctxt.yaml`
- [pipelines.md](pipelines.md) — pipeline runtime and detector chain
