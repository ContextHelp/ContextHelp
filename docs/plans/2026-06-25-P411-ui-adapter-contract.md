# Headless-First UI Adapter Contract — Design

**Date:** 2026-06-25
**Plan:** P411
**Status:** PROPOSAL — not yet approved or implemented
**Related:** P410 (web-ui), hybrid-search, P080 (progressive-retrieval)

---

## Problem

ctxt ships one bespoke front-end: a React 19 + TanStack Router + Radix +
Zustand SPA, embedded in the Go binary and served at `/ui/`
(`web/ui/`, `internal/server/http/server.go`). It is the only first-class
consumer of the engine's retrieval surface.

This couples the knowledge engine to one UI:

- **Maintenance duplication.** Every retrieval feature is re-plumbed by hand
  in the SPA. No second consumer validates the contract.
- **Engine ↔ UI coupling.** The SPA reads `/api/v1` directly, but no part of
  that surface is declared "the UI contract." Internal handler shape and UI
  expectations drift together; nothing pins them.
- **No path for external front-ends.** External consumers and alternative
  front-ends — chat UIs like Open WebUI or LobeChat, plus future/not-yet-
  designed UI types — cannot target ctxt without bespoke per-UI glue. A raw
  bus-event SSE stream exists (`/api/v1/events`,
  `internal/server/http/handlers_events.go:18`) but it dumps undocumented
  internal bus events with no envelope, ordering, profile scoping, or replay —
  nothing a chat UI can subscribe to for transcripts, session state, or pushed
  suggestions.

The goal is to make ctxt **headless-first**: a stable, documented UI-facing
API plus an **adapter contract**, so any front-end becomes a pluggable
adapter — ctxt's own React SPA, Open WebUI, LobeChat, and UIs not yet
designed. Reached **incrementally**. No big-bang rewrite. The React SPA is
never deleted and keeps working throughout.

---

## Current State — honest summary

ctxt already exposes most of a read contract across three protocols. The gap
is a live stream and a *declared* contract, not raw capability.

### Retrieval surface today

