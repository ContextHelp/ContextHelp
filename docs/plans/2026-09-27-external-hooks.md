# External hooks on ctxt and dpkms: survey, first increment, open decisions

> **Date:** 2026-09-27
> **Decision record:** [ADR-075 – External Hook Surface](../decisions/ADR-075-external-hook-surface.md) (Proposed)
> **Status:** design only. Nothing here is implemented.
> **Audience:** the owner deciding on ADR-075, then whoever implements the first increment

The goal is that other applications can hook in **before** or **after** something happens in ctxt (actions an operator starts from the CLI) or in dpkms (work inside the daemon). ADR-075 holds the decision and its rationale. This note holds the evidence behind it, the smallest increment that proves both hook kinds, and the questions the owner still has to answer.

An earlier version of this note covered only delivering the CLI's `ctxt.upgrade.embedding_model.*` events to an instance, through one `jobs` row per event. That idea survives in generalized form as §4 of the ADR: an ordered outbox in the instance database that covers events from both processes.

## Survey

### ctxt and dpkms today

| Finding | Evidence | Consequence for hooks |
|---|---|---|
| Most mutating CLI commands write the instance database directly | `newService` → `resolveStoragePath` (`cmd/ctxt/cmd/helpers.go`); `ctxt delete` calls `svc.DeleteObject`; `openEmbeddingsBackend` | The CLI process is itself a writer, so it has to host hooks. The daemon never sees these writes. |
| HTTP routing ignores `--instance` | `clientEndpoints`: `server.urls` → `server.url` → `127.0.0.1:8080` | Anything announced by URL can land on the wrong instance. Hooks and events must bind to the database. |
| `internal/service` is shared by both processes | the CLI builds it in `newService`; `DELETE /api/v1/objects/{id}` and `ctxt delete` both reach `Service.DeleteObject` | This is the natural place to host hooks. |
| Domain events go only to `svc.Bus` (`events.LocalBus`) and SSE | `cmd/dpkms/cmd/serve.go`; `GET /api/v1/events` | No IDs and no resume, so a disconnected consumer loses events. Nothing reaches `/ws/bus`, contrary to what `docs/architecture.md` says. |
| The SSE handler leaks its subscription | `HandleSSE` subscribes `*` on every connection, and `LocalBus` has no unsubscribe | A defect to fix before SSE carries hook traffic. |
| `ctxt.runtime.object.{updated,deleted}` are declared but never published | `internal/events/topics.go`; no publisher | The first after-hook for deletes needs them to be published. |
| `/ws/bus` uses one shared `BUS_TOKEN`, runs without relay, and discards inbound publish errors | kit `network.go` `readLoop`; `AuthFromEnv` | It cannot carry before hooks, and it should not be given to external apps. |
| The veto-capable subscribers on the hub are the kit policy engine's, wired in process | `internal/policy.Init(hubBus)`; `policies_default.yaml` guards deleting and archiving pipelines | This is prior art for in-process `cel` before hooks. |
| The CLI-to-daemon handoff through the `jobs` table already works | `ctxt embeddings migrate` enqueues `embeddings_migrate` | It proves that the database is the right channel. A dedicated ordered table replaces one job per event. |

### Existing hook systems

| System | Scope | What is reused |
|---|---|---|
| **kit `runtime/bus`** | topic grammar `[Source].[Category].[Object].[Action]`; sync handlers veto and async handlers observe; memory, SQLite and network adapters | The grammar, and the `pre_` convention in the Action segment (`pre_persisted`, `pre_transitioned`). It is the in-process bus of both hosts. |
| **kit `runtime/policy`** | a CEL engine, deny-overrides, veto on **three fixed kit topics** | The `cel` handler kind. **Gap:** it rejects adopter topics, so it needs a kit change to accept `ctxt.*.pre_*`. |
| **kit `runtime/domain`** | pre phases are sync and veto-able; post phases are best effort and swallow errors | The phase semantics. |
| **kit `runtime/notify`** | webhook sink, retry, dead-letter, filter; in memory, not durable | The webhook push transport, placed behind the outbox cursor. |
| **nerv** (`poly-nerv`) | a hook dispatcher for **AI-assistant host CLIs**. Its exec handler reads an envelope on stdin and writes `allow/warn/block/rewrite` on stdout; exit codes 0/1/2/3, 4 and above fails open; per-hook timeout of 1–300 s | The decision JSON and exit codes, minus `rewrite`, for `exec` hooks, so nerv handler scripts are reusable. Its layered configs (a lower layer never shadows a higher one) are a model for local hook config. |
| **axon** (`poly-axon`) | the canonical host-CLI hook contract, with nerv as a consumer. Its `spec/events.yaml` says: *"hop-top's own event vocabularies do NOT belong"* | Its explicit exclusion is why ctxt and dpkms events do not go into axon or nerv. |
| **xat** | a conformance harness for AI-assistant plugins against host contracts | Nothing yet. A later step could reuse its pattern (one spec, run against each host) for ctxt hook conformance. |

