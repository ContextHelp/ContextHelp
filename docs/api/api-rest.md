# REST API

This document specifies the ContextHelp REST API: available endpoints, request/response formats, query parameters, conventions, and **mention-aware** semantics. The REST API provides a thin, consistent interface over the job system, pipelines, storage, query engine, registries, entity-resolution subsystems, and **plugin-augmented API surfaces**.

Plugins may extend the API through documented extension points without modifying core functionality.

## Principles

- Local-first API surface.
- Job-based ingestion for deterministic processing.
- Write path mediated through durable jobs.
- Read path supports multi-source retrieval.
- Extended RSQL Query Language with **mention operators**.
- Entities, mentions, hints, and tags are independent semantic layers.
- I18N/L10N optional and plugin-driven but exposed consistently.
- Plugins may:
  - add new knowledge object types
  - attach plugin metadata to knowledge objects
  - inject supplemental data into REST responses
  - define new endpoints under `/plugins/*`

## Base URL

Default:

- `http://127.0.0.1:8080`

Configurable in ContextHelp settings.

## Authentication

Supported modes:

- API key via `Authorization: Bearer <token>` or `X-API-Key: <token>`
- Optional mTLS
- Optional reverse-proxy auth integration

Private instances (loopback, no `server.auth`) accept requests without a token. On protected and public instances:

- Every `/api/v1` route requires one scope (`read:objects`, `write:objects`, `delete:objects`, `process:inbox`, `admin:audit`, ...). A token's roles (`admin`, `writer`, `reader`) expand to its scopes. Route-to-scope table: [config-and-permissions](../manual/reference/config-and-permissions.md#inbound-auth-roles-and-scopes).
- A missing or invalid token gets 401 `UNAUTHORIZED`.
- A valid token without the route's scope gets 403 `INSUFFICIENT_SCOPE`, with `details.required_scope` and a `WWW-Authenticate: Bearer error="insufficient_scope", scope="..."` header (RFC 6750).
- `GET /api/v1/whoami` returns the caller:

```json
{"principal": "phone", "provider": "static", "roles": ["writer"], "scopes": ["read:objects", "write:objects", "..."]}
```

## Content Types

- JSON: `application/json`
- UTF-8 for all text

## Error Format

```json
{
  "error": {
    "code": "INVALID_QUERY",
    "message": "Failed to parse query: unexpected token",
    "details": {}
  }
}
```

Common error codes:

- `INVALID_QUERY`
- `INVALID_MENTION`
- `INVALID_PARAM` (400; `details.param` names the query parameter)
- `UNKNOWN_ENTITY`
- `VALIDATION_FAILED`
- `NOT_FOUND`
- `UNAUTHORIZED` (401)
- `INSUFFICIENT_SCOPE` (403)
- `INTERNAL_ERROR`
- `PLUGIN_ERROR` (plugin-defined error states surfaced via API)

## Conventions

- All timestamps are ISO-8601 UTC.
- IDs are opaque strings.
- Pagination uses `limit` + `offset`.
- Boolean parameters use `true`/`false`.
- Plugins may add fields to responses under:
  - `plugins.<pluginName>.{...}`
- Plugins may add new routes under:
  - `/plugins/{pluginName}/...`

## Reserved Routes

- `/admin/v1/*` is reserved for the **Node Admin API** (used by context.help cloud and other admin clients).
- `/plugins/*` is reserved for plugin-defined routes.

The Node Admin API is documented separately in `docs/api/node-admin-api.md`.

## Mention Semantics in the REST Layer

Mentions appear in three places:

- In **ingestion**, via user-supplied `@entities` in the `mentions` field (legacy `[]string` accepted for backward compatibility).
- In **knowledge objects**, via a `mention_uris` array of `ctxt://entity/<namespace>/<slug>` URIs.
- In **querying**, via mention operators in `q` expressions (`mention==ui.best-practice`, `mention==stripe.api.*`).

Mentions do not influence hints or tags and are resolved through the Entity Registry.

---

## Endpoints Overview

Core endpoints:

- `GET /health`
- `POST /analyze`
- `GET /jobs`
- `GET /jobs/{id}`
- `POST /jobs/{id}/retry`
- `GET /objects`
- `GET /objects/facets`
- `GET /objects/{id}`
- `GET /objects/{id}/related`
- `PATCH /objects/{id}`
- `DELETE /objects/{id}`
- `POST /find`
- `GET /search/graph`
- `GET /profiles`
- `GET /profiles/{name}`
- `POST /compose`
- `GET /registries`
- `GET /registries/{id}`
- `GET /entities`
- `GET /entities/{slug}`
- `GET /entities/{slug}/related`
- `GET /suggest/tags` (optional)
- `GET /suggest/mentions` (optional)

Plugin endpoints:

- `/plugins/*` namespace is reserved for plugin-defined routes.
- Plugins may expose read-only or read-write API surfaces.

Plugins may also augment core responses with:

```
"plugins": {
  "<pluginName>": { ... }
}
```

---

## Health

### `GET /health`

#### Response

```json
{
  "status": "ok",
  "version": "0.1.0",
  "uptimeSeconds": 12345,
  "storage": {
    "backend": "sqlite",
    "status": "ok"
  },
  "jobs": {
    "workerRunning": true
  },
  "plugins": {
    "notifications": {
      "pending": 2
    }
  }
}
```

Plugins may inject diagnostic information.

---

## Ingestion: Analyze

### `POST /analyze`

Enqueue a job that runs a full enrichment pipeline.
Supported for both core and plugin-defined bookmark types.

#### Request

```json
{
  "type": "text",
  "content": "Fix signup flow friction",
  "hints": ["#ux", "#bad"],
  "mentions": ["@ui.best-practice", "@stripe.api.checkout"],
  "profile": "founder",
  "pipeline": "auto",
  "language": "en",
  "skipTranslation": false,
  "plugins": {
    "price_monitor": {
      "threshold_percent": 10
    }
  }
}
```

Notes:

- The `mentions` field accepts `@namespace.slug` strings for backward compatibility. The pipeline converts them to `ctxt://entity/namespace/slug` URIs stored as `mention_uris`.
- Mentions are **not resolved here**; resolution happens in the pipeline.
- Plugins may hook ingestion via `plugins.<pluginName>` blocks.
- Plugins may inject additional processing steps via configured hooks.

#### Response

```json
{
  "jobId": "job_123",
  "status": "pending"
}
```

---

## Jobs

### `GET /jobs`

Query Parameters:

- `status`
- `type`
- `limit`
- `offset`
- `plugin` (return jobs created by specific plugin)

#### Response

```json
{
  "jobs": [
    {
      "id": "job_123",
      "type": "analyze",
      "status": "completed",
      "createdAt": "2025-01-01T12:00:00Z",
      "updatedAt": "2025-01-01T12:00:02Z",
      "input": {
        "type": "text",
        "hints": ["#ux", "#bad"],
        "mentions": ["@ui.best-practice"]
      },
      "mention_uris": ["ctxt://entity/ui/best-practice"],
      "resultObjectId": "obj_abc",
      "plugins": {
        "rss_feed": {
          "derivedItems": 2
        }
      }
    }
  ],
  "limit": 50,
  "offset": 0,
  "total": 1
}
```

Plugins may attach job metadata.

---

## Knowledge Objects

Knowledge objects include **tags**, **hints**, **mentions**, and **plugin metadata**.

### `GET /objects`

Lists the objects that match a filter, one page at a time. Scope: `read:objects`.

#### Query Parameters

Every parameter is optional and takes exactly one non-empty value. An unknown, repeated, empty or malformed parameter is a 400 `INVALID_PARAM` whose `details.param` names it. Filters combine with AND.