| Surface | What exists | Adapter-ready? |
|---|---|---|
| REST search | `GET /api/v1/search?q=&limit=&offset=&profile=` → `{data:[KnowledgeObject], total}` (RSQL, `internal/server/http/handlers_search.go`); `POST /api/v1/find` runs fts, vector or hybrid with diagnostics and scores ([api-rest.md](../api/api-rest.md#search-find)) | Yes — formalize |
| REST objects | `GET /api/v1/objects`, `/api/v1/objects/{id}` | Yes — formalize |
| REST entities | `GET /api/v1/entities`, `/api/v1/entities/{slug}` | Yes — formalize |
| gRPC | `QueryService.Search / ListObjects / GetObject / NodeAwareSearch` (`api/proto/dpkms.proto:179-190`) | Yes — already typed |
| MCP | `search` + `schema` JSON-RPC tools at `/api/v1/mcp/` (`internal/mcp/`, mounted `server.go:188`) | Yes — agent-facing |
| Live events | Raw bus-event SSE at `/api/v1/events` (`handlers_events.go:18`, `server.go:148`). No WebSocket. | Partial — exists, but undeclared |

### Data model

`KnowledgeObject` (`pkg/pluginapi/pluginapi.go:74-108`): `id`, `type`,
`subtype`, `source`, `text_content`, `summaries`, `sections`, `tags`,
`mentions`, `embeddings`, `pipeline`, `profile_id`, `created_at`,
`updated_at`, `graph`. This is the canonical REST/MCP read payload. Note: the
gRPC proto's `KnowledgeObject` (`dpkms.proto:49-64`) is **narrower** — it
omits `subtype`, `summaries`, `sections`, `tags`, `mentions`, `embeddings`.
Reconciling these two shapes is contract work (see open questions).

### Transport & auth

- HTTP binds `127.0.0.1:8080`; gRPC `9090`. `--public` binds `0.0.0.0`.
- Auth: none by default. `profile_id` scoping segments tenant data. Optional
  federation bearer token for cross-node calls.

### What's already adapter-friendly vs UI-coupled

- **Adapter-friendly:** the three read protocols. They are stateless,
  request/response, and already consumed by the SPA over `/api/v1`. Promoting
  them to a contract is mostly *documentation + versioning + a conformance
  suite*, not new code.
- **UI-coupled / raw:** a live channel exists but is unfit as a contract.
  `/api/v1/events` streams the internal event bus verbatim — no envelope,
  no `seq`, no `profile_id` scoping, no replay, no documented event taxonomy.
  The SPA otherwise polls or re-fetches. There is no push of transcripts,
  conversation state, or suggestions in a form a chat UI can target. Part B
  below proposes hardening this raw stream into a declared contract.

---

## Proposed adapter contract

The contract is **versioned, documented, and transport-explicit**. It has two
parts: a read contract (formalizing what exists) and a live event contract
(new).

### Part A — Query / Read contract

Formalize the existing `/api/v1` read surface as `ctxt-ui/v1`. No behavior
change; the change is *commitment*. The SPA's current calls must remain valid.

Operations (each maps to today's REST, with gRPC/MCP as equivalent bindings):

| Op | REST today | Returns |
|---|---|---|
| `search` | `GET /api/v1/search?q=&limit=&offset=&profile=` | `{data:[KnowledgeObject], total}` |
| `getObject` | `GET /api/v1/objects/{id}` | `KnowledgeObject` |
| `listObjects` | `GET /api/v1/objects` | `{data:[KnowledgeObject], total}` |
| `listEntities` | `GET /api/v1/entities` | `{data:[Entity], total}` |
| `getEntity` | `GET /api/v1/entities/{slug}` | `Entity` |
| `searchGraph` | none — planned `GET /api/v1/search/graph` (see below) | JGF document, `ctxt.search-graph/v1` |

Contract rules:

- `KnowledgeObject` and `Entity` JSON shapes are **frozen within a major
  version**. Additive fields only; no removals or renames without a major bump.
- `profile_id` scoping is part of the contract: an adapter passes `profile`
  and receives only that profile's objects.
- Envelope shape `{data, total}` is normative for list/search responses.
- gRPC `QueryService` and MCP `search`/`schema` are declared **alternative
  bindings** of the same operations — same semantics, same payloads.

#### `searchGraph` (proposal — endpoint not built)

**Status:** the producer ships in the CLI today: `ctxt find "<q>" --graph`
(`cmd/ctxt/cmd/find_graph.go`, `internal/searchgraph`). The dpkms endpoint
**`GET /api/v1/search/graph`** is planned, not built; the manual says the
graph is CLI-only for now
([search-graph workflow](../manual/workflows/search-graph.md#limits)).
Parameter names below are a proposal.

`searchGraph` returns every candidate a hybrid search considered — returned,
cut by limit, cut by threshold — with scores, and the entities and links
between them. It traces the hybrid FTS+vector pipeline `ctxt find` runs, not
the query-language search REST `search` serves today
(`internal/search/engine.go`), so its candidates can differ from `search`.

**Inputs** (query parameters; defaults are `ctxt find --graph`'s):

| Param | CLI flag | Default |
|---|---|---|
| `q` (required) | positional query | — |
| `profile` | — | none; see scoping below |
| `limit` | `--limit` | `10` |
| `min_score` | `--min-score` | search config |
| `meta_type`, `topic`, `person`, `since`, `until`, `source_type` | same-named filters | none |
| `max_nodes` | `--graph-max-nodes` | `250` (query node included) |
| `max_edges` | `--graph-max-edges` | `1500` |
| `similar` | `--graph-similar` | `false` |
| `similar_threshold` | `--graph-similar-threshold` | `0.8`, in `(0,1]` |

No mode, `explain` or `facets` parameter: the graph is always the hybrid
trace, which is why the CLI rejects `--fts`, `--semantic`, `--explain` and
`--facets` with `--graph`.

**Output:** `200`, body is the bare JGF v2.1 single-graph document
`{"graph": {...}}` — the bytes `ctxt find --graph --format json` prints, not
wrapped in `{data, total}` (an exception to the envelope rule above). The
vocabulary is `graph.metadata.vocabulary = "ctxt.search-graph/v1"`, defined
in the `internal/searchgraph` package doc (`document.go`) and the
[workflow's key table](../manual/workflows/search-graph.md#useful-keys);
`ctxt.search-graph/v1` is versioned independently of `ui/v1` and bumps on a
breaking document change.

**Errors:**

| Case | Response |
|---|---|
| `q` missing | `400 INVALID_REQUEST`, as `search` |
| `max_nodes` / `max_edges` < 1; `similar_threshold` outside `(0,1]` or given without `similar=true` | `400 INVALID_REQUEST` (CLI: usage error) |
| `similar=true` but the store cannot read embeddings by id (`searchgraph.ErrSimilarUnsupported`) | `400 INVALID_REQUEST`; adapters avoid it by reading the capability below |
| No default embedding model, provider unreachable, no vectors yet | **Not an error.** `200`, full-text-only graph: `metadata.mode` is `fts_only` or `fts_fallback`, `metadata.semantic_status` names the reason, `metadata.vector_error` explains a failure. No `similar` edges without `metadata.vector_model`. |
| Entitlement / quota | per the notes below |

**Security and entitlements:**

- Every string in the document (labels included) is untrusted text; adapters
  escape it before rendering as HTML. The encoder deliberately does not.
- **Entities pass the same entitlement gate as entity reads.** With an
  inbound gate wired, an entity whose namespace the principal is not
  entitled to is left out, with its `mentions` edges — the unmetered filter
  `listEntities` applies (`handlers_entities.go`). The filter must run
  before the builder derives edges: `co_mention` weights count every stored
  mention of a pair, including entities the node cap drops
  (`internal/searchgraph/relations.go`), so filtering nodes after the build
  would leak hidden entities through edge weights.
- **Objects are `profile`-scoped** like every Part A read. Both hybrid legs
  honor `ObjectFilter.ProfileID` on SQLite and Postgres (as `POST /find`'s
  `profile` does); `ctxt find` sets no profile, so the CLI graph stays
  unscoped.
  `profile` is caller-asserted, not an access control.
- The endpoint sits under `/api/v1` and inherits its auth (`RequireAuth`
  when configured). The CLI viewer's rules — loopback-only bind, per-run
  256-bit URL token, `Host` check, idle shutdown
  (`cmd/ctxt/cmd/find_graph_viewer.go`) — and the `0600` mode of `-o` files
  apply only to the CLI; they are not part of this contract.

### Part B — Live event contract (hardens existing raw SSE)

A raw bus-event SSE stream already exists at `/api/v1/events`
(`handlers_events.go:18`), but it is undeclared: it emits internal bus events
with no envelope, ordering, scoping, or replay. This part proposes a
**contract-grade** stream layered on the same SSE transport: a single
server-push channel an adapter subscribes to for session-scoped data. **SSE
stays the default transport** (one-way server→client, proxy-friendly, trivial
in browsers, already implemented); WebSocket is an optional upgrade for
adapters needing client→server frames.

**Endpoint (proposal):** `GET /api/v1/stream?profile=&since=` — a declared,
enveloped SSE surface alongside (eventually superseding) the raw
`/api/v1/events`. Optional `GET /api/v1/ws` (WebSocket) as a
capability-negotiated upgrade.

**Event envelope** (CloudEvents-flavored, JSON):

```json
{
  "id": "evt_01HZ...",
  "seq": 4217,
  "type": "transcript.appended",
  "profile_id": "work",
  "session_id": "sess_01HZ...",
  "time": "2026-06-25T14:03:11Z",
  "data": { }
}
```

**Event types** (proposal, namespaced):

| Type | Meaning | `data` payload |
|---|---|---|
| `transcript.appended` | New message/turn in a session transcript | `{role, content, refs:[object_id]}` |
| `session.state` | Conversation state changed | `{session_id, status, title, profile_id}` |
| `suggestion.pushed` | Engine pushes a proactive suggestion | `{kind, object_id?, text, score}` |
| `notification.pushed` | System notice (job done, capture indexed) | `{level, text, ref?}` |
| `object.changed` | A `KnowledgeObject` was created/updated | `{object_id, change}` |

Envelope rules:

- **Ordering.** `seq` is a monotonic per-profile cursor. Events are delivered
  in `seq` order within a `profile_id`. No cross-profile ordering guarantee.
- **Replay.** `?since=<seq>` requests replay from a cursor; the server replays
  buffered events then continues live. Buffer depth is bounded (config); a gap
  beyond the buffer returns a `resync_required` control event so the adapter
  re-fetches via Part A.
- **Auth.** The stream is `profile_id`-scoped. On `127.0.0.1` (default bind)
  no auth, matching current REST. When `--public`, the stream requires the
  same bearer credential the rest of the public surface uses; the token's
  scope pins which `profile_id`s it may subscribe to. Multi-tenant scoping is
  enforced server-side — an adapter cannot subscribe past its token's
  profiles.

### What an "adapter" must implement

An adapter is a **thin client of the contract**. To claim conformance it must:

1. Speak Part A for read (search / object / entity) using `{data, total}` and
   the frozen `KnowledgeObject` / `Entity` shapes.
2. Subscribe to Part B, honor `seq` ordering, and handle `resync_required`.
3. Pass `profile_id` through on every read and on stream subscribe.
4. Degrade gracefully when an optional capability is absent (see negotiation).

It must NOT depend on internal handler shapes, internal routes, or undocumented
fields.

### Capability negotiation

`GET /api/v1/capabilities` returns the engine's contract version and feature
flags:

```json
{
  "contract_version": "ui/v1",
  "transports": ["sse", "ws"],
  "events": ["transcript.appended", "session.state", "suggestion.pushed",
             "notification.pushed", "object.changed"],
  "auth": "none",
  "profiles": ["work", "personal"],
  "search_graph": {"vocabulary": "ctxt.search-graph/v1", "similar": true}
}
```

An adapter reads this first and adapts: e.g. fall back to SSE if `ws` absent,
hide suggestion UI if `suggestion.pushed` not advertised. `search_graph`
(proposal) is present only when the engine serves `searchGraph`: absent, or
a `vocabulary` the adapter does not understand, means hide the graph view
and keep the plain `search` list; `similar: false` means hide the similar
toggle (the store cannot read embeddings by id). Negotiation makes
the contract forward-compatible — older adapters ignore unknown event types;
newer adapters feature-detect.

### Versioning

- Path-versioned: `ui/v1`. Breaking change → `ui/v2`, served alongside `v1`.
- Within a major: additive only (new event types, new optional fields).
- `contract_version` in `/capabilities` is the single source of truth.

---

## Adapter examples (sketches, not implementations)

### (a) ctxt's own React SPA — proves no-regression

The existing SPA is re-pointed at `ctxt-ui/v1` rather than calling `/api/v1`
ad hoc. Its current search/object/entity calls already match Part A, so the
read path is a rename, not a rewrite. The new work is swapping any
polling/re-fetch for a Part B subscription (transcript, suggestions). If the
SPA passes the conformance suite, the contract demonstrably covers the
shipping UI — no regression.

### (b) Open WebUI adapter

A small shim presents ctxt as an Open WebUI-compatible backend: map Open
WebUI's chat/model calls to Part A `search` + `getObject` for retrieval, and
bridge Part B `transcript.appended` / `suggestion.pushed` into Open WebUI's
streaming message channel. The shim is stateless and owns no knowledge — ctxt
remains the engine. ctxt is NOT replaced by Open WebUI; Open WebUI is one
optional front-end.

### (c) Generic third-party adapter (LobeChat)

LobeChat (or any chat UI) targets the published spec directly: read via Part A,
subscribe via Part B SSE, negotiate via `/capabilities`. No ctxt-specific code
beyond the adapter file. This is the proof that "any front-end is a pluggable
adapter" — a UI the ctxt team did not write reaches feature parity through the
contract alone.

---

## Incremental migration path

Each phase is independently shippable. The React SPA keeps working at every
step.

**Phase 1 — Document + stabilize the read contract.**
Publish `ctxt-ui/v1` over the existing `/api/v1` read surface. Add
`/api/v1/capabilities`. Write a conformance suite that exercises
search/object/entity against the frozen shapes. No engine behavior changes.
Ships value immediately: a documented, tested read contract.

**Phase 2 — Harden the live event stream into a contract.**
Build `GET /api/v1/stream` (SSE) on top of the existing
`/api/v1/events` bus stream, adding the envelope, `seq` ordering, `?since=`
replay, and `profile_id` scoping the raw stream lacks. Map the bus events
already flowing (object changed, capture indexed) to the declared event types
first; transcript / suggestion events follow as those subsystems emit. The
raw `/api/v1/events` keeps working for current consumers (e.g. the SPA's job
updates) until they migrate. SPA unaffected until it opts in.

**Phase 3 — Port the React SPA onto the contract.**
Re-point the SPA at `ctxt-ui/v1` for reads and subscribe to Part B for live
data. Run it through the conformance suite. This validates the contract
against the real shipping UI and removes bespoke polling.

**Phase 4 — Publish the adapter SDK / spec.**
Ship the written spec, the `/capabilities` schema, and a thin reference
adapter so external UIs (Open WebUI, LobeChat, future types) can target ctxt.
Optionally a small client SDK. External adapters now plug in without engine
changes.

---

## Backward compatibility & non-goals

**Compatibility:**

- `/api/v1` REST endpoints are NOT broken. `ctxt-ui/v1` is the same surface,
  declared and versioned — existing callers keep working.
- gRPC `QueryService` and MCP `search`/`schema` remain; they are alternative
  bindings, not deprecated.
- The existing raw `/api/v1/events` SSE stream is NOT removed; the contract
  stream is added alongside it, and current consumers migrate on their own
  schedule.
- The React SPA is NOT deleted and continues to ship embedded at `/ui/`.

**Non-goals:**

- Not mandating Open WebUI (or any external UI) as the front-end.
- Not removing the bespoke SPA.
- Not changing default-local-no-auth posture.
- Not designing the transcript/session subsystems themselves — only the
  contract surface that exposes them.

---

## Open questions / risks

- **Live-stream auth model.** REST is unauthenticated on `127.0.0.1`. Does the
  stream inherit that, or always require a token even locally? Token → profile
  scope mapping needs a concrete design before `--public` streaming ships.
- **Multi-tenant scoping over the stream.** Enforcing `profile_id` isolation on
  a long-lived connection differs from per-request REST scoping. Subscription
  must be re-validated if a token's profile scope changes mid-connection.
- **Ordering & replay bounds.** `seq` is per-profile; buffer depth is finite.
  What happens on buffer overrun beyond `resync_required`? Persisted event log
  vs. in-memory ring buffer is an open trade-off.
- **Transport choice.** SSE default vs. WebSocket for adapters needing
  client→server frames (e.g. live typing). Negotiated, but the WS upgrade path
  adds surface area.
- **Adapter hosting & trust.** Who hosts adapters — bundled in ctxt, separate
  processes, or third-party? An out-of-process adapter on a `--public` bind
  widens the trust boundary and needs its own auth story.
- **Payload-shape reconciliation.** The REST/Go `KnowledgeObject`
  (`pluginapi.go:74-108`) is richer than the gRPC proto
  (`dpkms.proto:49-64`, missing `subtype`, `summaries`, `sections`, `tags`,
  `mentions`, `embeddings`). The contract must declare ONE normative shape;
  either widen the proto or document the gRPC binding as a deliberately
  reduced projection. Unresolved drift here breaks the "alternative bindings,
  same payload" claim.
- **Contract drift enforcement.** The conformance suite must run in CI against
  both the SPA and reference adapters, or the "frozen shape" guarantee erodes.

---

**Status: PROPOSAL — not yet approved or implemented**
