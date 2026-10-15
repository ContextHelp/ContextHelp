# ADR-080 – Sharing: Audiences, Share Policies and Per-Viewer Visibility

> **Status:** Accepted (2026-10-14; decisions D1–D15 as recommended in the plan; amended the same day after review, D18–D23)
> **Date:** 2026-10-14
> **Author:** jadb
> **Applies to:** dPKMS, ctxt
> **Depends on:** ADR-079 (authentication methods and principals)
> **Extends:** ADR-023 (profile-scoped access, never built), ADR-015 (focus profiles)
> **References:** ADR-010/ADR-050 (RSQL), ADR-019 (encryption and privacy), ADR-049 (edges table), ADR-058 (external steps; ingest-time masking), ADR-063 (graph-canonical object), ADR-064 (federation), ADR-068 (MCP read surface), ADR-071 (embedding index versioning), ADR-075 (hooks and events)
> **Plan:** [`docs/plans/2026-10-14-auth-and-sharing.md`](../plans/2026-10-14-auth-and-sharing.md)

---

## Context

### The use case

An owner keeps a large network of high-value contacts in dpkms, plus notes about those people. They want to share:

- the network with **work colleagues**, without phone numbers or email addresses;
- the network with **partners**, with contact details but without the owner's personal notes.

Personal notes leak in ways that are easy to miss. "Sarah — kids Leo and Mia, moving to Lisbon" is a note. It mentions `person/sarah`, so it adds a mention edge, a backlink, a co-mention signal for `related`, terms in the full-text index, a vector, a summary, and possibly entries in `topic_index` or an entity page. A partner who is allowed to see Sarah's contact card must not learn any of that: not the text, not that a note exists, not a count that went up by one.

### What dpkms does today

Authorization is scope-only (ADR-079 §5). Every principal that holds `read:objects` reads everything. Focus profiles are a lens the caller chooses, not access control. `FindRequest.Profile` says so in its own comment (`internal/service/find.go`), and `auth.Principal` carries no profile.

