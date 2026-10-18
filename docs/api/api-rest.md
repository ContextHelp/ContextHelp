# REST API

This document specifies the ContextHelp REST API: available endpoints, request/response formats, query parameters, conventions, and **mention-aware** semantics. The REST API provides a thin, consistent interface over the job system, pipelines, storage, query engine, registries, and entity-resolution subsystems.

There is no plugin system ([ADR-078](../decisions/ADR-078-no-plugin-system.md)): nothing adds routes or response fields outside the dpkms binary.

## Principles

- Local-first API surface.
- Job-based ingestion for deterministic processing.
- Write path mediated through durable jobs.
- Read path supports multi-source retrieval.
- Extended RSQL Query Language with **mention operators**.
- Entities, mentions, hints, and tags are independent semantic layers.
- I18N/L10N optional but exposed consistently.

## Base URL

Default:

- `http://127.0.0.1:8080`

Configurable in ContextHelp settings.

## Authentication

Supported modes:

- API key via `Authorization: Bearer <token>` or `X-API-Key: <token>`
- Web UI session cookie, browser only; minted by `ctxt ui open` (see [Sign in to the web UI](../manual/workflows/web-ui-sign-in.md)). Its scopes are the minting token's intersected with the web UI's set
- Optional mTLS
- Optional reverse-proxy auth integration

Private instances (loopback, no `server.auth`) accept requests without a token. On protected and public instances:

- Every `/api/v1` route requires one scope (`read:objects`, `write:objects`, `delete:objects`, `process:inbox`, `admin:audit`, ...). A token's roles (`admin`, `writer`, `reader`) expand to its scopes. Route-to-scope table: [config-and-permissions](../manual/reference/config-and-permissions.md#inbound-auth-roles-and-scopes).
- A missing or invalid token gets 401 `UNAUTHORIZED`.
- A valid token without the route's scope gets 403 `INSUFFICIENT_SCOPE`, with `details.required_scope` and a `WWW-Authenticate: Bearer error="insufficient_scope", scope="..."` header (RFC 6750).
- `GET /api/v1/whoami` returns the caller, with `via` (`token`, `session`, or `none` on a private instance):

```json
{"principal": "phone", "provider": "static", "roles": ["writer"], "scopes": ["read:objects", "write:objects", "..."], "via": "token"}
```

A web UI session cookie reports the session's narrowed scopes and the session itself:

```json
{"principal": "phone", "provider": "static", "roles": ["writer"],
 "scopes": ["read:objects", "read:inbox", "read:feeds", "read:jobs", "write:jobs", "read:registries", "read:system", "signout:ui"],
 "via": "session",
 "session": {"id": "uis_3f2a…", "kind": "ui", "created_at": "…", "idle_expires_at": "…", "expires_at": "…"}}
```

Invite and sign-in links (`ctxt share invite`, `ctxt principal link`) open the web UI at `/ui/invite` and `/ui/signin` with the ticket in the fragment; the page redeems it for the session cookie. For a browser signed in through an invite or a sign-in link, the session is bound to the `link` identity the redemption created, with the principal's role scopes narrowed to the web UI's set. `ctxt principal identity remove` on that identity ends the session; revoking the principal also ends it.

The sign-in ceremonies (`POST /ui/auth/session`, `/ui/invite/session` and `/ui/signin/session`) share one budget per client address: ten attempts a minute, then 429 `RATE_LIMITED` with a `Retry-After`. A ceremony on a request that already carries a live session is refused with 409 `SESSION_ACTIVE`: the browser signs out first. Every ticket is single use; a second redemption is 401 `INVALID_TICKET`.

Sharing routes (see [Share your network](../manual/workflows/sharing.md)): audiences, share policies, grants and the ceiling under `admin:shares`; a key's own policy binding under `write:self`. Audiences and policies may be named by id or by name in a path or a body. A policy document goes in the `document` field of a JSON body, as YAML or JSON text. The instance validates it and refuses one that does not parse with 400 naming the field. Every response is the plain record with its id. `POST /share/invites` takes an optional `audience` the person joins on accepting, and `GET /whoami` reports `shared: {owner, policies}` for a non-owner principal with a grant in force.

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

## Conventions

- All timestamps are ISO-8601 UTC.
- IDs are opaque strings.
- Pagination uses `limit` + `offset`.
- Boolean parameters use `true`/`false`.

## Reserved Routes

