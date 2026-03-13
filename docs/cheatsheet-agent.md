# ctxt Cheatsheet — Retrieval (Agent)

Quick reference for autonomous agents, scripts, and LLMs consuming the API or CLI.
Scannable in 30 seconds.

---

## Prerequisites

```bash
dpkms serve                          # REST :8080, gRPC :9090, background worker
curl http://127.0.0.1:8080/health    # → {"status":"ok"}
ctxt version                         # verify client
```

---

## Agent Loop Contract

```
1. Ingest      →  ctxt analyze / POST /analyze
2. Poll        →  ctxt job status <id> / GET /jobs/{id}  (gate on "completed")
3. Retrieve    →  ctxt list --q "<rsql>" / GET /objects?q=  (deterministic first)
4. Fallback    →  ctxt find "<nlq>" / GET /objects  (semantic only if step 3 insufficient)
5. Compose     →  ctxt make ... / POST /compose
```

**DO:** poll before compose. **DON'T:** compose immediately after ingest.
**DO:** use RSQL for repeatable queries. **DON'T:** rely on semantic search for deterministic workflows.

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

## Error Handling

| Condition | Handling |
|-----------|----------|
| Job stays `pending` | Check `dpkms serve` is running; `ctxt job list --state running` |
| Job `failed` | `ctxt job log <id>` → fix input → `ctxt job retry <id>` |
| RSQL returns empty | Remove one clause at a time; verify with `ctxt list --type <t>` |
| Registry latency | Separate local-first and federated runs in agent policy |
| Compose before job complete | Gate composition on `completed` status — never compose immediately after ingest |

---

## KnowledgeObject Data Model (Key Fields)

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | stable UUID |
| `type` | string | `text`, `url`, `image`, `audio`, `video`, `feed` |
| `subtype` | string | pipeline-assigned, optional |
| `summaries` | []string | AI-generated summaries |
| `tags` | []`{label, weight, source}` | assigned tags |
| `mentions` | []string | `@namespace.slug` references |
| `decisions` | []`{title, status, impact}` | extracted decisions |
| `pipeline` | string | pipeline that processed this object |
| `source` | string | origin (URL, file path) |
| `created_at` | ISO 8601 | ingestion timestamp |
| `updated_at` | ISO 8601 | last modification |
| `fts_indexed` | bool | full-text search ready |
| `vector_indexed` | bool | semantic search ready |