A survey of every read path in `internal/server` and `internal/mcp` (full table in the [plan appendix](../plans/2026-10-14-auth-and-sharing.md#appendix-a-read-path-inventory)) found:

| Finding | Where | Consequence for sharing |
|---|---|---|
| `ObjectFilter.ProfileID` is applied by `List`, `FTSSearch` and `VectorSearch` only when the caller passes it | `internal/storage/sqlite/objects.go`, `postgres/objects.go` | No route ties it to the caller |
| `Get`, `EdgeStore`, `EntityStore`, `EmbeddingStore`, `ProximityStore` take no filter at all | `internal/storage/storage.go` | Object by ID, related, edges, backlinks, entities, pages and stats ignore every scope |
| `FTSSearch` and RSQL `ListBySQL` do not apply `Status` | `sqlite/objects.go` FTS query; `postgres/objects.go` | Inbox and discarded objects show up in search today (an existing bug) |
| SQLite vector search over-fetches ×10, then filters in Go | `sqlite/objects.go` `VectorSearch` | A narrow viewer gets too few results; recall depends on the filter |
| Hybrid rerank counts mentions over all objects (`CountMentionsTo`); alias expansion uses the global scope | `internal/ranking/reranker.go`, `internal/service/service.go` | Hidden objects change the scores of visible ones |
| Searchgraph reads mention edges of kept objects with no scope; "similar" edges read stored vectors | `internal/searchgraph/build.go`, `relations.go` | Graph shows entities that only hidden objects mention |
| RSQL `mention` and `related` compile to subqueries over all edges | `internal/search/compiler.go` | A visible object can be returned *because of* a hidden one |
| `GET /events` broadcasts every bus event to every subscriber | `internal/server/http/handlers_events.go` | IDs, types, tags and profile of all writes reach every reader |
| Job payloads are the submitted content | `storage.Job.Payload`; `GET /jobs/{id}` under `read:jobs` | Raw content outside the object store |
| `embeddings.text` stores the embedded text in plain text | `internal/pipeline/steps/embedding.go`; migration `032_embedding_models.sql` | A second copy of content, outside masking |
| Entity pages concatenate snippets from many sources and are stored as objects with `ProfileID ""`; `topic_index` is one global object holding one-line summaries of every object | `internal/service/page.go`, `internal/service/index.go` | Multi-source derived objects that are visible under any filter that allows their type |
| Content-hash dedup (`Reinforce`) merges tags, mentions and content into the existing object whatever its profile | `sqlite/objects.go` `Reinforce` | A write from one context changes an object in another |
| Contacts from CardDAV (`cardamum`) are objects of type `contact`; email is in `metadata.email` **and** in the content text, and the vCard `NOTE` is appended to the content | `internal/ingest/cardamum/cardamum.go` | Hiding a metadata field does not hide the email, and the personal note sits inside the contact itself |

Two things in the existing model help:

- **Entities carry no content-derived facts today.** `entity_resolver` only creates thin placeholders (slug, title, namespace) and `mentions` edges. Full entity records come from registries. `structured_metadata` writes `people` and `topics` onto the *object*, not the entity. So the "facts attached to Sarah's entity" problem does not exist yet. This ADR fixes the rule before the first enrichment step creates it.
- **Lineage already exists at two levels.** Graph node IDs encode their object (`<objectID>/<nodeType>/<ordinal>`, `internal/storage/node_identity.go`). Every pipeline step declares the fields it `Requires` and `Produces` (`pipeline.StepContract`), which gives field-to-field lineage inside an object for free.

Response-only filtering (strip what the caller may not see from the JSON) was reviewed and rejected as the primary mechanism. Totals, facet counts, pagination, ranking and derived entity attributes are all computed before the response exists, so they leak. And every new route would have to remember to filter.

---

## Decision

**dpkms enforces sharing in two layers that every read path goes through: storage-level visibility (which rows exist for this viewer) and a render layer (which fields and values of those rows the viewer sees). Both are compiled from named share policies granted to audiences of principals. Derived data inherits visibility from its sources through recorded provenance. Search for restricted viewers runs on a masked shadow index. A fail-closed response backstop checks every object and entity ID, and every masked value, before a response leaves the server. The owner bypasses all of it by default.**

### 1. Vocabulary

| Term | Meaning |
|---|---|
| **Principal** | ADR-079: owner, person, device, peer. An agent is a key on one of them, not a principal. |
| **Audience** | A named, owner-managed group of principals (`colleagues`, `partners`). A principal can be in several. |
| **Share policy** | A named, reusable rule: data filter + exclusions + field projection + redaction + capabilities. Versioned. |
| **Grant** | Audience × share policy, with an optional expiry. Grants are the only thing that gives a non-owner any data. |
| **Viewer** | The compiled, per-request form of "what may this principal see": the set of grants that apply to it, narrowed by the API key's own policy, if any. |
| **Content class** | A sensitivity label on an object or on a part of one (`personal`, `contact_details`, `financial`, …). Policies exclude by class. |

### 2. Share policies

```yaml
# dpkms share policy, stored in the instance DB, edited with `ctxt share policy`
name: colleagues-network
version: 3                      # bumped on every edit; caches key on it
data: "type==contact;tag=in=(work,client)"     # RSQL (ADR-050), compiled to SQL
exclude_classes: [personal]                    # objects and parts with these classes are gone
fields:
  contact: [title, org, role, tags, city]      # per-type allowlist of rendered fields
  "*": [title, summary, tags, created_at]      # every other visible type
redact:
  mask: [email, phone, address]                # PII categories masked in visible text
  reveal: []                                   # categories or value patterns shown anyway
  style: category                              # category ("[email]") | partial ("s…@acme.com")
capabilities:
  search: [fts, vector]                        # graph | fts | vector | rsql
  entities: true                               # entities reachable through visible objects
  export: false
  mcp: true
  events: false
---
name: partners-network
version: 1
data: "type==contact"
exclude_classes: [personal]
fields: { contact: [title, org, role, email, phone, tags, city] }
redact: { mask: [], reveal: [] }
capabilities: { search: [fts, vector, graph], entities: true, export: false, mcp: true, events: false }
```

Rules:

- **Deny by default.** A principal that is not the owner sees nothing unless a grant applies to it.
- **Union across grants.** An object is visible if *any* grant's `data` matches and that same grant's exclusions do not remove it. The viewer sees the union of the fields the matching grants allow. A category stays masked only if *every* matching grant masks it.
- **Exclusions are per grant.** `exclude_classes` in one grant does not hide data another grant shows.
- **A per-principal ceiling is the hard guarantee.** `principals.deny_classes` lists classes a principal may never see, whatever its grants say. It is ANDed into the predicate after the grant union and applied to parts in the render layer. A partner who is also a colleague keeps both views and still never sees `personal`. One column; no audience gymnastics.
- **Keys narrow, never widen.** An API key bound to a share policy (ADR-079: `principal_identities.share_policy_id`) intersects with its principal's view. An agent is such a key (ADR-079 §1), so it is never broader than the person it acts for.
- **Expiry everywhere.** Grants, audience memberships, invites and keys each carry an optional expiry. The shortest one wins.
- **Owner bypass.** The owner sees raw data and pays no cost. `ctxt --as <principal|policy>` (header `X-Ctxt-View-As`, owner only) runs a request through another viewer, so the owner can check what a colleague sees before sending the invite. That is also how the owner gets masking on their own requests (for example, during a screen share).
- **The data filter is RSQL**, already compiled for SQLite and Postgres by `internal/search.CompileFor`. The compiler handles `type`, `subtype`, `pipeline`, `source`, `tag`, `mention` and the date fields today. It gains `profile`, `topic`, `class` and `metadata.<key>` for policies. The `related` and `mention` operators are refused inside a policy, because they reach across objects.

### 3. Content classes

A class marks something the owner may want to withhold whatever the data filter says.

| Where a class lives | Set by |
|---|---|
| Object: `objects.classes` (JSON array) | Config rules evaluated at ingest (`sharing.classes.rules`: by type, source, pipeline, tag or profile, for example `type==note;profile==personal → personal`); the LLM `structured_metadata` type (`person_note` → `personal`); manual `ctxt object classify` |
| Part of an object: graph node `metadata.classes`, or a field class from the type renderer (§5) | The step that produces the part. Structured types declare classes per field (contact: `email`, `phone`, `address` → `contact_details`; `note` → `personal`). |

Unclassified objects are visible to any grant whose data filter matches. Rules can declare a default class for whole sources. The plan proposes `personal` as the default for typed notes (Decision D5).

### 4. Storage-level visibility (rows)

- **The viewer travels in the context.** After authentication, the middleware resolves the principal's grants and attaches a `sharing.Viewer`. For the owner it is the `Unrestricted` sentinel. For anyone else it holds the compiled SQL predicate for objects (per dialect), the policy versions, and the render plan.
- **Reads go through a scoped store.** `storage.ScopedDriver(viewer)` wraps the driver. Every object read (`List`, `Get`, `FTSSearch`, `VectorSearch`, `ListBySQL`, counts, facets) ANDs the viewer predicate into its SQL. Every edge, entity, embedding, proximity, alias, page and saved-search read is rewritten to join against visible objects (§6). Handlers in `internal/server`, `internal/mcp` and the export path get only the scoped driver. The pipeline, jobs and housekeeping keep the unscoped driver, which they must name explicitly (`storage.System(driver)`). A lint check bans the raw driver in server and MCP packages (forbidigo, newly enabled in `.golangci.yml`; neither forbidigo nor depguard runs today).
- **The predicate is computed, not materialized.** `(objects.status = 'active') AND ((g1.data AND NOT g1.excluded) OR (g2.data AND NOT g2.excluded) …)` over indexed columns. It is always current, needs no rebuild job when a policy changes, and is cheap for the filters a share policy uses. A materialized `object_grants(object_id, grant_id)` table is the fallback if profiling shows the predicate is too slow on Postgres with large tag sets.
- **The status bug is fixed on the way.** `FTSSearch` and `ListBySQL` start applying `Status` for every viewer.
- **Hidden means absent.** An object, entity or edge outside the viewer's predicate is answered as if it did not exist: `GET /objects/{id}` is 404, a write to it is 404, a list omits it. 403 `NOT_SHARED` is reserved for routes a capability closes (export, events, jobs), where the route itself is withheld, not a row. A 403 on a row would confirm the row exists.
- **Dedup stays inside a profile.** `Reinforce` (content-hash dedup) merges tags, mentions and content only into an existing object with the same `profile_id`. The same content arriving under another profile becomes its own object. Otherwise a private capture would write its tags onto an object a viewer can see.
- **Vector search filters before ranking.** Postgres puts the predicate in the KNN query (pgvector iterative scan). SQLite keeps over-fetching, but in rounds: it widens until it has `k` visible hits or the index is exhausted. The visibility check runs in SQL on the candidate IDs. A sqlite-vec `vec0` metadata column (probed: v0.1.6 supports `IN` filters inside a KNN query) can pre-filter by `classes` or `type` when one policy dominates.
- **Per-principal state.** Search history, saved searches, reminders and the resurfacing queue get an `owner_principal` column. A restricted viewer sees only its own rows.

### 5. Render layer (fields and values)

Rows that pass §4 go through `sharing.Render(viewer, obj)` before any encoder sees them: REST, gRPC, MCP, SSE, export and composition all use the same function.

1. **Field projection.** Only the fields that the matching grants allow for the object's type. IDs, type and `created_at` are always kept.
2. **Structured types render from fields.** A type with a field schema (contact first) gets its text rebuilt from the visible fields, instead of the stored text being masked. The contact card a colleague sees is `Sarah Lee | Head of Ops @ Acme`, because `email`, `phone` and `note` are not projected. This is the only reliable way to hide a field that ingest also copied into the text, as cardamum does today. Contact ingest is changed to keep `note` as a separate `personal`-class graph node and to stop copying email into the text.
3. **Derived fields follow their inputs.** Each derived field (summaries, sections, decisions, tasks, graph nodes, embedding text) is shown only if *all* its input fields, by the step contracts of the pipeline that produced the object, are projected and contain no masked span. Otherwise it is dropped. Contracts name whole fields, so `Metadata` is coarse: a metadata key written by a step counts as derived from everything that step `Requires`, unless the step declares a narrower input. A dropped field is never regenerated per viewer: regeneration per policy would cost an LLM call per object per policy and would still not be provably clean. (Policy-specific summaries are a possible later addition, behind Decision D7.)
4. **Span masking.** A built-in `pii_detect` step annotates spans in every text field, including summaries, sections and graph node content: `(field, node_id, start, end, category, value_hash)`. The raw text is kept. Render replaces masked spans with the category token or the partial form. Detection is a provider role with a local default (rules plus checksum validators for email, phone, IBAN, card numbers and addresses) and an optional local NER model. A cloud detector is opt-in only.
5. **Exclusion beats masking.** Masking depends on a detector that can miss things. Projection and class exclusion do not. The docs say so: put content that must never reach a viewer behind a class or a field, not behind a mask.

### 6. Derived data inherits source visibility

| Derived artifact | Provenance it relies on | Rule for a restricted viewer | Filter or recompute |
|---|---|---|---|
| Intra-object parts: summaries, sections, graph nodes, decisions, tasks, chunks | Node ID prefix; step contracts | Visible iff the object is visible, the part's class is not excluded, and its inputs pass §5.3 | Filter |
| FTS hit, snippet, highlight | `objects_fts` row = one object | Run on the masked shadow index (§7). Snippets come from the rendered view, never from stored text. | Filter + shadow |
| Embedding, vector hit, "similar" edge | `embeddings(object_id, …)` | Masked-variant vector (§7). `embeddings.text` never leaves the server. | Filter + shadow |
| Object ↔ object edge (links, `related`) | `edges.from_id`/`to_id` | Visible iff both ends are visible | Filter |
| Object → entity `mentions` edge, backlink | `edges.from_id` = source object; the mention's node ID gives its field | Visible iff the source object is visible and the mention's node or field is projected and unmasked | Filter |
| Entity (existence, title, aliases) | Registry origin, or mention edges | Registry-sourced: visible if `capabilities.entities` is on and the existing namespace entitlement gate allows it. Placeholder (content-derived): visible only while at least one visible mention points to it. | Filter (EXISTS over visible mentions) |
| Entity attributes and descriptions merged from content (none today) | Required: `entity_facts` with source object, node and span | Only facts whose source passes the mention rule. A step may not write content-derived attributes into `entities.metadata`. | Filter at read |
| Mention counts, facet counts, `stats`, totals, pagination | Same predicate | Always computed inside the scoped query | Recompute (SQL) |
| Rerank signals (`CountMentionsTo`, reinforcement) | Edges | Computed over visible objects | Recompute (SQL) |
| Searchgraph | Built from the scoped `Find` results + filtered edges | Built only from rendered, visible results. The trace omits cut candidates. | Recompute |
| Entity page, `topic_index` (multi-source derived objects) | `SourceIDs` in metadata | Never served to a restricted viewer as stored. Built at read time from visible sources only, or omitted. | Recompute or omit |
| Proximity / `related` scores | Co-occurrence over all objects | Neighbors filtered to visible. Scores recomputed from visible co-mentions. | Recompute (SQL) |
| Events / SSE / watch streams | Event payload object IDs | Delivered per subscriber after a visibility check on the payload's object. Event types without an object (jobs, pipelines) are owner-only. `/ws/bus` stays peer-only. | Filter |
| Jobs, inbox, capture/recent | Payload = raw content | Owner-only scopes. They never belong to a share policy. | Deny |
| Export (`ctxt export`, bundles) | Scoped reads | Export as a restricted viewer is the rendered view, only if `capabilities.export` allows it | Filter |
| Federation push (ADR-064) | Syncer reads | Peers stay owner-trusted replicas. Pushing a policy-filtered view to someone else's instance is a later addition (a peer principal with a share policy). | Out of scope |
| MCP tools | Same service calls | Inherit everything above. Capability `mcp` gates the mount for the viewer. | Filter |

Provenance changes this ADR requires:

- `edges.metadata.source_node` on every `mentions` edge, written by `entity_resolver`, `jobs/worker` and `service/fanout`. Today they write empty metadata.
- `entity_facts(entity_slug, key, value, source_object_id, source_node_id, span_start, span_end, step, confidence, created_at)` before any step derives entity attributes from content.
- `metadata.source_ids` on every multi-source derived object (entity page, topic index, future digests), plus `classes: [derived]`. Restricted viewers never receive stored derived objects.
- `objects.owner_principal` on per-principal rows (§4).

**Filter, don't recompute, by default.** Everything that can be decided with a SQL join over provenance is filtered at read time. Only aggregates (counts, scores, multi-source pages) are recomputed, and only in SQL or from already-rendered inputs. Nothing is cached per policy except the compiled viewer (`principal × policy versions`, invalidated on any grant, policy, audience membership, ceiling, key or principal change) and, if profiling needs it, placeholder-entity visibility keyed by `(policy versions, change_seq high-water mark)`.

### 7. Search must not leak masked or hidden terms

- **Hidden objects** are handled by §4: they are not in the candidate set, so they cannot match, rank or count.
- **Masked terms inside visible objects** need a shadow index. `projected_fts_body_masked` is the FTS projection with *every* detected PII span replaced by its category token and every non-default class part removed. It has its own `objects_fts_masked` table (SQLite) or `fts_masked` tsvector (Postgres). Restricted viewers always query the masked index. One shadow index, not one per policy.
- **Revealed categories stay searchable.** A policy that reveals a category (partners see email) gets matching through `pii_terms(object_id, category, value_norm)`. The query matches the masked FTS **or** an exact `pii_terms` row in a revealed category. A colleague searching for a full email address therefore gets nothing, and a partner gets the contact.
- **Projection below the searchable minimum does not hide terms.** The masked index is policy-independent, so it cannot follow per-policy field projection. Each structured type declares a `SearchableMinimum` field set (contact: `title`, `org`, `role`, `tags`, `city`), and only those fields go into the masked body and the masked vector. Fields outside the set (`email`, `phone`, `address`, `note`) match only through `pii_terms` for revealed categories, or not at all. A policy that projects out a field inside the minimum (hides `org`) still lets a viewer match on it, though the rendered result will not show it. The docs say so: a field that must not even match goes behind a class, not behind projection.
- **Embeddings of visible objects with masked spans or projected-out fields** get a masked-variant vector computed from the masked projection. It is stored under the same `(object_id, model_id, chunk_idx)` key plus a `variant` column (`raw` | `masked`), which extends ADR-071's key. It is written only when sharing is enabled and only for objects that have spans or excluded parts. Everything else uses one vector for both variants. Restricted viewers search the masked variant.
- **The RSQL `mention` and `related` operators** in a restricted viewer's query compile their edge subqueries with the viewer predicate on both sides.
- **Snippets and highlights** are cut from the rendered text.
- **Ingest providers still see raw text.** Embedding, summarization and NER run on the owner's data with the owner's providers, as today. Per-viewer masking protects viewers' reads, not the providers. That is what ingest-time masking (§10) is for.

### 8. Fail-closed response backstop

A middleware sits after the handler and before the encoder, on REST, gRPC (interceptor), MCP and SSE. Every route declares what its response carries (`objects`, `entities`, `mixed` or `none`) next to its scope in the route tables, and the backstop runs only on the first three. It checks two things:

1. **ID ledger.** The scoped driver records every object ID, entity slug and edge ID it released for the request. The backstop walks the known ID-bearing paths of the declared response shape (`objects[].id`, `results[].id`, `related[].id`, `edges[].{id,from_id,to_id}`, `entities[].slug`, `backlinks[].from_id`, `metadata.source_ids[]`, and node IDs, which carry their object ID as prefix) and removes any value not in the ledger. It never keys on a bare field name: object IDs are opaque strings with no format of their own, and `id` also names principals, audiences, policies, grants, jobs and sessions. A route that returns objects through a path the walker does not know fails the leak suite, which is where the path gets added.
2. **Masked-value canary.** The scoped driver also hands the backstop the raw values of each released object's masked spans and projected-out fields, held in memory for the request only (they are the owner's data on the owner's server, and a hash cannot be substring-searched). Values shorter than six runes are skipped, so a first name or an area code does not blank legitimate text. Any raw value found in the body is replaced with its category token.

Any hit is stripped, logged as a `security.visibility_backstop` event with route, principal and policy versions, and counted. In tests and in `--strict-sharing` mode a hit returns 500 instead, so the leak suite fails loudly. The backstop is a safety net: a hit is a bug in §4–§7 to fix, not normal operation.

### 9. Audit: who saw what

Every request by a non-owner principal appends to `access_log(at, principal_id, identity_id, route, policy_versions, object_count, entity_count)`. The ID list itself goes to a separate `access_log_objects` table with configurable retention (default 90 days). Entries are buffered and flushed in batches by one writer goroutine, so a read request never waits on the SQLite writer; entries still in the buffer at a crash are lost, and the docs say so. The owner can ask "who saw Sarah" (`ctxt share audit --object <id>`) and "what did this partner see" (`--principal`). Owner requests are not logged by default. Grant, policy, invite and revocation changes go to the existing `audit_log`.

### 10. Ingest-time masking stays as "never store"

| | Ingest-time masking (custom step, ADR-058, `insert_steps`) | Per-viewer masking (this ADR) |
|---|---|---|
| Stored | Masked text only. The raw value is gone. | Raw text + spans |
| Owner sees | Masked | Raw |
| Providers (LLM, embeddings) see | Masked | Raw |
| Reversible | No | Yes, by changing the policy |
| Use for | Secrets and regulated data the owner never needs: card numbers, credentials, health data | Data the owner uses but shares selectively: emails, phones, personal notes |

Both use the same detector. ADR-058's writable fields gain `pii_spans`, so an installed step can annotate instead of rewriting. The custom-steps guide gets a section on choosing between the two. The guide's known gaps (job payloads, `--raw`, media originals, `source`) also apply to per-viewer masking. Job payloads are owner-only for that reason.

### 11. Management surface

| Command (ctxt, over the API) | Scope |
|---|---|
| `ctxt share audience create|add|remove|list` | `admin:shares` |
| `ctxt share policy create|edit|show|list` (YAML in, versioned) | `admin:shares` |
| `ctxt share grant <audience> <policy> [--expires]`, `ctxt share revoke <grant>` | `admin:shares` |
| `ctxt share ceiling <principal> --deny-classes personal,…` | `admin:shares` |
| `ctxt share invite --audience <a> [--expires 7d]` → link (ADR-079) | `admin:shares` |
| `ctxt share preview --as <principal|policy> <command…>` (any read command) | owner |
| `ctxt share audit [--principal|--object]` | `admin:audit` |

The web UI gets the same operations under a Sharing page. A shared viewer's UI shows a "shared by <owner> · <policy>" banner and hides the controls its capabilities do not allow.

---

## Rationale

- **Storage first, because the leaks are aggregate.** Counts, totals, ranks, co-mention scores and entity existence are all computed in SQL. The only place to stop them is the query that computes them. A scoped driver makes "forgot to filter" a compile-time or lint error instead of a leak.
- **A backstop anyway, because joins get forgotten.** The inventory found five storage interfaces that take no filter at all. New ones will appear. An ID ledger turns a missed join into a stripped field and an alarm instead of a silent leak. The canary does the same for masking.
- **Render from fields for structured types.** Span masking cannot reliably hide something ingest copied into free text. Rebuilding the text from projected fields can, and the contact card is exactly that kind of type.
- **One shadow index, not one per policy.** Indexes per policy multiply storage and ingest cost by the number of policies and go stale on every edit. The masked index plus `pii_terms` gives exact per-policy matching with a constant 2× cost on FTS and, only for objects with spans, on vectors.
- **Filter over recompute.** Recomputing derived data per policy (summaries, entity descriptions) costs LLM calls and is still not provably clean. Filtering by recorded provenance is cheap, deterministic and testable. Aggregates are the exception and they are plain SQL.
- **RSQL for data filters.** It already compiles to both dialects and it is the language users already search with. CEL (kit `runtime/policy`) cannot be pushed into SQL, so it would force post-filtering, which is exactly what leaks counts.

### Alternatives considered

| Alternative | Why not (as primary) |
|---|---|
| Response-only filtering (strip in middleware) | Counts, pagination, ranking and derived attributes leak, and every new route must opt in. Kept as the backstop (§8). |
| A separate instance per audience, fed by federation | Strong isolation, but a copy per audience, no live updates without push, and federation pushes whole objects (no projection). A possible later "publish to a partner's instance" built on this ADR's render layer. |
| Per-audience encryption (ADR-019 shared keys) | Solves storage at rest on someone else's server, not per-viewer reads on the owner's own server. Orthogonal; could come later. |
| Materialized per-principal visibility tables | Rebuilds on every policy edit, and lag means a revoked grant still shows data until the rebuild ends. Kept as a performance fallback for objects only. |
| Mask at ingest only (custom step) | The owner loses their own data. It cannot vary by viewer. Stays as the "never store" option (§10). |
| Regenerate summaries and entity text per policy | LLM cost per object × policy, non-deterministic, and still can echo hidden facts. Rejected; policy-specific summaries only as an opt-in later addition (D7). |
| Three ADRs (grants, masking, derived data) | They share one vocabulary, one viewer object and the same enforcement points. Splitting them would repeat §4–§8 three times and let the pieces drift. Authentication is split off (ADR-079) because it is useful without sharing and partly belongs in kit. |

---

## Consequences

### Positive

- The contact case works: colleagues see names, roles and companies, partners also see details, and neither sees notes or anything derived from them.
- Every read path, existing and future, inherits visibility through the scoped driver, and the backstop catches the rest.
- Today's bugs get fixed along the way: search ignoring `Status`, cross-scope rerank signals, the event stream broadcasting everything.
- The owner can preview any viewer and audit who saw what.

### Negative

- Every storage read interface changes signature or gets a scoped wrapper. That is a large mechanical refactor across SQLite and Postgres, with conformance tests for both.
- Ingest does more work when sharing is on: PII detection on every text field, a second FTS projection, and masked vectors for objects with spans.
- Restricted viewers get worse recall: they search masked text and cannot use cross-object RSQL operators beyond what they see.
- PII detection is best effort. The docs must steer owners to classes and projection for anything critical.
- The response backstop adds a JSON walk to every non-owner response. The owner path skips it.

### Neutral or Considerations

- `private` instances have one principal (the owner), so sharing is a `protected`/`public` feature.
- Focus profiles stay a lens for the owner. A share policy may *use* `profile` in its data filter, but profiles grant nothing.
- Multi-source derived objects become owner-only unless rebuilt per view. The topic index for a colleague is built on read from what they can see.
- Federation and device sync are unchanged. Both replicate to owner-trusted targets.

---

## Implementation Notes

### Schema sketch

```sql
CREATE TABLE audiences (id TEXT PRIMARY KEY, name TEXT UNIQUE NOT NULL, created_at TIMESTAMP NOT NULL);
CREATE TABLE audience_members (
  audience_id TEXT NOT NULL REFERENCES audiences(id),
  principal_id TEXT NOT NULL REFERENCES principals(id),
  expires_at TIMESTAMP, added_by TEXT NOT NULL, added_at TIMESTAMP NOT NULL,
  PRIMARY KEY (audience_id, principal_id));
CREATE TABLE share_policies (
  id TEXT PRIMARY KEY, name TEXT UNIQUE NOT NULL, version INTEGER NOT NULL,
  spec TEXT NOT NULL,                      -- JSON of the YAML above, validated on write
  updated_at TIMESTAMP NOT NULL);
CREATE TABLE share_grants (
  id TEXT PRIMARY KEY, audience_id TEXT NOT NULL REFERENCES audiences(id),
  policy_id TEXT NOT NULL REFERENCES share_policies(id),
  expires_at TIMESTAMP, created_by TEXT NOT NULL, created_at TIMESTAMP NOT NULL, revoked_at TIMESTAMP);

ALTER TABLE principals ADD COLUMN deny_classes TEXT NOT NULL DEFAULT '[]';           -- §2 ceiling
ALTER TABLE principal_identities ADD COLUMN share_policy_id TEXT REFERENCES share_policies(id); -- §2 keys narrow
ALTER TABLE objects ADD COLUMN classes TEXT NOT NULL DEFAULT '[]';
ALTER TABLE objects ADD COLUMN projected_fts_body_masked TEXT NOT NULL DEFAULT '';
CREATE TABLE pii_spans (
  object_id TEXT NOT NULL, field TEXT NOT NULL, node_id TEXT NOT NULL DEFAULT '',
  start_rune INTEGER NOT NULL, end_rune INTEGER NOT NULL,
  category TEXT NOT NULL, value_hash TEXT NOT NULL, detector TEXT NOT NULL,
  PRIMARY KEY (object_id, field, node_id, start_rune));
CREATE TABLE pii_terms (object_id TEXT NOT NULL, category TEXT NOT NULL, value_norm TEXT NOT NULL,
  PRIMARY KEY (category, value_norm, object_id));
CREATE TABLE entity_facts (
  id TEXT PRIMARY KEY, entity_slug TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL,
  source_object_id TEXT NOT NULL, source_node_id TEXT NOT NULL DEFAULT '',
  span_start INTEGER, span_end INTEGER, step TEXT NOT NULL, confidence REAL, created_at TIMESTAMP NOT NULL);
CREATE INDEX idx_entity_facts_entity ON entity_facts(entity_slug);
-- embeddings: add variant TEXT NOT NULL DEFAULT 'raw' to the (object_id, model_id, chunk_idx) key
-- search_history, saved_searches, resurfacing_queue: add owner_principal TEXT NOT NULL DEFAULT 'owner'
CREATE TABLE access_log (id TEXT PRIMARY KEY, at TIMESTAMP NOT NULL, principal_id TEXT NOT NULL,
  identity_id TEXT, route TEXT NOT NULL, policy_versions TEXT NOT NULL,
  object_count INTEGER NOT NULL, entity_count INTEGER NOT NULL);
CREATE TABLE access_log_objects (access_id TEXT NOT NULL, object_id TEXT NOT NULL,
  PRIMARY KEY (access_id, object_id));
CREATE INDEX idx_access_log_objects_object ON access_log_objects(object_id);
```

SQLite migrations continue the numbered series from 047 (045 and 046 are Go migrations already in `internal/storage/sqlite`); Postgres continues `pgMigrations` from version 27. ctxt is unreleased, so existing rows get defaults and no compatibility shims.

### Code map

| Package | Change |
|---|---|
| `internal/sharing` (new) | Policy spec, validation, `Viewer` compile (RSQL → per-dialect predicate), `Render`, ledger |
| `internal/storage` | `ScopedDriver`, `System`; filters on edge/entity/embedding/alias/page/proximity reads; `Status` fix |
| `internal/storage/{sqlite,postgres}` | Predicate injection, masked FTS, `pii_*`, `entity_facts`, variants; conformance tests in `storagetest` |
| `internal/pipeline/steps` | `pii_detect`; `mentions` edges get `source_node`; contact renderer; class rules |
| `internal/server/{http,grpc}`, `internal/mcp` | Viewer middleware/interceptor, response-shape declaration per route, backstop, 404 for hidden rows, per-subscriber SSE filter, scoped driver only |
| `internal/service` | Entity page and topic index built per view; rerank and alias expansion over visible objects |
| `cmd/ctxt` | `ctxt share …`, `--as` |

### Testing

- **Leak suite** (`internal/sharing/leaktest`): a fixture corpus (the Sarah note, contacts with email and phone, an entity page, a topic index, co-mentions). For each fixture principal (colleague, partner, agent key narrower than its partner, expired grant, revoked principal), every read route and RPC in the route table is called. The suite asserts that no hidden ID, masked value or hidden term appears anywhere in the body, the headers or the SSE frames, and that counts equal the visible count. The route table is enumerated from `routes.go` and `methodScopes`, so a new route without a leak case fails the suite.
- **Query oracle cases:** FTS for a masked email, vector search for a hidden note's sentence, RSQL `related==person/sarah`, facet counts, pagination totals, rerank order with and without the hidden note. The visible results must be identical whether or not the hidden data exists. This differential test (run with and without the hidden fixtures, compare) is the strongest one.
- **Backstop mutation:** remove the scoped join from one store method and check that the backstop fires and the suite goes red. Remove the backstop and check that the leak suite still catches the leak.
- **Backstop precision:** a restricted viewer's `whoami`, audience and policy listings and job-status errors come back intact; a fixture with a short projected-out value (`Lee`) does not blank other text.
- **Hidden-row semantics:** `GET /objects/{hidden}` is 404 for the viewer and 200 for the owner; a capability-closed route is 403 `NOT_SHARED`.
- **Ceiling:** a principal in both audiences with `deny_classes: [personal]` never receives a `personal` object or part through either grant.
- **Dedup:** the same content captured under two profiles yields two objects, and the viewer's copy carries only its own tags.
- **Conformance:** every scoped store method, on both drivers, in `storagetest`.

---

## References

- ADR-079, ADR-023 (profile-scoped access: §7 "Profile-Scoped Authorization", not built), ADR-015 (§ graph navigation honors profile scopes: not implemented), ADR-033, ADR-049, ADR-050, ADR-058 (amendment 2026-10-14; custom-steps guide "What masking cannot cover"), ADR-063, ADR-064, ADR-068, ADR-071, ADR-075
- `internal/storage/types.go` (`ObjectFilter`, `Entity`, `Edge`), `internal/storage/storage.go`, `internal/search/compiler.go`, `internal/pipeline/steps/{entity_resolver.go,structured_metadata.go,embedding.go}`, `internal/service/{find.go,page.go,index.go}`, `internal/searchgraph/build.go`, `internal/ingest/cardamum/cardamum.go`
- `docs/manual/admin-extensibility/custom-steps.md`
- sqlite-vec v0.1.6 metadata columns in KNN (probed), pgvector iterative index scans