- `/admin/v1/*` is reserved for the **Node Admin API** (used by context.help cloud and other admin clients).

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
- `POST /jobs/{id}/cancel`
- `GET /events` (SSE)
- `GET /objects`
- `GET /objects/facets`
- `GET /objects/{id}`
- `GET /objects/{id}/related`
- `PATCH /objects/{id}`
- `DELETE /objects/{id}`
- `POST /objects/{id}/reprocess`
- `GET /objects/{id}/edges`
- `POST /edges`
- `DELETE /edges`
- `POST /aliases`
- `GET /aliases`
- `GET /aliases/{alias}`
- `DELETE /aliases/{alias}`
- `POST /find`
- `GET /search/graph`
- `POST /inbox`
- `GET /inbox`
- `GET /inbox/queue`
- `POST /inbox/{id}/triage`
- `POST /inbox/{id}/discard`
- `POST /inbox/clear`
- `GET /profiles`
- `GET /profiles/{name}`
- `POST /compose`
- `GET /registries`
- `GET /registries/{id}`
- `GET /entities` (`?q=` searches)
- `GET /entities/resolve`
- `GET /entities/{slug}`
- `GET /entities/{slug}/backlinks`
- `GET /entities/registry-pending`
- `POST /entities/registry-pending/approve`
- `POST /entities/registry-pending/reject`
- `GET /pages`
- `GET /pages/{slug}`
- `POST /pages/{slug}/refresh`
- `GET /topic-index`
- `GET /topic-index/{topic}`
- `POST /topic-index`
- `GET /stats`
- `GET /suggest/tags` (optional)
- `GET /suggest/mentions` (optional)
- `GET /whoami`
- `GET|POST /principals`, `GET /principals/{id}`, `POST /principals/{id}/suspend`, `POST /principals/{id}/revoke`, `POST /principals/{id}/links`
- `GET|POST /principals/{id}/identities`, `DELETE /principals/{id}/identities/{identity}`
- `GET|POST /principals/{id}/keys`, `DELETE /principals/{id}/keys/{key}`
- `POST /share/invites`
- `GET|POST /share/audiences`, `GET /share/audiences/{audience}`, `PUT|DELETE /share/audiences/{audience}/members/{principal}`
- `GET|POST /share/policies`, `GET|PUT /share/policies/{policy}`, `GET /share/templates`, `POST /share/templates/{name}`
- `GET|POST /share/grants`, `DELETE /share/grants/{grant}`, `GET|PUT /principals/{id}/ceiling`, `PUT /share/keys/{key}/policy`
- `GET|POST /self/keys`, `DELETE /self/keys/{key}`, `PUT /self/keys/{key}/policy`, `GET /self/identities`, `DELETE /self/identities/{identity}`, `GET /self/sessions`, `DELETE /self/sessions/{session}`

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
  }
}
```

---

## Ingestion: Analyze

### `POST /analyze`

Enqueue a job that runs a full enrichment pipeline.

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
  "note": "free-form text, e.g. a transcript",
  "data": {"title": "Self-hosting n8n", "channel": "fireship"},
  "source_key": "youtube:abc123"
}
```

Notes:

- `note` is stored as the object's `inbox_note` (at most 2 MiB); `data` is a JSON object (at most 256 KiB encoded) stored as `metadata.data`. Both are on the draft every pipeline step receives; neither replaces the fetched content. Over a limit: `400 INVALID_REQUEST`.
- `source_key` is stored as the object's `source_key`. A submission whose key (or content hash) matches a stored object updates it instead of creating another: a non-empty `note` replaces the stored note, a non-empty `data` replaces `metadata.data`, `hints` and `mentions` are added, the object is reinforced, and the job's `result_id` is the stored object. The key is checked before the content hash, and a submission carrying a key, note or data is not dropped by `duplicates.policy: drop`. With `"raw": true` the same update applies and the response is the stored object's ID.
- `POST /pipelines/enqueue` takes the same `note`, `data`, `source_key`, `hints`, `mentions` and `profile` fields.

- The `mentions` field accepts `@namespace.slug` strings for backward compatibility. The pipeline converts them to `ctxt://entity/namespace/slug` URIs stored as `mention_uris`.
- Mentions are **not resolved here**; resolution happens in the pipeline.

#### Response

```json
{
  "jobId": "job_123",
  "status": "pending"
}
```

---

## Events

### `GET /events`

Server-Sent Events stream of bus events. For an external listener (a recipe that reacts to new objects), see [Listen for events](../manual/workflows/event-listener.md).

- Scope `read:objects`. A private instance needs no key.
- `Content-Type: text/event-stream`. One `data: <CloudEvents JSON>` per event, a blank line between.
- **Owner.** Every event, with the publisher's whole payload.
- **Any other principal** (a key with a share grant). Only object events (`object.ingested`, `object.updated`, `object.deleted`, `object.raw_stored`), only for objects its viewer may read: the same scoped read `find`, `list` and `get` use, decided per event by object ID.
  - The payload is rebuilt from the object as the viewer reads it. `object.updated` carries `object_id` and `type`, not `fields`. `source` and `tags` appear only when the policy projects them, and `source_key` appears with `source` (the same rule find, list and get apply to the object).
  - A hidden or missing object produces no event, as if it did not exist.
  - Jobs, upgrade, embedding and every other non-object event stay owner-only, and so do the inbox events (`inbox.captured`, `inbox.triaged`): the inbox is an owner-only surface, so a non-owner never receives them even though they carry an object.
  - A principal with no grant gets `200` and an empty stream (heartbeats only), never `403`, so the answer does not reveal what exists.
  - The stream follows the caller. Each event, and each heartbeat, authenticates the credential again and resolves the viewer as it is then (the check is reused for at most 2 seconds during a burst of events). A grant revoked or added mid-stream applies to the next event, with no reconnect. When the key or session itself is revoked or expires, the server writes a final comment, `: closed: credential no longer valid`, and ends the stream. This holds for the owner's key too; a private instance has no credential to check.
- **Deletes.** The object is gone when `object.deleted` fires, so the viewer's read cannot decide it. A non-owner gets the event only for an object that connection was already sent an event for (it saw the object exist), a replayed event included: a replay that delivers an object's `object.ingested` or `object.updated` and then its `object.deleted` delivers both. A delete of any other ID is dropped, so a listener that was away while an object it knew was deleted does not learn of it from the stream: reconcile with `find` or `list`. (The delete row does not record what the viewer could read before the deletion, so the stream cannot decide it for an object the connection never saw.)
- **Heartbeat.** While the stream is idle the server writes an SSE comment line, `: heartbeat`, every `server.events_heartbeat` (default `15s`; zero or negative selects the default). Clients ignore comment lines; proxies and clients use them to tell a quiet stream from a dead one.
- Not provided: `event:` fields and a topic filter.

#### Event ids and resume

Object events (`object.ingested`, `object.updated`, `object.deleted`, `object.raw_stored`) are written to the `event_outbox` table in the transaction of the change that caused them ([ADR-075](../decisions/ADR-075-external-hook-surface.md) §4), and the stream serves them from it.