**Conclusion.** No existing cross-application contract covers events that hop-top apps raise themselves. kit is the substrate those apps share, so the contract is kit-native, and nerv's handler shape is borrowed so that handlers stay portable. ctxt already consumes nerv's `MemoryRead` and `MemoryWrite` events, and that does not change.

## Options

ADR-075 covers the options in full. In summary:

| Option | Verdict | Deciding reason |
|---|---|---|
| **A. kit-native hosts at the service layer, with a transactional outbox and a nerv-compatible decision JSON** | **Recommended** | The database-bound registry and outbox are correct for `--instance`, remote Postgres and running with no daemon. Before hooks run where the write happens. After hooks survive consumers and the daemon being down. |
| B. nerv as the dispatcher | Rejected | Outside axon's and nerv's scope. nervd is per machine, so a remote dpkms cannot reach it. |
| C. Everything over `/ws/bus` | Rejected | No reply channel, a shared secret with no principal, no durability, and targeting by URL. |
| D. The daemon as the only host, with every CLI write routed through the API | Deferred | A single point of enforcement, but it breaks running with no daemon and reintroduces the URL split. |
| E. Server-side change detection | Rejected | Loses intent, and collapses transitions that happen while the daemon is down. |
| F. One `jobs` row per event (the earlier version of this note) | Superseded by A | Covers CLI events only, with no ordering and no replay. |

## Hookable events: the initial catalog

| Pre (veto-able) | Post | Host | Increment |
|---|---|---|---|
| `ctxt.upgrade.embedding_model.pre_promoted` | `ctxt.upgrade.embedding_model.promoted` | CLI | 1 |
| `ctxt.upgrade.embedding_model.pre_deprecated` | `ctxt.upgrade.embedding_model.deprecated` | CLI | 1 |
| `ctxt.upgrade.embedding_model.pre_purged` | `ctxt.upgrade.embedding_model.purged` | CLI | 1 |
| `ctxt.runtime.object.pre_deleted` | `ctxt.runtime.object.deleted` | CLI or daemon (service layer) | 1 |
| none | `dpkms.upgrade.embeddings_migration.{completed,failed}` | daemon | 1 |
| none | `ctxt.runtime.hook.failed` (every before hook that failed open) | both | 1 |
| `ctxt.runtime.object.pre_ingested` | `ctxt.runtime.object.ingested` | daemon (worker) | 2, once the hot-path latency budget is measured |
| none | `ctxt.runtime.job.{completed,failed}`, `dpkms.upgrade.reingest.{completed,failed}` | daemon | 2 |

## First increment

It proves both hook kinds end to end on a small set, with no kit change and no webhooks.

1. **Storage.** Add a migration on each engine (the next SQLite and Postgres versions):
   - `event_outbox (seq, id, topic, source, occurred_at, origin, actor, instance_id, payload)`
   - an index on `seq`
   - `instance_meta (instance_id)`, seeded once
2. **The `internal/hooks` package.**
   - `Host.Before(ctx, Event) (Decision, error)` runs the handlers in order, merges their decisions deny-overrides, and applies timeouts and `on_failure`.
   - `Host.Emit(tx, Event)` appends to the outbox inside the caller's transaction.
   - The catalog is loaded from `contracts/hooks/events.yaml`.
   - Handlers in this increment are `exec` only, from local `hooks.yaml`.
3. **Wiring.**
   - `Service.DeleteObject` gets a pre and a post event, and writes the post event in its transaction.
   - The three embedding-model registry operations (`SetDefault`, `Deprecate`, `Purge`) take an emit hook inside their existing transactions.
   - The CLI's `KIT_BUS_SINK` publish stays.