| Parameter | Matches |
|---|---|
| `type`, `subtype` | the object's type or subtype |
| `tag` | objects carrying the tag label |
| `mention` | objects mentioning the entity, as `@ns.slug` or `ctxt://entity/...` |
| `pipeline` | the pipeline that produced the object |
| `status` | `active` (the default), `inbox`, `discarded`, `raw`, or `all` for every status |
| `after`, `before` | `created_at` at or after / at or before the instant: RFC 3339 (`2026-02-10T09:00:00Z`) or a date (`2026-02-10`, midnight UTC) |
| `meta_type` | `metadata.type` |
| `topic`, `person` | an element of `metadata.topics` / `metadata.people` |
| `source_type` | `metadata.source_type` |
| `since`, `until` | a `metadata.dates_mentioned` date on or after / on or before the date (`YYYY-MM-DD` only) |

Order and paging:

| Parameter | Values | Default |
|---|---|---|
| `sort` | `created_at`, `updated_at` | `created_at` |
| `dir` | `asc`, `desc` | `desc` |
| `limit` | integer >= 0; `0` returns every match | `20` |
| `offset` | integer >= 0 | `0` |

RSQL queries go to `GET /search?q=`; `q` is not a parameter here.

#### Response

`total` counts every match; `data` holds one page.

```json
{
  "data": [
    {"id": "o-a", "type": "article", "status": "active", "tags": [{"label": "ux"}], "created_at": "2026-01-10T09:00:00Z", "updated_at": "2026-06-01T09:00:00Z"}
  ],
  "total": 1
}
```

### `GET /objects/facets`

Counts the objects that match a filter by `metadata.type`. Objects without one count under `(none)`. Scope: `read:objects`.

Takes the filter parameters of `GET /objects` (`type` through `until`), with the same validation. `limit`, `offset`, `sort` and `dir` are rejected: counts cover every match.

```json
{"data": {"observation": 1, "task": 1, "(none)": 1}}
```

No match returns `{"data": {}}`.

### `GET /objects/{id}/related`

Objects that share a mention target with the object, following shared targets up to `depth` hops. The object itself is never included. Scope: `read:objects`.

| Parameter | Values | Default |
|---|---|---|
| `depth` | 1 to 3 | `1` |
| `limit` | 1 to 100 | `10` |

Any other parameter is a 400 `INVALID_PARAM`. An unknown `{id}` is a 404 `NOT_FOUND`.

```json
{"data": [{"id": "o-b", "type": "note", "...": "..."}]}
```

### Plugin Metadata

```
"plugins": {
  "rss_feed": {
    "feedUrl": "...",
    "guid": "..."
  },
  "price_monitor": {
    "current_price": 1199.00,
    "threshold_percent": 10
  }
}
```

---

## Search: Find

### `POST /find`

Full-text, vector or hybrid search, with the diagnostics `ctxt find` prints and, on request, a per-result score breakdown and facet counts. Scope: `read:objects`. REST only; gRPC has no equivalent method.

dpkms embeds the query itself, with the default embedding model's provider as configured on the dpkms host. The client doesn't embed anything. The caller resolves its profile and search strategy and sends the resulting values (ADR-077 §6).

#### Request

```json
{
  "query": "rotating signing keys",
  "mode": "hybrid",
  "limit": 10,
  "profile": "work",
  "filter": {
    "meta_type": "task",
    "topic": "security",
    "person": "alice-chen",
    "source_type": "slack",
    "since": "2026-04-01",
    "until": "2026-04-30"
  },
  "search": {
    "rrf_k": 60,
    "fts_weight": 0.5,
    "vector_weight": 0.5,
    "fts_pool": 50,
    "vector_pool": 50,
    "min_score": 0,
    "fallback_to_fts": true,
    "mention_boost_per_mention": 0.05,
    "max_mention_boost": 1.0,
    "direct_backlink_boost": 0.08,
    "hop_backlink_boost": 0.03
  },
  "explain": false,
  "facets": false
}
```