- **`id:`.** Each object event frame starts with `id: <seq>`, the row's position in the outbox: a positive integer that grows with every row and is never reused. Events that are not object events (jobs, upgrades, embeddings; owner-only) are live from the bus and carry no `id:` line. An `EventSource` remembers the last `id:` and sends it back on reconnect.
- **Envelope `id`.** An object event's envelope `id` is the row's UUIDv7, the same on every connection and on replay: deduplicate on it. Other events keep the UUIDv5 over topic, source, time and payload.
- **Resume.** `Last-Event-ID: <seq>` (header, or the `last_event_id` query parameter for clients that cannot set headers; the header wins) replays the rows with a greater `seq`, then follows live. `Last-Event-ID: 0` replays everything retained. Without either, the stream starts at the present: nothing earlier is sent.
- **Gap-free switch.** One cursor reads the outbox: a replay reads rows in `seq` order, a page at a time, until none is left, and the live tail reads from the same cursor when a new row is committed. A row written during the replay is read as part of it, and no row is read twice. The bus only wakes the stream; a lost wake-up is made up at the next heartbeat.
- **Viewer filter.** Replayed rows go through the same per-viewer filter, caller re-check and payload projection as live ones. A replay under a grant that has since been revoked yields nothing for what it no longer reads, and a hidden object's rows never arrive.
- **Retention.** Rows are kept `server.events_retention` (default `168h`, 7 days) and pruned hourly. The newest row is always kept.
- **`410 Gone`.** An id older than the oldest retained row (`Last-Event-ID` below the pruned floor), or ahead of the newest row the instance wrote (a restored database), is answered before the stream starts:

  ```json
  {"error": {"code": "EVENT_ID_EXPIRED",
             "message": "the event id is older than the retained events; resync from the API and reconnect without Last-Event-ID",
             "details": {"oldest_retained": 1840, "newest": 2210}}}
  ```

  Recover by re-reading current state through the API (`find`, `list`, or a ctxt cursor), then reconnect without `Last-Event-ID`, or with `newest` from the body. An `EventSource` that gets a non-200 answer stops reconnecting; the client must open a new one. A malformed id is `400 INVALID_EVENT_ID`.
- **Delivery.** Events are delivered at least once: a client that reconnects with the last `id:` it processed gets every later row, and may see one it had already handled if it did not record the id. Each connection still holds a 64-event buffer for the non-object events; those are dropped when it is full. Not built yet: pull (`?after=`), webhooks, the `/ws/bus` mirror and the hooks ([ADR-075](../decisions/ADR-075-external-hook-surface.md)).

Envelope: `{id, source, specversion: "1.0", type, datacontenttype, time, data}`. `type` is the topic.

| Topic | `source` | `data` |
|---|---|---|
| `ctxt.runtime.object.ingested` | `worker.pool` | `object_id`, `type`, `source`, `source_key`, `pipeline`, `tags` (labels), `profile_id`, `duration_ms` |
| `ctxt.runtime.object.updated` | `service.objects` | `object_id`, `type`, `fields` (always `null` today) |
| `ctxt.runtime.object.deleted` | `service.objects` | `object_id` |
| `ctxt.runtime.job.enqueued` | `service.analyze` | the full job, including its payload |
| `ctxt.runtime.job.completed` | `worker.pool` | `job_id`, `object_count`, `duration_ms` |
| `ctxt.runtime.job.failed` | `worker.pool` | `job_id`, `error`, `object_id` |

`object.ingested` example:

```
data: {"id":"…","source":"worker.pool","specversion":"1.0","type":"ctxt.runtime.object.ingested","datacontenttype":"application/json","time":"2026-11-19T21:30:00Z","data":{"object_id":"852e0fac-…","type":"text","source":"yta","source_key":"","pipeline":"text.short","tags":["star-candidate"],"profile_id":"","duration_ms":173}}
```

`source` is the object's source (`yta`, `cli`, ...). `source_key` is the object's external dedup key; it is empty for objects created through the job pipeline, which does not persist one. Reinforced duplicates (existing content hash) publish no `object.ingested`. `ctxt ingest` and importer-direct writes publish no events.

---

## Jobs

### `GET /jobs`

Lists jobs, newest first. Scope: `read:jobs`. CLI: `ctxt job list`.

Query Parameters:

- `status`: `pending`, `running`, `completed`, `failed` or `cancelled`
- `limit` (default 20)
- `offset`

#### Response

```json
{
  "data": [
    {
      "id": "job_123",
      "type": "ingest:text",
      "status": "failed",
      "payload": "...",
      "pipeline": "text.short",
      "source": "cli",
      "result_id": "",
      "error": "provider timeout",
      "retry_count": 1,
      "max_retries": 3,
      "created_at": "2026-10-01T12:00:00Z",
      "updated_at": "2026-10-01T12:00:09Z",
      "started_at": "2026-10-01T12:00:01Z",
      "completed_at": "2026-10-01T12:00:09Z"
    }
  ],
  "total": 1
}
```

### `GET /jobs/{id}`

Returns one job in the same shape. Scope: `read:jobs`. An unknown ID is 404 `NOT_FOUND`. CLI: `ctxt job status`.

### `POST /jobs/{id}/retry`

Puts the job back in the queue as `pending`, spends one retry and returns the job. Scope: `write:jobs`. An unknown job, or one whose `retry_count` reached `max_retries`, is 400 `INVALID_REQUEST`. CLI: `ctxt job retry`.

### `POST /jobs/{id}/cancel`

Cancels a `pending` or `running` job and returns it as `cancelled`. Scope: `cancel:jobs`, which only `admin` holds. An unknown ID is 404 `NOT_FOUND`; a job that already finished (`completed`, `failed`, `cancelled`) is 409 `CONFLICT` and is left as it is. CLI: `ctxt job cancel`.

