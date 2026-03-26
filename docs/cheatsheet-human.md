# ctxt Cheatsheet — Capture (Human)

Quick reference for daily capture. Scannable in 30 seconds.

---

## Start

```bash
dpkms serve                          # start worker + REST/gRPC (keep running)
ctxt version                         # verify client is connected
curl http://127.0.0.1:8080/health    # optional health probe
```

Config: `~/.config/contexthelp/config.yaml`

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
ctxt analyze https://example.com/feed.xml --type feed --fetch-new
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
  --hints "#checkout #pricing" \
  --mentions "@project.checkout-redesign" \
  --profile growth \
  --pipeline text.long
```

| Flag | Purpose |
|------|---------|
| `--hints "#tag"` | Influence AI tagging |
| `--mentions "@ns.slug"` | Attach explicit entity references |
| `--profile <name>` | Apply focus profile (Founder, Engineer, …) |
| `--pipeline <name>` | Force a specific pipeline |
| `--wait` | Block until job completes; returns object ID |
| `--raw` | Store without AI enrichment |

---

## Inbox Triage

Items captured with deferred processing (e.g. via mobile share, PWA, or `--inbox`) land here.

```bash
ctxt inbox list                      # see what's waiting
ctxt inbox triage <id>               # enqueue for processing → returns job ID
ctxt inbox triage <id> --pipeline text.long  # force a pipeline
ctxt inbox discard <id>              # remove noise
ctxt inbox clear                     # discard everything at once
```

---

## Import in Bulk

```bash
ctxt import chrome  --file bookmarks.html [--dry-run] [--max-items 200]
ctxt import safari  --file bookmarks.html
ctxt import firefox --file bookmarks.html

ctxt import pinboard  [--token <tok>] [--since 2026-01-01] [--tag research]
ctxt import raindrop  --all [--token <tok>] [--since 2026-01-01]
ctxt import onedrive  --drive-id <id> [--since 2026-01-01] [--include-ext .pdf]
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

```bash
ctxt job list                        # recent jobs
ctxt job list --state failed         # failed only
ctxt job status <job_id>             # single job state
ctxt job log    <job_id>             # execution log
ctxt job retry  <job_id>             # re-queue failed job
ctxt job cancel <job_id>
```

States: `pending` → `running` → `completed` / `failed` / `cancelled`

---

## Verify Capture

```bash
ctxt list                            # all recent objects
ctxt list --type url --after 2026-02-01 --limit 20
ctxt list --tag checkout,conversion
ctxt open <object_id>                # human-readable view
ctxt open <object_id> --output json  # full structured view
```

---

## Search

```bash
ctxt find "checkout conversion anomalies"            # hybrid FTS+vector (default)
ctxt find "incident patterns" --profile eng --limit 10
ctxt find "auth flow" --fts                          # FTS-only
ctxt find "checkout flow" --semantic                 # vector-only
ctxt find "indexing" --fts-weight 0.3 --vector-weight 0.7  # override RRF weights
ctxt list --q "type==url;tag=in=(checkout,pricing)"  # structured (deterministic)
ctxt list --mention @project.checkout-redesign
ctxt list --q "related==@arch.decision"              # graph traversal (shared mention targets)
ctxt open obj_12345678                               # See Also section shows related objects
```

---

## Compose

```bash
ctxt make brief   --tag checkout,funnel --since 2026-02-01 --output-file brief.md
ctxt make plan    --mention @project.checkout-redesign --since 2026-02-01
ctxt make summary --tag incident --since 2026-01-01
ctxt make draft   --tag blog,launch --output-file post.md
```

---

## Common Tips and Failure Modes

| Symptom | Fix |
|---------|-----|
| Jobs stuck on `pending` | Confirm `dpkms serve` is running |
| Object missing expected tags | Re-ingest with `--hints` and `--mentions` |
| Wrong type detected | Set `--type` explicitly |
| Captured object needs specific pipeline | Add `--pipeline text.long` (or matching name) |
| Verify what was captured | `ctxt open <id> --output json` |
| Feed not updating | Use `--fetch-new` flag when analyzing the feed URL |
