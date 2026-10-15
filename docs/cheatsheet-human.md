# ctxt Cheatsheet — Capture (Human)

Quick reference for daily capture. Scannable in 30 seconds.

---

## Start

```bash
dpkms serve                          # start worker + REST/gRPC (keep running)
dpkms serve --daemon                 # detach from terminal (background daemon)
dpkms serve --name work              # named instance for multi-instance setups
ctxt status                          # health of the instance ctxt resolves (GET /healthz)
curl http://127.0.0.1:8080/health    # bare liveness probe: {"status":"ok"}
```

Config: `~/.config/contexthelp/ctxt.yaml` (ctxt), `~/.config/contexthelp/dpkms.yaml` (dpkms)

### Multiple instances

```bash
dpkms serve --name work     --port 8080 --data-dir ~/kb/work
dpkms serve --name personal --port 8081 --data-dir ~/kb/personal --daemon
dpkms ps                             # NAME  PID  PORT  GRPC  DB  UPTIME
ctxt instance use work               # persist active instance
ctxt instance list                   # * marks current
ctxt --instance personal stats       # per-call override
```

### Manage running instances

```bash
dpkms ps                             # list all running instances
dpkms shutdown                       # stop default instance (graceful drain)
dpkms stop --port 8081               # stop instance on port 8081
dpkms reboot                         # restart default instance (drain+exit; re-launch manually)
dpkms reboot --port 8081             # restart specific instance
```

---

## Capture

### Text

```bash
ctxt analyze "Decision: move to Postgres" --type text
echo "notes..." | ctxt analyze --type text
ctxt analyze --file ./meeting.md --type text
```

### URL

```bash
ctxt analyze https://example.com/postmortem --type url
ctxt feed add https://example.com/feed.xml         # subscribe
ctxt feed sync --url https://example.com/feed.xml  # fetch new items (nothing polls feeds on a timer)
```

### Image / Audio / Video

```bash
ctxt analyze --file ./wireframe.png  --type image
ctxt analyze --file ./standup.m4a   --type audio
ctxt analyze --file ./demo.mp4      --type video
```

### With context controls

```bash
ctxt analyze "Pricing experiment notes" --type text \
  --hint checkout,pricing \
  --mention "@project.checkout-redesign" \
  --profile growth \
  --pipeline text.long
```

| Flag | Purpose |
|------|---------|
| `--hint <tag>` | Tagging hints (repeatable, CSV) |
| `--mention "@ns.slug"` | Attach explicit entity references |
| `--profile <name>` | Apply focus profile (Founder, Engineer, …) |
| `--pipeline <name>` | Force a specific pipeline |
| `--wait` | Block until job completes; returns object ID |
| `--raw` | Store without AI enrichment |

---

## Local Capture (clipboard, directories, browser)

There is no ambient daemon: `ctxt capture --ambient` exits with "not yet implemented". These run today:

```bash
# Clipboard + directory watchers (watch.* in ctxt.yaml); blocks until Ctrl-C
ctxt watch enable clipboard
ctxt watch start
ctxt watch status
ctxt watch stop

# Browser (Chromium family)
ctxt capture tabs --browser brave         # open tabs of one profile
ctxt capture history --browser brave      # visits since the last run
ctxt capture schedule install             # tabs + history on a timer (macOS)
```