---

## Knowledge Objects

Knowledge objects include **tags**, **hints** and **mentions**.

#### Object content

Content above `storage.blob.threshold` is stored in the blob store, but no response carries the `blob://<key>` reference it is stored as: `raw_content` and `text_content` always hold content, on REST, gRPC and MCP alike.

- **One object** (`GET /objects/{id}`, `PATCH /objects/{id}`): the whole content.
- **Lists and searches** (`GET /objects`, `/objects/{id}/related`, `/search`, `POST /find`, entity backlinks, inbox, reminders): at most `storage.blob.threshold` bytes of it, the size an object stored inline can have. A cut preview sets `metadata.content_truncated: true`; `metadata.blob_original_size` is the full length. Read the object by ID for all of it.
- A blob that cannot be read fails the request with 500, never a reference or empty content. So does a reference whose key is not a blob key (ASCII letters, digits, `-` and `_` only): it is never resolved, so stored content cannot name a file or object outside the blob store.
- **Search** indexes the first `storage.blob.threshold` bytes of such content, never the reference. List results flag that stored prefix `metadata.text_content_truncated: true`; a read by ID returns the whole content as `text_content`.

### `GET /objects`

Lists the objects that match a filter, one page at a time. Scope: `read:objects`.

#### Query Parameters

Every parameter is optional and takes exactly one non-empty value. An unknown, repeated, empty or malformed parameter is a 400 `INVALID_PARAM` whose `details.param` names it. Filters combine with AND.

| Parameter | Matches |
|---|---|
| `type`, `subtype` | the object's type or subtype |
| `tagged` | objects by tag: `a,b` any of the labels; a leading `+` marks a label required (`+a,+b` all; `+a,b,+c` a and c required, b only ranks); with no `+` label one plain label must match. Every listed label an object carries counts one in the ranking, more first. The parameter may repeat; values join in order. Only commas separate labels, so a label may hold spaces (`machine learning` is `machine%20learning`); labels are case-sensitive. `+` is read raw from the query string, not as a space: `tagged=+a,b` and `tagged=%2Ba,b` both mean a required, and a `+` past a label's first character is literal. An unknown label matches nothing; an empty label, a bare `+`, or a leading `-` (reserved for exclusion) is 400 |
| `mention` | objects mentioning the entity, as `@ns.slug` or `ctxt://entity/...` |
| `pipeline` | the pipeline that produced the object |
| `status` | `active` (the default), `inbox`, `discarded`, `raw`, or `all` for every status |
| `since`, `until` | `created_at` bounds, half-open: `since <= created_at < until`. RFC 3339 (`2026-02-10T09:00:00Z`) or a UTC date (`2026-02-10`): a date `since` is that day's start, a date `until` the start of the next day, so `until=2026-02-10` takes the whole 10th |
| `meta_type` | `metadata.type` |
| `topic`, `person` | an element of `metadata.topics` / `metadata.people` |
| `source_type` | `metadata.source_type` |
| `mentioned_since`, `mentioned_until` | a `metadata.dates_mentioned` date on or after / on or before the date (`YYYY-MM-DD` only) |

Order and paging:

| Parameter | Values | Default |
|---|---|---|
| `sort` | `created_at`, `updated_at` | `created_at` |
| `dir` | `asc`, `desc` | `desc` |
| `limit` | integer >= 0; `0` returns every match | `20` |
| `offset` | integer >= 0 | `0` |
| `changed_after` | integer >= 0, a change sequence | none |

`changed_after=N` lists by change order instead: the objects created or updated after change sequence `N`, oldest change first. Every insert and update moves an object's sequence past all earlier ones, whatever its `created_at` and `updated_at` say, so an old object edited since `N` appears here and not under `since=<created_at>`. It cannot be combined with `sort`, `dir` or `offset` (400 `INVALID_PARAM` naming that parameter): the order is fixed and the position is the cursor. `status` and the other filters still apply, `tagged` among them: required and any-mode tags select, but a `tagged` that ranks by tags matched (two or more tags with none required, or a plain tag beside a required one) is 400 (`tagged`) because it would reorder the page; `status` defaults to `active`, so pass `status=all` to see an object that was discarded. `0` starts from the first change. Not accepted by `GET /objects/facets`.

RSQL queries go to `GET /search?q=`; `q` is not a parameter here. `GET /search` accepts `profile` as the caller's focus profile, a lens: it never narrows the results, and what a caller sees is set by its viewer scope alone (ADR-080). The gRPC `Search` and `NodeAwareSearch` `profile_id` field works the same way.

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

With `changed_after`, the response also carries `next_changed_after`: the change sequence of the page's last object, or the request's own value when the page is empty. Pass it as `changed_after` for the next page; a page shorter than `limit` means the listing has caught up. Objects do not carry their own sequence: it is an offset in one store's change order, and the response-level cursor is the only place it appears.

```json
{"data": [{"id": "o-a", "...": "..."}], "total": 1, "next_changed_after": 17}
```

Viewer scoping: a restricted viewer is listed only the objects it may see, rendered through its policy, and `total` counts only those. The cursor is a position in the store's whole change order, so it jumps over the changes to objects the viewer cannot see. That gap (how many changes lie between two visible ones) is the one thing the list reveals about hidden objects, and it cannot be removed while the cursor is a store position. No hidden object's id, content or existence is named. An empty page returns the viewer's own cursor unchanged, so a viewer never learns that hidden changes happened after it.

### `GET /objects/facets`

Counts the objects that match a filter by `metadata.type`. Objects without one count under `(none)`. Scope: `read:objects`.

