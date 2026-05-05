# ctxt Cheatsheet — Retrieval (Agent)

Quick reference for autonomous agents, scripts, and LLMs consuming the API or CLI.
Scannable in 30 seconds.

---

## Prerequisites

```bash
dpkms serve --name work --daemon     # REST :8080, gRPC :9090, detached daemon
curl http://127.0.0.1:8080/health    # → {"status":"ok"}
ctxt version                         # verify client

# Multiple instances: target by name or port
dpkms ps                             # list running instances (PID/PORT/GRPC/DB/UPTIME)
ctxt instance use work               # persist selection
ctxt --instance work stats           # per-call (overrides state file)
CTXT_INSTANCE=work ctxt stats        # env var (same precedence as flag)

# Lifecycle management
dpkms shutdown                       # graceful stop (drains in-flight jobs)
dpkms shutdown --port 8081           # stop specific instance
dpkms reboot                         # SIGHUP drain+exit (caller must re-launch)
```

---

## Agent Loop Contract

```
1. Ingest      →  ctxt analyze / POST /analyze
2. Poll        →  ctxt job status <id> / GET /jobs/{id}  (gate on "completed")
3. Retrieve    →  ctxt list --q "<rsql>" / GET /objects?q=  (deterministic first)
4. Fallback    →  ctxt find "<nlq>" / GET /objects  (hybrid FTS+vector; --fts or --semantic for single-mode)
5. Compose     →  ctxt make ... / POST /compose
```

**DO:** poll before compose. **DON'T:** compose immediately after ingest.
**DO:** use RSQL for repeatable queries. **DON'T:** rely on semantic search for deterministic workflows.

### Inbox Triage (deferred-capture path)

Items captured via mobile share, PWA, or human quick-dump land in inbox with `status=inbox`.
An agent may triage them to kick off pipeline processing:

```bash
ctxt inbox list --output json        # enumerate pending items
ctxt inbox triage <id>               # → {"job_id":"<id>"} — then poll as normal
ctxt inbox discard <id>              # mark noise
ctxt inbox clear                     # bulk-discard all

# REST equivalents
GET  /api/v1/inbox
POST /api/v1/inbox/{id}/triage       # body: {"pipeline":"<name>"}  (optional)
POST /api/v1/inbox/{id}/discard
```

---

## Ingest

```bash
# CLI
ctxt analyze "content or URL" --type text|url|image|audio|video|feed
ctxt analyze --file ./doc.md --type text --wait   # --wait blocks; returns object ID

# REST
curl -X POST http://127.0.0.1:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{"content":"...","type":"text"}'
# → {"job_id":"<id>"}
```

---

## Poll Jobs

```bash
ctxt job status <job_id>             # CLI
GET /jobs/{id}                       # REST
```

States: `pending` → `running` → `completed` | `failed` | `cancelled`

```bash
# Wait loop pattern
until ctxt job status "$JOB_ID" | grep -q completed; do sleep 2; done

# Retry on failure
ctxt job retry <job_id>
POST /jobs/{id}/retry
```

---

## Retrieve: Deterministic (RSQL) — Use First

```bash
ctxt list --q "<rsql-expression>" [--limit N]
GET /objects?q=<rsql>&limit=N
```

### RSQL Operators

| Operator | Meaning | Example |
|----------|---------|---------|
| `==` | equals | `type==url` |
| `!=` | not equals | `type!=text` |
| `=in=` | in set | `tag=in=(checkout,pricing)` |
| `=out=` | not in set | `type=out=(image,video)` |
| `>` `>=` | greater / gte | `created_at>2026-01-01` |
| `<` `<=` | less / lte | `created_at<=2026-03-01` |
| `;` | AND | `type==url;tag==checkout` |
| `,` | OR | `tag==checkout,tag==pricing` |
| `(` `)` | grouping | `(tag==a,tag==b);type==url` |

### Queryable Fields

| Field | Notes |
|-------|-------|
| `type` | `text`, `url`, `image`, `audio`, `video`, `feed` |
| `subtype` | pipeline-assigned subtype |
| `pipeline` | e.g. `text.short`, `text.long`, `url.generic` |
| `source` | origin URL or file path |
| `tag` | supports `==`, `!=`, `=in=` |
| `mention` | supports `==` only (`@namespace.slug`) |
| `related` | graph traversal: objects sharing mention targets (`related==@ns.slug`) |
| `similar` | FTS match (`similar=="keyword"`) |
| `created_at` | ISO 8601 date/datetime |
| `updated_at` | ISO 8601 date/datetime |

### Examples

```bash
# All URLs ingested after Feb 2026
ctxt list --q "type==url;created_at>2026-02-01"

# Objects tagged checkout OR pricing
ctxt list --q "tag=in=(checkout,pricing)" --limit 20

# Decisions mentioning a specific entity
ctxt list --q "mention==@project.checkout-redesign;type==text"

# Exclude noisy types
ctxt list --q "type=out=(image,video);tag==incident"

# FTS keyword match
ctxt list --q "similar==\"authentication anomaly\""
```

---

## Retrieve: Semantic Fallback

Use only when RSQL returns empty or confidence is low.

```bash
ctxt find "recent checkout architecture decisions" --limit 10
GET /objects?search=<nlq>&limit=10
```

---

## Inspect

```bash
ctxt open <object_id> --output json
GET /objects/{id}
```

---

## Entity Resolution

```bash
ctxt entity show <namespace.slug>
ctxt entity backlink <namespace.slug>
GET /entities/{slug}
GET /entities/{slug}/related
```

---

## Compose