- `query` is required.
- `mode` is `fts`, `vector` or `hybrid`. It defaults to `hybrid`.
- `limit` defaults to 10.
- `profile` restricts both legs and the facet counts to objects owned by that profile. Leave it out for no profile filter, as when listing objects. The caller picks the profile; any token with `read:objects` can name any profile, so it scopes a search and is not access control.
- `filter` and each of its fields are optional. `since` and `until` are dates (`YYYY-MM-DD`), compared with an object's `dates_mentioned`.
- In `search`, any knob you leave out takes the built-in default shown above, which is also ctxt's config default. `rrf_k` and the pools must be positive. The weights, `min_score` and the boosts must not be negative. `min_score` applies to hybrid results.
- `explain` applies to `hybrid` mode only. Other modes ignore it.
- `facets` counts metadata types over every object `filter` matches, whatever the `limit`.
- An unknown field gets 400, so a misspelled knob never silently falls back to its default.

#### Response

```json
{
  "query": "rotating signing keys",
  "mode": "hybrid",
  "objects": [{"id": "o_123", "type": "text", "metadata": {"rrf_score": 0.0164}}],
  "total": 1,
  "diagnostics": {
    "candidate_count": 3,
    "below_threshold_count": 0,
    "threshold": 0,
    "staleness_warning": {"count": 1, "reason": "1 objects in this result set are pending pipeline upgrade — run 'ctxt upgrade plan' to see what's affected"},
    "semantic": {"status": "ok", "model_id": "arctic"}
  },
  "explain": [
    {
      "id": "o_123",
      "score_breakdown": {"fts": 0.0082, "vector": 0.0082, "mention_boost": 0, "graph_relevance": 0, "word_overlap": 0, "total": 0.0164},
      "document_view": {"title": "Key rotation runbook"}
    }
  ],
  "facets": {"task": 1, "(none)": 2}
}
```

- `objects` lists the results, best first. It is `[]`, never `null`, when nothing matched. In hybrid mode without `explain`, each object's `metadata.rrf_score` is its total score. In vector mode, `metadata.score` is its similarity.
- `diagnostics` is zero-valued, with no `semantic` block, in `fts` mode:
  - `candidate_count` counts the objects either leg returned before the threshold.
  - `below_threshold_count`, `top_below_threshold_score` and `threshold` describe the candidates `min_score` dropped. When there are no results but `below_threshold_count` is positive, everything that matched fell below the threshold.
  - `staleness_warning` appears when hybrid candidates are pending a pipeline upgrade.
  - `semantic.status` is `ok`, `no_default_model`, `provider_error`, `dimension_mismatch`, `index_missing` or `low_coverage`. Any status except `ok` comes with a `detail` and a one-line `notice`, and means the results are full-text only.
- `explain` has one entry per object, in the same order. It is present only for `hybrid` with `explain: true`.
- `facets` maps metadata type to count, with `(none)` for objects without one. It is present only when requested.

#### Errors

| Status | Code | When |
|---|---|---|
| 400 | `INVALID_REQUEST` | The body isn't valid JSON, has an unknown field, lacks `query`, or has a bad `mode`, date or knob |
| 401 | `UNAUTHORIZED` | The token is missing or invalid (protected and public instances) |
| 403 | `INSUFFICIENT_SCOPE` | The token lacks `read:objects` |
| 422 | `SEMANTIC_UNAVAILABLE` | The request sets `fallback_to_fts: false` and the semantic leg can't run. `details.status` gives the reason, and `details.model_id` the model when there is one |

With `fallback_to_fts` on (the default), a semantic leg that can't run, for example because the embedding provider is down, doesn't fail the search. Vector and hybrid modes answer full-text only and report the reason in `diagnostics.semantic`.

The MCP `search` tool runs this search for agents; see [MCP agents](../manual/workflows/mcp-agents.md).

### `GET /search/graph`

Served by dpkms at `/api/v1/search/graph`. Returns the hybrid search trace as a graph: every candidate the search scored (returned, cut by `limit`, cut by `min_score`) with its scores, the entities those candidates mention, and the links between them. It is the document `ctxt find "<q>" --graph --format json` prints, built by the same pipeline; see the [search graph workflow](../manual/workflows/search-graph.md) for how to read it.

```
GET /api/v1/search/graph?q=signup+friction&profile=work&max_nodes=100&similar=true
```