Takes the filter parameters of `GET /objects` (`type` through `until`), with the same validation. `limit`, `offset`, `sort`, `dir` and `changed_after` are rejected: counts cover every match.

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

### `PATCH /objects/{id}`

Changes an object's editable metadata and returns the stored object. Scope: `write:objects`. REST only; gRPC has no equivalent method.

Send only the fields to change. Each field is optional, but the body needs at least one:

| Field | Type | Effect |
|---|---|---|
| `type` | string | replaces the type |
| `subtype` | string | replaces the subtype |
| `title` | string | replaces the summaries with this one; `ctxt list` and `show` print the first summary as the title |
| `summary` | string | replaces the first summary and keeps the others |
| `tags` | string array | replaces the tags; each label is trimmed and empty labels are dropped. A label already on the object keeps its `source` and `weight`; a new label gets `source: "manual"`. `[]` clears the tags |
| `mentions` | string array | replaces the mentions; each is `@namespace.slug` or `ctxt://entity/<namespace>/<slug>`. `[]` clears them |

`updated_at` moves to the time of the update.

```json
{"title": "Onboarding notes", "tags": ["ux", "onboarding"], "mentions": ["@ux.onboarding"]}
```

Errors, with nothing written:

- 400 `INVALID_REQUEST`: a body that isn't JSON, an unknown field, a field of the wrong type, no field, or `title` together with `summary` (both set the first summary).
- 400 `INVALID_MENTION`: a mention that doesn't parse.
- 404 `NOT_FOUND`: no object with that ID.

### `DELETE /objects/{id}`

Deletes the object and its edges. Answers 204 with no body. Scope: `delete:objects`, which only `admin` tokens hold: a `writer` gets 403 `INSUFFICIENT_SCOPE`. An unknown ID is a 404 `NOT_FOUND`. REST only; gRPC has no equivalent method.

To delete by filter, list the IDs with `GET /objects` and delete each one; `ctxt delete` does this.

### `POST /objects/{id}/reprocess`

Queues a job that re-runs one enrichment step on a stored object and writes the result back to it. The job runs on the dpkms host with that host's providers and configuration. Scope: `write:objects`. REST only; gRPC has no equivalent method.

```json
{"step": "tagger"}
```

`step` is required and is one of `structured_metadata`, `entity_extractor` or `tagger`. The response is 202:

```json
{"job_id": "5f0c…", "object_id": "o-a", "step": "tagger"}
```

Follow the job with `GET /jobs/{job_id}`. Its type is `object:reprocess`, and on completion `result_id` is the object ID. A job whose object was deleted in the meantime fails without a retry.

Errors: 400 `INVALID_REQUEST` for an unknown or missing `step`, an unknown field or a body that isn't JSON; 404 `NOT_FOUND` for an unknown object. Nothing is queued on an error.

### `POST /edges`

Stores a typed link between two objects and, unless the type is symmetric, its inverse, in one transaction. Scope: `write:objects`. REST only; gRPC has no equivalent method.

```json
{"source": "o-a", "target": "o-b", "type": "extends", "context": "newer research"}
```

`type` is one of `extends`, `contradicts`, `supersedes`, `supports`, `related-to`, `derived-from`, or an inverse name (`extended-by`, `contradicted-by`, `superseded-by`, `supported-by`, `derived-to`). The inverse edge runs from `target` to `source` with the inverse type; `related-to` is symmetric and stores one edge. `context` is optional and is stored as each edge's `metadata.context`. The response is 201 with the stored edges, forward first:

```json
{"data": [
  {"id": "…", "from_type": "object", "from_id": "o-a", "to_type": "object", "to_id": "o-b", "edge_type": "extends", "weight": 1, "metadata": {"context": "newer research"}, "created_at": "…"},
  {"id": "…", "from_type": "object", "from_id": "o-b", "to_type": "object", "to_id": "o-a", "edge_type": "extended-by", "weight": 1, "metadata": {"context": "newer research"}, "created_at": "…"}
]}
```

Errors, with nothing stored: 400 `INVALID_REQUEST` for a missing `source`, `target` or `type`, an unknown type, an unknown field or a body that isn't JSON; 404 `NOT_FOUND` when either object is missing.

### `GET /objects/{id}/edges`

The object's edges to and from other objects: typed links, their inverses and other object-to-object edges. Scope: `read:objects`. REST only.

| Parameter | Values | Default |
|---|---|---|
| `direction` | `out`, `in`, `both` | `both` |
| `type` | an edge type; only edges of that type | every type |
| `depth` | 1 to 3 | none |

Without `depth`, every edge touching the object: outbound first, then inbound. With `depth`, a breadth-first walk to that many hops that reaches each object once, through the first edge found; `direction` and `type` apply at every hop. Each entry is a hop:

```json
{"data": [
  {"depth": 1, "from": "o-a", "direction": "out", "object_id": "o-b", "edge": {"edge_type": "extends", "...": "..."}},
  {"depth": 2, "from": "o-b", "direction": "out", "object_id": "o-c", "edge": {"edge_type": "supports", "...": "..."}}
]}
```

`from` is the object the hop leaves, `direction` whether the edge leaves it (`out`) or points at it (`in`), and `object_id` the other end. No edges is `{"data": []}`. Any other parameter, or a value out of range, is a 400 `INVALID_PARAM`; an unknown `{id}` a 404 `NOT_FOUND`.

### `DELETE /edges`

Deletes the links between two objects in both directions, each link together with its inverse, in one transaction. Scope: `delete:edges`, which only `admin` tokens hold: a `writer` gets 403 `INSUFFICIENT_SCOPE`. REST only.

| Parameter | Values |
|---|---|
| `source` | object ID, required |
| `target` | object ID, required |
| `type` | a link type, optional |

