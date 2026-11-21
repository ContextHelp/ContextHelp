# Workflow: Capture a URL, File, Stdin or Clipboard

## Goal

Send one thing to your ctxt server with `ctxt capture`, let the server pick the pipeline, and know what the pick will be.

## Scope

- One source per run: a URL, a file, a quoted string, stdin, or the clipboard
- Re-capturing a source on a timer (`--every`)
- Sending to the inbox for triage instead of processing now (`--inbox`)
- Not covered: browser tabs and history ([tabs](./browser-tab-capture.md), [history](./browser-history-capture.md)), watching folders and the clipboard ([watch](./watch.md)), bulk imports ([ingestion](./ingestion-capture.md))

`ctxt capture --ambient`, `--input`, `--skip` and `--window` are reserved. They fail with an error saying ambient mode is not implemented.

## Prerequisites

1. A dpkms instance your client resolves (`--instance`, `ctxt instance use`, else the first `server.urls` entry), with a token that may write (`writer` or `admin`). Nothing is queued on this machine.
2. A server that can run the pipeline you need. Text, URL and document pipelines need the providers they use configured on the server.

## Procedure

### Step 1: Pick the source

| You run | Source |
|---|---|
| `ctxt capture https://example.com/post` | the URL; the server fetches it |
| `ctxt capture ./notes.md` | the file's contents; its absolute path is the source |
| `ctxt capture "a quoted string"` | the string itself |
| `cat README.md \| ctxt capture --stdin` | stdin |
| `ctxt capture` | the clipboard |

An argument counts as a URL when it parses with an `http` or `https` scheme. Otherwise, if it names an existing file, the file is read. Anything else is sent as literal text. `--stdin` cannot be combined with a positional source. With no source, no `--stdin` and an empty or unavailable clipboard, the command fails and tells you to pass one of them; `CTXT_NO_CLIPBOARD=1` turns the clipboard fallback off.

A binary file (an image, a PDF) or binary stdin is sent base64-encoded and stored as the file's exact bytes; text is sent as it is. Base64 adds about a third to the request, which counts against `server.max_analyze_bytes` on a protected or public instance.

To label content that arrives on stdin, pass `--source <path-or-url>`. The server treats that value like the source of a positional capture, so `--source ./notes/plan.md` makes piped text route as a Markdown file.

### Step 2: Capture

```bash
ctxt capture https://example.com/post
```

```text
Job ID: job_1
```

The job runs in the background. Add `--wait` to block until it finishes; the command exits non-zero if the job fails.

```bash
ctxt capture https://example.com/post --wait
ctxt show <object-id>        # inspect the result once the job is done
```

### Step 3: Shape the capture (optional)

| Flag | Effect |
|---|---|
| `--hint <text>` | add a hint; repeatable |
| `--mention @entity.slug` | tie the capture to an entity; repeatable |
| `--note <text>` | audit note recorded with the capture |
| `--profile <name>` | file the capture under a focus profile |
| `--pipeline <name>` | skip auto-routing and force one pipeline |
| `--type text\|url\|image\|audio\|video\|feed\|auto` | input type; default `text` |
| `--raw` | no AI: store the content as a raw object |
| `--no-fanout` | skip enrichment that runs after ingest |
| `--no-dedup` | do not drop the capture as a duplicate |
| `--source-key <key>` | your own dedup key (a Slack timestamp, a tweet ID) |

`--type` stays `text` unless you change it. The server still treats text that starts with `http` as a URL. You rarely need `--type`: pass it when the content is not a URL and not a file with a telling extension.

### Step 4: Send to the inbox instead (optional)

```bash
ctxt capture ./meeting.m4a --inbox
```

```text
Inbox object: obj_1
```

The object is stored in the inbox for later triage and no pipeline runs now. No job exists, so `--wait` has nothing to wait for and is ignored. `--pipeline`, `--raw`, `--no-fanout`, `--no-dedup` and `--source-key` are not sent on this path.

### Step 5: Repeat on a timer (optional)