```bash
ctxt make brief --tag checkout,funnel --since 2026-02-01 --output-file agent-brief.md
ctxt make plan  --mention @project.checkout-redesign --since 2026-02-01

POST /compose
Content-Type: application/json
{"type":"brief","tags":["checkout","funnel"],"since":"2026-02-01"}
```

Save artifact path **and** the source object IDs used — required for audit replay.

---

## System Stats

```bash
ctxt stats                           # objects, jobs, entities, feeds, profiles, reminders
ctxt stats --output json             # machine-readable (embed in monitoring workflows)
ctxt stats --watch                   # live 3s refresh
```

---

## Error Handling

| Condition | Handling |
|-----------|----------|
| Job stays `pending` | Check `dpkms serve` is running; `ctxt job list --state running` |
| Wrong instance targeted | `ctxt instance current`; use `--instance <name>` or `ctxt instance use` |
| Job `failed` | `ctxt job log <id>` → fix input → `ctxt job retry <id>` |
| RSQL returns empty | Remove one clause at a time; verify with `ctxt list --type <t>` |
| Registry latency | Separate local-first and federated runs in agent policy |
| Compose before job complete | Gate composition on `completed` status — never compose immediately after ingest |

---

## KnowledgeObject Data Model (Key Fields)

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | stable UUID |
| `type` | string | `text`, `url`, `image`, `audio`, `video`, `feed`, `meeting` |
| `subtype` | string | pipeline-assigned, optional |
| `summaries` | []string | AI-generated summaries |
| `tags` | []`{label, weight, source}` | assigned tags |
| `mentions` | []string | `@namespace.slug` references |
| `decisions` | []`{title, status, impact}` | extracted decisions |
| `status` | string | `inbox`, `active`, `discarded` |
| `inbox_note` | string | capture-time note (inbox items only) |
| `pipeline` | string | pipeline that processed this object |
| `source` | string | origin (URL, file path, ambient source name) |
| `session_id` | string | optional; populated for ambient-captured items (per ADR-067); soft-FK to `sessions` |
| `created_at` | ISO 8601 | ingestion timestamp |
| `updated_at` | ISO 8601 | last modification |
| `fts_indexed` | bool | full-text search ready |
| `vector_indexed` | bool | semantic search ready |

---

## MCP Read-Surface (For AI Agents)

ctxt exposes two MCP servers (per ADR-068). If you (the agent) are running on the user's machine, attach to **both**. If you're running elsewhere, attach to dpkms only.

### dpkms-side server — authoritative graph

Endpoint: `http://<dpkms-host>/api/v1/mcp/` (default localhost). 10 read-only tools.

| Tool | Args | Use when |
|---|---|---|
| `search(query, top_k=10, since?, until?, profile?, kinds?)` | hybrid FTS+vector | Specific keywords (person, project, error, file path) |
| `list(kind, filter?, limit=20, profile?)` | rows of `kind` | Browsing by type |
| `get(id, profile?)` | one KnowledgeObject | After search/list pinpoints it |
| `entity(slug, profile?)` | entity + facts + backlinks | Resolving `@person.alice` or `@project.q3` |
| `recent(since='today', limit=20, kind?, profile?)` | newest-first feed | "What's new / what has the user been up to?" |
| `sessions(since?, until?, limit=20, profile?)` | recent work sessions | "Show recent sessions" |
| `session(id, profile?)` | session metadata + items | After `sessions` points at one |
| `compose(template, scope?, profile?)` | rendered document | Synthesis: "summarize my last X" |
| `mentions(target, depth=1, profile?)` | objects referencing target | Graph traversal from entity or object |
| `schema()` | storage taxonomy | Constructing structured queries |

### ctxd-side server — local-only, live state

Endpoint: `http://127.0.0.1:8744/mcp`. 5 read-only tools surfacing state dpkms cannot see when remote.

| Tool | Args | Use when |
|---|---|---|
| `current_session()` | active session metadata, or `null` | "What is the user working on right now?" |
| `recent_local(limit=20, source?)` | most recent ambient events | Last-30-seconds questions; beats `recent()` for in-flight events |
| `pending_enqueue()` | events buffered awaiting dpkms | "Is dpkms reachable? What's stuck?" |
| `sources()` | active ambient sources + last-event timestamps | Diagnostics |
| `health()` | daemon status, buffer size, retention state | Diagnostics |

### Default flow

The user asks "what was I doing this afternoon?":

1. `current_session()` — if active and started before the asked time, return its metadata + first 5 items
2. Else `sessions(since="today")` and find the matching one
3. `session(id=...)` to get the items
4. Synthesize natural-language answer

The user asks "summarize yesterday's Q3 meeting":

1. `search(query="Q3", kinds=["meeting"], since="yesterday")`
2. `get(id=<top result>)` for the transcript
3. `compose(template="meeting-recap", scope="object:<id>")`

### Server-level instructions

Both servers pass an `instructions` string at connect-time emphasizing: **CALL THESE TOOLS FIRST** when the user asks about themselves, their work, their recent context — prefer this graph over replying "I don't know."

### Bus event observability

Every tool call emits `dpkms.mcp.tool.invoked|completed|failed` (or `ctxt.mcp.*` for ctxd-side). Useful for "did the meeting transcript land yet?" workflows: subscribe to `ctxt.ambient.meeting.transcript_ready`.

### Read-only

No tool mutates. Writes go through the existing `ctxt analyze` enqueue path (or its HTTP equivalent `POST /api/v1/analyze`). If the user wants the agent to ingest something, use that path — not MCP.

Full reference: [`manual/workflows/mcp-agents.md`](manual/workflows/mcp-agents.md).