With `type`, only `source -[type]-> target` and `target -[inverse]-> source` go. Without it, every link either way between the two. Object edges that are not links (mentions, alternatives) stay. The response is 200 with the number of edges deleted, 0 when there were none:

```json
{"deleted": 2}
```

A missing `source` or `target`, an unknown type or any other parameter is a 400 `INVALID_PARAM`.

### Aliases

An alias is a human-readable name for one object ID, stored in the instance's `aliases` table. A `global` alias resolves in every profile; a `profile` alias resolves only for its profile and wins over a global alias of the same name. The key is (`alias`, `scope`, `profile`): one global `charter` and one `charter` per profile can coexist. REST only: gRPC and MCP have no alias methods, and no `ctxt` command calls these routes. A storage backend without an alias store answers every alias route with 501 `NOT_IMPLEMENTED`.

#### `POST /aliases`

Stores an alias. Scope: `write:objects`.

```json
{"alias": "charter", "object_id": "o-a", "scope": "profile", "profile": "work"}
```

`alias` and `object_id` are required. `scope` is `global` (the default) or `profile`; `profile` is required with `profile` and refused with `global`. The response is 201 with the stored alias:

```json
{"alias": "charter", "object_id": "o-a", "scope": "profile", "profile": "work", "created_at": "…", "updated_at": "…"}
```

Errors, with nothing stored: 400 `INVALID_REQUEST` for a missing field, an unknown scope, a scope/profile mismatch or a body that isn't JSON; 404 `NOT_FOUND` for an unknown object; 409 `CONFLICT` when the (`alias`, `scope`, `profile`) key is taken. Delete the alias first to point it elsewhere.

#### `GET /aliases`

Lists aliases, all of them or, with `?object_id=`, those pointing at one object. Scope: `read:objects`. The response is 200 with a JSON array of alias records, `[]` when there are none.

#### `GET /aliases/{alias}`

Resolves an alias. Scope: `read:objects`. With `?profile=`, that profile's alias is tried before the global one; without it, only the global alias. The response is 200:

```json
{"alias": "charter", "object_id": "o-a"}
```

An alias that resolves to nothing is a 404 `NOT_FOUND`.

#### `DELETE /aliases/{alias}`

Deletes one alias by its key. Scope: `delete:aliases`, which only `admin` tokens hold: a `writer` gets 403 `INSUFFICIENT_SCOPE`. `?scope=` defaults to `global`; `?scope=profile` needs `?profile=`. Answers 204 with no body. No alias with that key is a 404 `NOT_FOUND`; a scope/profile mismatch is a 400 `INVALID_REQUEST`.

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
    "tagged": "+security,rotation",
    "since": "2026-04-01",
    "until": "2026-04-30T23:59:59Z",
    "meta_type": "task",
    "topic": "security",
    "person": "alice-chen",
    "source_type": "slack",
    "mentioned_since": "2026-04-01",
    "mentioned_until": "2026-04-30"
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
- `profile` is the caller's focus profile, a lens: it never filters. Both legs and the facet counts cover objects of every profile, and of none, whatever it says; what a caller sees is set by its viewer scope alone (ADR-080). The field is accepted and may be left out.
- `filter` and each of its fields are optional. `tagged` is the tag selection of `GET /objects` (`a,b` any, `+a,+b` all, `+a,b` a required, b optional). Results matching more of the listed tags rank first, then by relevance: a tier, not a score bonus, so `min_score` and `score_breakdown.total` are unchanged; the count is `score_breakdown.tags_matched`. `since` and `until` bound `created_at` (RFC 3339 or `YYYY-MM-DD`, midnight UTC). `mentioned_since` and `mentioned_until` are dates (`YYYY-MM-DD`), compared with an object's `dates_mentioned`.
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