```bash
ctxt capture https://hnrss.org/frontpage.rss --every 15m
```

The first capture runs at once, then every 15 minutes until you press Ctrl-C. A failed round prints `capture: <error>` on stderr and the loop continues. Each request times out after 30 seconds.

## How the server picks the pipeline

When you give no `--pipeline`, the server routes by two separate signals. The source (a URL or a file path) is read first; the content is read only when the source decides nothing.

### URLs

| URL shape | Pipeline |
|---|---|
| `github.com/<owner>/<repo>` | `url.github.repo` |
| `github.com/<owner>/<repo>/issues/<n>` | `url.github.issue` |
| `github.com/<owner>/<repo>/pull/<n>` | `url.github.pr` |
| `github.com/<owner>/<repo>/releases...` | `url.github.release` |
| `github.com/<owner-or-org>` | `url.github.profile` |
| `gitlab.com/<owner>/<repo>`, `bitbucket.org/<owner>/<repo>` | `url.repo` |
| any other `http` or `https` URL | `url.generic` |

Sub-paths of a repository that are not in the table (commits, files, wiki) fall through to `url.generic`. The pipelines `url.interactive` and `url.authenticated` (browser-driven fetch, with imported cookies for the second) are never picked automatically; request them with `--pipeline`.

The browser captures use the same routing: each allowed tab or history entry is sent as a URL capture.

### Files

A file's extension picks the pipeline:

| Extension | Pipeline |
|---|---|
| `.png .jpg .jpeg .webp .tiff .tif .bmp .gif` | `image.ocr` |
| `.mp3 .wav .ogg .flac .m4a` | `audio.transcribe` |
| `.mp4 .mov .avi .mkv .webm` | `video.full` |
| `.pdf` | `doc.pdf` |
| `.md .markdown` | `doc.markdown` |
| `.docx` | `doc.office` |
| `.go .py .js .ts .rs .java .rb .cpp .c .cs` | `doc.code` |
| `.csv`, `.tsv`, `.jsonl`, `.ndjson` | `batch.csv`, `batch.tsv`, `batch.jsonl` |
| `.eml`, `.mbox` | `email.newsletter`, `email.general` |

### Text and everything else

A string, stdin, the clipboard, and a file with another extension (`.txt`, `.log`) route by what they hold:

- 500 characters or more, or Markdown structure (a heading, or five or more bullets): `text.long`
- shorter: `text.short`

To see the pipelines your server has, and what each does, run `dpkms pipeline list` on the server host. To change one pipeline's steps, see [Pipeline Management](../admin-extensibility/pipeline-management.md).

## Outputs to validate

- `Job ID:` (or `Inbox object:` with `--inbox`) is printed
- After `--wait`, `ctxt show <object-id>` shows the object, and `ctxt find` returns it
- A URL capture of a repository landed in the pipeline you expected (`ctxt job status <job_id>`)

## Common failure modes

### `cannot combine --stdin with a positional source`

Pass one or the other.

### `no input provided (use a positional source, --stdin, or copy something to the clipboard)`

Nothing was passed and the clipboard is empty. Pass a source.

### `no pipeline registered for this content type`

The pipeline you named with `--pipeline` (or the type you passed) does not exist on the server. Check `dpkms pipeline list`.

### The capture is silently treated as a duplicate

Exact duplicates are dropped. Pass `--no-dedup` to force a new object. A capture that carries `--source-key` or `--note` updates the stored object instead.

### `ambient mode not yet implemented`

`--ambient`, `--input`, `--skip` and `--window` are reserved. Use [`ctxt watch`](./watch.md) and [`ctxt capture tabs|history`](./browser-tab-capture.md).

## Related references

- [`ingestion-capture.md`](./ingestion-capture.md) (`ctxt analyze`, imports)
- [`watch.md`](./watch.md)
- [`browser-tab-capture.md`](./browser-tab-capture.md), [`browser-history-capture.md`](./browser-history-capture.md)
- [`../reference/api-cli-reference.md`](../reference/api-cli-reference.md)