`200`, `Content-Type: application/json`. The body is the bare [JGF v2.1](https://jsongraphformat.info/) single-graph document, **not** the `{data, total}` envelope:

```json
{
  "graph": {
    "id": "search-graph",
    "type": "ctxt.search-graph",
    "label": "signup friction",
    "directed": true,
    "metadata": {
      "vocabulary": "ctxt.search-graph/v1",
      "mode": "hybrid",
      "semantic_status": "ok",
      "limit": 10,
      "counts": { "candidates": 12, "returned": 10, "nodes": 19, "edges": 41, "entities": 6, "...": "..." },
      "truncated": false,
      "caps": { "max_nodes": 100, "max_edges": 1500 },
      "...": "..."
    },
    "nodes": {
      "query": { "label": "signup friction", "metadata": { "kind": "query" } },
      "obj:6a8cf2a4-…": { "label": "Signup funnel review", "metadata": { "kind": "object", "stage": "returned", "rank": 1, "...": "..." } },
      "ent:ux.signup": { "label": "Signup", "metadata": { "kind": "entity", "slug": "ux.signup", "mention_count": 3 } }
    },
    "edges": [
      { "source": "query", "target": "obj:6a8cf2a4-…", "relation": "matched", "directed": true, "metadata": { "derivation": "derived", "weight": 0.0164 } }
    ]
  }
}
```

Keys and relations are defined by the `ctxt.search-graph/v1` vocabulary ([workflow key table](../manual/workflows/search-graph.md#useful-keys)). Every string, labels included, is untrusted text: escape it before rendering as HTML.

#### Parameters

| Param | Default | Meaning |
|---|---|---|
| `q` (required) | — | free-text query |
| `profile` | none | only this profile's objects |
| `limit` | `10` | results the search returns; candidates past it stay in the graph as `cut_limit` |
| `min_score` | `0` | results scoring below it stay in the graph as `cut_threshold` |
| `meta_type`, `topic`, `person`, `source_type`, `since`, `until` | none | the filters of `POST /find`; `since` and `until` are `YYYY-MM-DD` |
| `max_nodes` | `250` | node cap, query node included; `1`–`1000` |
| `max_edges` | `1500` | edge cap; `1`–`10000` |
| `similar` | `false` | add `similar` edges from stored embeddings of the default model |
| `similar_threshold` | `0.8` | minimum cosine for a `similar` edge, in `(0,1]`; only with `similar=true` |

Defaults are `ctxt find --graph`'s; every other search setting takes `POST /find`'s built-in default. There is no `mode` or `offset`: the graph is always the full hybrid trace. Caps outside their range are refused, not clamped. Objects are also bounded by the search's candidate pools (`search.candidate_pool`, 50 full-text + 50 vector by default), so the node cap mostly limits entities. When a cap drops nodes or edges, `metadata.truncated` is `true`.

#### Entities and profiles

- **Entities follow the entity entitlements.** On an instance with inbound entitlements, an entity whose namespace the caller is not entitled to is left out entirely: no node, no `mentions` edge, no share of a `co_mention` weight, not in `counts.entities` or any `mention_count`. The document reads as if the entity were never mentioned. An entity without a stored record is left out too. The check is the unmetered one `GET /entities` filters with. On a private instance every entity is shown.
- **Objects follow `profile`**, like `POST /find`. `profile` is chosen by the caller, not an access control.
- Like search, the graph is not metered.

#### When semantic search can't run

Not an error: `200` with a full-text-only graph. `metadata.mode` is `fts_only` (no default embedding model) or `fts_fallback` (the vector leg failed), `metadata.semantic_status` names the reason and `metadata.vector_error` explains a failure. There are no `similar` edges without `metadata.vector_model`.

#### Errors

Errors use the standard envelope.

| Case | Response |
|---|---|
| `q` missing; `mode` or `offset` given; malformed number, boolean or date; negative `limit` or `min_score` | `400 INVALID_REQUEST` |
| `max_nodes` or `max_edges` outside its range; `similar_threshold` outside `(0,1]` or without `similar=true` | `400 INVALID_REQUEST` |
| `similar=true` on a store that cannot read embeddings by id | `400 INVALID_REQUEST` |

---

## Focus Profiles

### `GET /profiles`

List available focus profiles.

#### Response

```json
{
  "profiles": [
    {
      "name": "founder",
      "description": "Founder worldview for strategic decisions",
      "enabled": true
    },
    {
      "name": "engineer",
      "description": "Engineering worldview for technical depth",
      "enabled": true
    },
    {
      "name": "research",
      "description": "Research worldview for comprehensive analysis",
      "enabled": true
    }
  ]
}
```

### `GET /profiles/{name}`

Get profile details including configuration.

---

## Composition

### `POST /compose`

Generate compositions (briefs, plans, summaries, drafts) from knowledge objects.

#### Request

```json
{
  "type": "brief",
  "profile": "founder",
  "filters": {
    "mention": ["@ui.best-practice"],
    "tag": ["ux.signup"],
    "since": "2025-01-01T00:00:00Z"
  },
  "options": {
    "format": "markdown",
    "includeLinks": true
  }
}
```

#### Response

```json
{
  "id": "comp_xyz",
  "type": "brief",
  "content": "# Executive Brief\n\n...",
  "metadata": {
    "objectsUsed": 15,
    "generatedAt": "2025-01-26T12:00:00Z"
  }
}
```

---

## Entities & Mentions

### `GET /entities`

List and search entities.

### `GET /entities/{slug}`

Get entity details including aliases, translations, and backlinks.

### `GET /entities/{slug}/related`

Get related entities through graph connections.

---

## Registries

### `GET /registries`

List configured registries.

### `GET /registries/{id}`

Get registry details and capabilities.

---

## Suggestions

### `GET /suggest/tags`

### `GET /suggest/mentions`

Plugins may register additional suggestion providers.

---

## Plugin Endpoints

All plugin-defined REST endpoints must live under:

```
/plugins/{pluginName}/...
```

Examples:

- `/plugins/rss_feed/items`
- `/plugins/price_monitor/history/{objectId}`
- `/plugins/notifications/pending`

Plugin routes must not conflict with core routes.

Each plugin defines:

- its own request schema
- its own response schema
- its own access control settings

Example Response:

```json
{
  "plugin": "notifications",
  "pending": [
    {
      "id": "uuid",
      "type": "price_drop",
      "level": "warning",
      "payload": { "new": 1199.0 },
      "timestamp": "2025-01-01T12:33:00Z"
    }
  ]
}
```

---

## Mention-Aware Retrieval Flow

```mermaid
flowchart TD
    Q[Query (RSQL + mentions)] --> AST[AST Parsing]
    AST --> MF[Mention Filters]
    MF --> PL[Query Planner]
    PL --> SG[Scatter–Gather Retrieval]
    SG --> GR[Graph Resolver (Entities)]
    GR --> RR[Reranker]
    RR --> OUT[Final Results with Plugin Augmentations]
```

---

## I18N Behavior

Applies uniformly:

- `language` on ingestion specifies raw input language.
- `origLang` filters knowledge objects by original language.
- `preferLang` requests transformed summaries/titles in preferred language.
- `translate=none` disables translation entirely.
- Plugins may provide additional translation metadata.

## Focus Profile Behavior

Focus profiles influence:

- Pipeline selection during ingestion
- Reranking weights during retrieval
- Composition style and emphasis
- Entity resolution priorities
- Tag weighting and filtering

---

## Summary

The REST API supports:

- mention-aware ingestion and retrieval
- editable mentions
- entity resolution and graph navigation
- focus profiles for contextualized behavior
- composition endpoints (briefs, plans, summaries)
- plugin-defined knowledge object types
- plugin-owned metadata
- `/plugins/*` namespace for plugin endpoints
- plugin-augmented responses (alerts, metadata, diagnostics)
- integration with refresh and auto-fetch systems via plugin-defined fields

This guarantees that plugins remain fully isolated, yet fully capable, without requiring modifications to core REST logic.