4. **Delivery.**
   - The daemon's dispatcher publishes outbox rows to `svc.Bus`.
   - `GET /api/v1/events?after=<seq>&limit=` serves pull.
   - SSE emits `id: <seq>`, resumes from `Last-Event-ID`, and unsubscribes on disconnect (the leak fix).
   - `ctxt events deliver` covers installations with no daemon; `ctxt events tail` covers debugging.
5. **Out of scope for this increment:** `cel` hooks on ctxt topics (they wait on kit), `webhook` hooks for both pre and post, the database hook registry (`ctxt hooks …`), the `/ws/bus` mirror, and retention and `lagging`.

### Test gates

- **A before hook blocks.** An `exec` hook exiting 2 blocks `purge`. Nothing is deleted and no outbox row is written. The error names the hook.
- **Warnings accumulate.** Exit 1 proceeds, and the message is shown.
- **Timeouts follow the failure policy.** With the default `open`, the operation proceeds and emits `ctxt.runtime.hook.failed`. With `closed`, it blocks.
- **Dry runs.** The handler sees `dry_run: true`, a block reads "would be blocked", and no row is written.
- **Atomicity.** A mutation that rolls back leaves no outbox row, checked by fault injection between the mutation and the commit.
- **Replay.**
  - Start a daemon after CLI purges: the pull consumer receives every event in `seq` order.
  - Disconnect and reconnect SSE with `Last-Event-ID`: no gap, and any duplicates share an `id`.
- **Targeting.** `--instance B` writes its outbox rows to B's database, never A's.
- **Mutation checks.** Removing the in-transaction emit, the `seq` ordering or the block short-circuit makes the tests fail.
- **Postgres.** The same suite runs under `-tags=integration`.
- **Live servers.** Never contact the live servers. Use temporary databases and isolated `HOME` and `XDG_*`.

## What `docs/architecture.md` should say afterwards

It replaces the "kit/bus integration (recent)" section:

> **Events and hooks (ADR-075).** ctxt and dpkms raise kit-grammar events at the service layer, in whichever process performs the mutation: the CLI for direct writes, the daemon for API and worker work.
>
> *Before hooks* (`…pre_<action>`) run synchronously there. They are `cel` (kit policy), `exec` (the nerv-compatible decision JSON) or `webhook` handlers, and they can allow, warn or block, never rewrite. Each has a timeout and an `on_failure` policy, open by default.
>
> *After events* are appended to the instance's `event_outbox` in the same transaction as the change. dpkms delivers them at least once and in order per subscription: by pull (`GET /api/v1/events?after=`), resumable SSE, webhooks, and a best-effort mirror of post topics onto `/ws/bus` for trusted peers.
>
> `BUS_TOKEN` authenticates that peer mesh only. External apps use the authenticated `/api/v1` surfaces. Hooks bind to the instance database, never to a URL, so `--instance`, remote Postgres and running with no daemon behave the same.

Until the first increment lands, the accurate statement is narrower: domain events reach only `svc.Bus` and SSE, and `/ws/bus` carries policy traffic and whatever peers publish.

## Decisions the owner must make

1. **The contract home.** Accept kit-native hosts with a nerv-compatible decision JSON, rather than extending nerv or axon, whose scope rule excludes app vocabularies.
2. **What a before hook can do.** Confirm veto-only (`allow`/`warn`/`block`) with no `rewrite`.
3. **The default failure policy.** Open everywhere (the nerv precedent), or closed by default for destructive events (`pre_purged`, `pre_deleted`)?
4. **Where hooks are registered.** Allow the instance database registry for `cel` and `webhook` hooks (recommended: it binds to every writer), or keep all hooks in local config? `exec` stays local-only either way.
5. **The kit change.** Should kit policy accept adopter `pre_*` topics? If yes, it is filed in kit.
6. **`/ws/bus`.** Mirror post topics onto it for peers (recommended), and confirm that `BUS_TOKEN` is never issued to external apps.
7. **Retention.** The default window (7 days is proposed), and what happens to a `lagging` subscription.
8. **The outbox and ADR-074's `sync_log`.** Keep them as separate tables (recommended: announcements versus replication operations), or share one log.