The web UI draws it at `/ui/searchgraph/?q=<query>`: the page forwards its own query string to this endpoint, so every parameter below works there too. See [Open it in the web UI](../manual/workflows/search-graph.md#open-it-in-the-web-ui).

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
| `profile` | none | the caller's focus profile, a lens: it never narrows the objects |
| `limit` | `10` | results the search returns; candidates past it stay in the graph as `cut_limit` |
| `min_score` | `0` | results scoring below it stay in the graph as `cut_threshold` |
| `tagged`, `since`, `until`, `meta_type`, `topic`, `person`, `source_type`, `mentioned_since`, `mentioned_until` | none | the filters of `POST /find`; `mentioned_since` and `mentioned_until` are `YYYY-MM-DD` |
| `max_nodes` | `250` | node cap, query node included; `1`–`1000` |
| `max_edges` | `1500` | edge cap; `1`–`10000` |
| `similar` | `false` | add `similar` edges from stored embeddings of the default model |
| `similar_threshold` | `0.8` | minimum cosine for a `similar` edge, in `(0,1]`; only with `similar=true` |

Defaults are `ctxt find --graph`'s; every other search setting takes `POST /find`'s built-in default. There is no `mode` or `offset`: the graph is always the full hybrid trace. Caps outside their range are refused, not clamped. Objects are also bounded by the search's candidate pools (`search.candidate_pool`, 50 full-text + 50 vector by default), so the node cap mostly limits entities. When a cap drops nodes or edges, `metadata.truncated` is `true`.

#### Entities and profiles

- **Entities follow the entity entitlements.** On an instance with inbound entitlements, an entity whose namespace the caller is not entitled to is left out entirely: no node, no `mentions` edge, no share of a `co_mention` weight, not in `counts.entities` or any `mention_count`. The document reads as if the entity were never mentioned. An entity without a stored record is left out too. The check is the unmetered one `GET /entities` filters with. On a private instance every entity is shown.
- **Objects follow `profile`.** `profile` is chosen by the caller, not an access control.
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

## Inbox

Captured content parked for a decision (`status: inbox`) before any pipeline runs. REST only; gRPC has no inbox methods.

| Route | Scope | Does |
|---|---|---|
| `POST /inbox` | `write:inbox` | Captures content as an inbox item |
| `GET /inbox` | `read:inbox` | Lists inbox items |
| `GET /inbox/queue` | `read:inbox` | Lists the processing queue: pending, running and failed jobs, and raw objects |
| `POST /inbox/{id}/triage` | `process:inbox` | Makes the inbox item `active` and enqueues it. Body `{"pipeline": "<name>"}` is optional. Returns `{"job_id": "…"}` |
| `POST /inbox/{id}/discard` | `process:inbox` | Marks the inbox item `discarded`. Returns 204 |
| `POST /inbox/clear` | `process:inbox` | Discards every inbox item |

`process:inbox` is in the `admin` bundle only. A `writer` token captures but gets 403 `INSUFFICIENT_SCOPE` on triage, discard and clear. Triage and discard act only on an inbox item: an object with `status: inbox`, the set `GET /inbox` lists. Any other ID answers 404 `NOT_FOUND` (`no inbox item <id>`) and changes nothing: an unknown ID, or an object that is `active`, `discarded` or `raw` (already triaged, discarded or still in the pipeline). The status check and the change are one write, so two concurrent triages of the same item enqueue one job; the other gets 404.

### `GET /inbox`

Query parameters: `limit` (default 20), `offset`, and `before` / `after` (RFC 3339) bounding the creation time.

```json
{ "items": [ { "id": "4a3b1c2d-…", "type": "text", "status": "inbox", "inbox_note": "read later", "created_at": "2026-04-10T09:00:00Z" } ], "total": 1 }
```

`items` are knowledge objects, as `GET /objects/{id}` returns them.

### `GET /inbox/queue`

| Parameter | Meaning |
|---|---|
| `pending` | `true` includes pending and running jobs |
| `failed` | `true` includes failed jobs |
| `raw` | `true` includes raw (unenriched) objects |
| `limit` | Page size, at least 1; default 50 |
| `offset` | Items to skip in the combined list; default 0 |

With none of `pending`, `failed` and `raw` set to `true`, all three categories are listed. Each parameter takes exactly one value. An unknown, repeated, empty or malformed parameter, or a boolean other than `true` / `false`, gets 400 `INVALID_PARAM` with `details.param` naming it.

```json
{
  "items": [
    { "id": "job-7f…", "kind": "job", "status": "failed", "type": "url", "source": "https://example.com", "pipeline": "url.generic", "created_at": "2026-04-12T09:00:00Z", "error": "fetch: 503" },
    { "id": "obj-1c…", "kind": "object", "status": "raw", "type": "note", "source": "", "pipeline": "", "created_at": "2026-04-11T09:00:00Z" }
  ],
  "total": 2
}
```

- `kind` is `job` (the `id` is a job ID) or `object` (an object ID).
- `status` is the job status (`pending`, `running`, `failed`) or `raw`.
- `error` is present on failed jobs only.
- `items` is `[]`, never `null`, when the queue is empty.

### `POST /inbox/clear`

Discards every item with `status: inbox`, however many there are. Objects in any other status are untouched. No body.

```json
{ "cleared": 2 }
```

`cleared` is 0 when the inbox was already empty.

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

Every entity read needs `read:objects`. On a non-private instance the inbound entitlement gate applies to every principal except `admin`: listings drop entities outside the principal's granted namespaces, and a read of one entity is refused with 403 `ENTITLEMENT_REQUIRED` outside them. A metered read past the principal's quota is a 429 `QUOTA_EXHAUSTED`. Admin principals are neither gated nor metered.

Search and resolve are REST only. gRPC `EntityService` has `ListEntities`, `GetEntity` and `GetEntityBacklinks`, with no query field and no resolve method; ctxt uses REST.

### `GET /entities`

Lists entities by slug, one page at a time. With `q`, only the entities whose slug, title or any alias contains `q` are listed. The match ignores ASCII case, and `%` and `_` match literally. Unmetered.

| Parameter | Matches | Default |
|---|---|---|
| `q` | substring of the slug, title or an alias | every entity |
| `namespace` | the entity's namespace, exactly | every namespace |
| `limit` | page size; applied after `q` | `20` |
| `offset` | entities to skip | `0` |

```json
{"data": [{"slug": "ui.checkout-flow", "title": "Checkout Flow", "namespace": "ui", "created_at": "2026-01-10T09:00:00Z", "updated_at": "2026-01-10T09:00:00Z"}]}
```

No match returns `{"data": []}`.

### `GET /entities/resolve?mention=`

Returns the entity a mention names: the entity whose slug equals `mention`, else the one carrying `mention` as an alias. Both comparisons are exact, and a leading `@` is part of the mention. The read is metered as an `entity_resolve`, like `GET /entities/{slug}`. The body is the entity object that `GET /entities/{slug}` returns.

The static path takes precedence over `/entities/{slug}`, so an entity whose slug is `resolve` is read with `?mention=resolve`.

| Status | Code | When |
|---|---|---|
| 400 | `INVALID_PARAM` | `mention` is missing or empty |
| 403 | `ENTITLEMENT_REQUIRED` | The entity's namespace is outside the principal's grant |
| 404 | `NOT_FOUND` | No slug or alias equals `mention` |
| 429 | `QUOTA_EXHAUSTED` | The principal's `entity_resolve` quota is spent |

### `GET /entities/{slug}`

Returns one entity by exact slug, with its aliases, description and provenance (`content_status`, `version_hash`, `registry_url`). Metered as an `entity_resolve`. An unknown slug is a 404 `NOT_FOUND`.

### `GET /entities/{slug}/backlinks`

Lists the objects that mention the entity: `{"data": [...objects]}`. The gate check is unmetered. An unknown slug returns `{"data": []}` to an ungated caller and a 404 to a gated one.

### Registry approval queue

Entities an `untrusted` registry serves are not written by sync; they wait here. `sandboxed` registries' entities are blocked, never queued. Every route needs `sync:registries`, which only `admin` holds: listing too, since the payloads are unreviewed content from registries the instance does not trust. `reader` and `writer` tokens get 403 `INSUFFICIENT_SCOPE`.

| Route | Does |
|---|---|
| `GET /entities/registry-pending` | Lists the queue; `?registry=<name>` keeps one registry's: `{"pending": [...], "total": N}` |
| `POST /entities/registry-pending/approve` | Writes the selected entities as a trusted sync would (the registry's `sync_mode`), with metadata `provenance: "approved from <registry>"` on full writes, and drops them from the queue: `{"approved": [...], "total": N}` |
| `POST /entities/registry-pending/reject` | Drops the selected entities unwritten: `{"rejected": [...], "total": N}` |

A queued entity: `id`, `registry`, `registry_url`, `slug`, `namespace`, `title`, `version` (the `version_hash`, or `sha256:<hex>` of the content when the registry serves none), `entity` (the payload), `signature_status` (`verified` or `unsigned`), `first_seen_at`, `last_seen_at`. The ID is stable for a registry, slug and version.

Approve and reject take `{"ids": ["..."]}`; approve also takes `{"all": true, "registry": "<name>"}`. Unknown fields are rejected.

| Status | Code | When |
|---|---|---|
| 400 | `INVALID_REQUEST` | Bad JSON; no IDs and no `all`; `all` without `registry`; IDs together with `all` |
| 404 | `NOT_FOUND` | An ID is not queued; nothing is written or dropped |
| 409 | `REGISTRY_SANDBOXED` | Approve of an entity whose registry is now `sandboxed`; nothing is written |

---

## Entity Pages, Topic Index and Stats

REST only; gRPC has none of these methods.

| Route | Scope | Does |
|---|---|---|
| `GET /pages` | `read:objects` | Lists entity pages (objects of type `entity_page`), newest first |
| `GET /pages/{slug}` | `read:objects` | Returns the entity page of one entity slug |
| `POST /pages/{slug}/refresh` | `write:objects` | Recompiles the page from its current sources, stores it and returns it |
| `GET /topic-index` | `read:objects` | Returns the stored topic index |
| `GET /topic-index/{topic}` | `read:objects` | Returns the entries of one topic |
| `POST /topic-index` | `write:objects` | Rebuilds the topic index from every object |
| `GET /stats` | `read:objects` | Counts objects, jobs, entities, feeds, reminders and resurfacing candidates |

A browser session may make the reads; refresh and rebuild need a `writer` or `admin` token. A `reader` token gets 403 `INSUFFICIENT_SCOPE` on both.

### `GET /pages`

Query parameter: `limit`, at least 1, default 50. An unknown, repeated or malformed parameter gets 400 `INVALID_PARAM`.

```json
{"data": [{"id": "p-alice-0001", "type": "entity_page", "raw_content": "# person.alice …", "metadata": {"entity_page": {"entity_slug": "person.alice", "source_ids": ["s-1", "s-2"], "revision_count": 2, "revision_log": []}}, "updated_at": "2026-10-01T09:00:00Z"}]}
```

Items are knowledge objects, as `GET /objects/{id}` returns them; `metadata.entity_page` names the entity and its sources. `data` is `[]` when there is no page.

### `GET /pages/{slug}` and `POST /pages/{slug}/refresh`

Both return the page object. A slug without a page is a 404 `NOT_FOUND`. Refresh takes no body; it recompiles `raw_content` from the page's `source_ids` and leaves the revision log as it is.

### `GET /topic-index` and `GET /topic-index/{topic}`

The index groups object summaries by the topics of `enrichment.structured_metadata`:

```json
{"categories": [{"topic": "go", "entries": [{"object_id": "s-1", "summary": "Alice ships Go"}]}], "object_ids": ["s-1"], "generated_at": "2026-10-01T09:00:00Z"}
```

`GET /topic-index/{topic}` returns `{"data": [...entries]}` of the first category whose name starts with `topic`, ignoring case, and `{"data": []}` when none does. Before the first rebuild both reads are a 404 `NOT_FOUND`.

### `POST /topic-index`

Rebuilds the index from every object and stores it. No body. Returns `{"id": "<index object ID>"}`.

### `GET /stats`

Query parameter: `profile` scopes the resurfacing count to one focus profile; without it every profile counts.

```json
{
  "knowledge_objects": 3,
  "objects_by_type": {"article": 1, "entity_page": 1, "note": 1},
  "jobs": {"total": 3, "pending": 1, "running": 0, "completed": 2, "failed": 0},
  "entities": 1,
  "feeds": 2,
  "feeds_active": 1,
  "reminders_pending": 1,
  "resurfacing_candidates": 2
}
```

- `knowledge_objects` counts every object, whatever its status.
- `entities` counts only the entities the principal may list: the inbound entitlement gate applies as it does to `GET /entities`.
- `resurfacing_candidates` counts entries neither surfaced nor dismissed. It is `null` on a storage backend without a resurfacing queue (Postgres, for now): unknown, not zero.

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
    RR --> OUT[Final Results]
```

---

## I18N Behavior

Applies uniformly:

- `language` on ingestion specifies raw input language.
- `origLang` filters knowledge objects by original language.
- `preferLang` requests transformed summaries/titles in preferred language.
- `translate=none` disables translation entirely.

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
