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

- API key via `Authorization: Bearer <token>`
- Optional mTLS
- Optional reverse-proxy auth integration

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
- `UNKNOWN_ENTITY`
- `VALIDATION_FAILED`
- `NOT_FOUND`
- `AUTH_REQUIRED`
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

- In **ingestion**, via user-supplied `@entities`.
- In **knowledge objects**, via a `mentions` array of canonical entity IDs.
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
- `GET /objects/{id}`
- `PATCH /objects/{id}`
- `DELETE /objects/{id}`
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

Supports:

- metadata filters
- Query Language (`q`)
- mention-based filters
- i18n
- plugin-defined filters (`plugin=<pluginName>`)
- pagination and sorting

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

#### Query Parameters

Core:

- `type`
- `tag`
- `mention`
- `profile`
- `pipeline`
- `after`, `before`
- `limit`
- `offset`
- `q`

Plugin:

- `plugin=<pluginName>`
- plugin-defined fields inside `q`

#### Response

```json
{
  "objects": [
    {
      "id": "obj_abc",
      "createdAt": "2025-01-01T12:00:02Z",
      "type": "text",
      "subtype": "text.short",
      "title": "Signup flow friction",
      "summary": "The signup flow has too many steps.",
      "language": "en",
      "tags": [
        {
          "label": "ux.signup.antipattern",
          "weight": 0.92,
          "polarity": "negative"
        }
      ],
      "hints": ["#ux", "#bad"],
      "mentions": [
        "ui.best-practice",
        "stripe.api.checkout"
      ],
      "plugins": {
        "price_monitor": {
          "current_price": 1199.00,
          "threshold_percent": 10
        }
      },
      "pipeline": "text.short"
    }
  ],
  "plugins": {
    "notifications": {
      "pending": [
        {
          "id": "note1",
          "type": "price_drop",
          "level": "warning"
        }
      ]
    }
  },
  "limit": 50,
  "offset": 0,
  "total": 1
}
```

Plugins may surface alerts or supplemental data in responses.

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