Full guide: [`ambient.md`](ambient.md#what-runs-today).

---

## Meeting Recordings

No recorder and no `ctxt capture meeting` command. Capture a recording made elsewhere as a file; the extension picks the pipeline:

```bash
ctxt capture ./standup.m4a --hint meeting   # audio.transcribe
ctxt capture ./demo.mp4 --wait              # video.full
```

Full guide: [`manual/workflows/meeting-capture.md`](manual/workflows/meeting-capture.md).

---

## Compose a Time Window

No work sessions and no `ctxt session` command. Compose over a creation-time range instead:

```bash
ctxt compose summary --since 2026-10-14
ctxt compose brief --since 2026-10-01 --until 2026-10-07 --tagged launch
```

---

## Agent Integration (MCP)

dpkms serves one read-only MCP endpoint, `/api/v1/mcp/`, with two tools: `search` and `schema`. There is no `ctxt mcp` command; register the URL in your client:

```bash
claude mcp add --transport http ctxt-graph http://127.0.0.1:8080/api/v1/mcp/
```

Full reference (tokens, tool arguments): [`manual/workflows/mcp-agents.md`](manual/workflows/mcp-agents.md).

---

## Inbox Triage

Items captured with deferred processing (`ctxt capture <source> --inbox`, or a client posting to `/api/v1/inbox`) land here.

```bash
ctxt inbox list                      # see what's waiting
ctxt inbox triage <id>               # enqueue for processing → returns job ID
ctxt inbox triage <id> --pipeline text.long  # force a pipeline
ctxt inbox discard <id> --confirm-token=<token>  # remove noise
ctxt inbox clear --confirm-token=<token>         # discard everything at once
```

---

## Import in Bulk

```bash
ctxt import chrome  --file bookmarks.html [--dry-run] [--max-items 200]
ctxt import safari  --file bookmarks.html
ctxt import firefox --file bookmarks.html

ctxt import pinboard  [--token <tok>] [--since 2026-01-01] [--tagged research]
ctxt import raindrop  --all [--token <tok>] [--since 2026-01-01]
ctxt import onedrive  --drive-id <id> [--since 2026-01-01T00:00:00Z] [--include-ext .pdf]
```

Batch from a URL list:

```bash
xargs -I{} ctxt analyze "{}" --type url < urls.txt
```

Batch from a directory of Markdown files:

```bash
find ./imports -name "*.md" -print0 | xargs -0 -I{} ctxt analyze --file "{}" --type text
```

---

## Track Jobs

`ctxt job` reads the jobs of the instance ctxt talks to, remote ones included. Cancel needs an admin token:

```bash
ctxt job list                        # recent jobs
ctxt job list --state failed         # failed only
ctxt job status <job_id>             # state, retries, error
ctxt job retry  <job_id>             # re-queue failed job
ctxt job cancel <job_id> --confirm-token=<token>
```

States: `pending` → `running` → `completed` / `failed` / `cancelled`

---

## Verify Capture

```bash
ctxt list                            # all recent objects
ctxt list --type url --after 2026-02-01 --limit 20
ctxt list --tagged checkout          # one tag
ctxt show <object_id>                # human-readable view
ctxt show <object_id> --format json  # full structured view
```

---

## Search

```bash
ctxt find "checkout conversion anomalies"            # hybrid FTS+vector (default)
ctxt find "incident patterns" --profile eng --limit 10
ctxt find "auth flow" --fts                          # FTS-only
ctxt find "checkout flow" --semantic                 # vector-only
ctxt find "indexing" --fts-weight 0.3 --vector-weight 0.7  # override RRF weights
ctxt find "onboarding" --explain                     # per-signal score breakdown
ctxt find "deployment" --graph                       # 3D graph of every candidate in the browser (Ctrl-C to stop)
ctxt find "deployment" --graph -o deployment.html    # save: .html .json .yaml .graphml .gexf
ctxt list --q "type==url;tag=in=(checkout,pricing)"  # structured (deterministic)
ctxt list --mention @project.checkout-redesign
ctxt list --q "related==@arch.decision"              # graph traversal (shared mention targets)
ctxt show obj_12345678                               # lists objects related through shared mentions
```

---

## Compose

```bash
ctxt compose brief   --tagged checkout --since 2026-02-01 --output-file brief.md
ctxt compose plan    --mention @project.checkout-redesign --since 2026-02-01
ctxt compose summary --tagged incident --since 2026-01-01
ctxt compose draft   --tagged launch --output-file post.md
ctxt compose brief   --tagged launch --export json   # structured citations
```

---

## At-a-glance Stats

```bash
ctxt stats                           # object counts, job states, feeds, profiles
ctxt stats --format json             # machine-readable snapshot
ctxt stats --watch                   # live refresh every 3s (Ctrl-C to exit)
```

---

## Common Tips and Failure Modes

| Symptom | Fix |
|---------|-----|
| Jobs stuck on `pending` | Confirm `dpkms serve` is running; check `ctxt instance current` |
| Stats from wrong DB | `ctxt instance use <name>` or `ctxt --instance <name> stats` |
| Object missing expected tags | Re-capture with `--hint` and `--mention` |
| Wrong type detected | Set `--type` explicitly |
| Captured object needs specific pipeline | Add `--pipeline text.long` (or matching name) |
| Verify what was captured | `ctxt show <id> --format json` |
| Feed not updating | `ctxt feed sync --url <feed-url>` (or `--id <feed-id>`) |
| Server won't stop | `dpkms shutdown --port <port>` or `dpkms ps` to find the right port |
