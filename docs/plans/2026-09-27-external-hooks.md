# External hooks on ctxt and dpkms: survey, first increment, open decisions

> **Date:** 2026-09-27
> **Decision record:** [ADR-075 – External Hook Surface](../decisions/ADR-075-external-hook-surface.md) (Proposed). It holds kit's extension points (K1–K13, cited by file and line) and the full naming audit.
> **Status:** design only. Nothing here is implemented.
> **Audience:** the owner deciding on ADR-075, then whoever implements the first increment

The goal is that other applications can hook in **before** or **after** something happens in ctxt (actions an operator starts from the CLI) or in dpkms (work inside the daemon).

The owner has ruled that kit is the authority on naming and on the hook and policy machinery. ctxt conforms, and no kit change is requested. That rule drives this revision:

- **Before hooks** attach to kit's three veto-able topics, and each request is described through `policy.ContextAttrsKey`.
- **ctxt no longer mints `ctxt.*.pre_*` topics.** The previous revision proposed them.
- **After events** keep ctxt-owned topics that pass `ValidateTopic`, delivered through an instance-database outbox.

## Survey

### ctxt and dpkms today

| Finding | Evidence | Consequence |
|---|---|---|
| Most mutating CLI commands write the instance database directly | `newService` → `resolveStoragePath` (`cmd/ctxt/cmd/helpers.go`); `ctxt delete` → `svc.DeleteObject` | The CLI process is a writer, so it has to host hooks. |
| HTTP routing ignores `--instance` | `clientEndpoints` in `cmd/ctxt/cmd/helpers.go` | Hooks and events bind to the database, never to a URL. |
| `internal/service` is shared by both processes | the CLI's `newService`; `DELETE /api/v1/objects/{id}` | This is where hooks are hosted. |
| Domain events go to `events.LocalBus` (`svc.Bus`) and SSE, with no IDs and no resume | `cmd/dpkms/cmd/serve.go`; `internal/server/http/handlers_events.go` | Nothing reaches `/ws/bus`, contrary to what `docs/architecture.md` says. The SSE handler also leaks subscriptions. |
| ctxt already hooks into kit's veto seams | `domain.Service[Pipeline]` on the hub bus; `internal/policy/policies_default.yaml` (`context.request_attrs.action`); `internal/lateral/{promote,reject}/handler.go` (`Engine.Decide` on `kit.runtime.entity.pre_persisted`); `internal/adapter/events.go:32-55` (the ADR-065 rule to route policy-gated mutations through `domain.Service[T]`) | This design generalizes an existing ctxt pattern rather than inventing one. |
| The CLI-to-daemon handoff through the `jobs` table already works | `ctxt embeddings migrate` | The database is the right channel, and an ordered outbox is better suited to events than jobs. |

### kit (the authority)

The full table with file and line citations is in ADR-075, K1–K13. In short:

- **Grammar.** Topics are 4 segments, lowercase, with snake_case segments and a past-tense Action (`ed` or the whitelist). There is no registry of sources or categories. The first underscore in the Object segment marks a modifier.
- **Before and after.** "Before" is `pre_<past>` (`pre_validated`, `pre_persisted`, `pre_transitioned`). These phases are synchronous and can veto. "After" is a bare past tense, or `post_transitioned`, and is best effort.
- **The veto-able topics are fixed** at `kit.runtime.state.pre_transitioned`, `kit.runtime.entity.pre_validated` and `kit.runtime.entity.pre_persisted`. There is no API or configuration to add any, and a policy rule `on:` any other topic fails when the file loads.
- **How a host plugs in.** It emits on those kit families, through `domain.Service[T]` / `StateMachine` or `Engine.Decide`, and describes the request through `ContextAttrsKey` → `request_attrs`.
  - Rebranding a whole prefix drops the pre phases out of policy.
  - `WithTopics` can rebrand the post phases alone.
- **A remote peer cannot veto** over the network adapter.
- **`ai/ext/hook`'s `before_*` / `after_*` names are not conformant topics.** Do not copy them.

### Other hook systems

| System | Verdict |
|---|---|
| **nerv** (`poly-nerv`) | Its scope is AI-assistant host CLIs. We reuse its handler decision JSON and exit codes (without `rewrite`) for `exec` and `webhook` hooks. |
| **axon** (`poly-axon`) | The host-CLI contract. `spec/events.yaml:6-11` excludes hop-top's own vocabularies, so ctxt events do not belong there. |
| **xat** | A harness for testing host-CLI plugins. A later step could reuse its pattern for ctxt hook conformance. |

## Options

| Option | Verdict | Deciding reason |
|---|---|---|
| **A. Before hooks on kit's veto topics plus `request_attrs`; after events on conformant ctxt topics through an outbox** | **Recommended** | Needs no kit change, `cel` works today, and binding to the database is correct everywhere. |
| A′. ctxt-owned `pre_*` topics (the previous revision) | Withdrawn | Not veto-able under kit policy, so the owner's rule excludes it. |
| B. nerv as the dispatcher | Rejected | Out of scope, and runs per machine. |
| C. Everything over `/ws/bus` | Rejected | No veto path, a shared secret, no durability, and targeting by URL. |
| D. The daemon as the only host | Deferred | Breaks running with no daemon. |
| E. Server-side change detection | Rejected | Loses intent. |
| F. One `jobs` row per event | Superseded by A | No ordering and no replay. |

## Hookable events: the initial catalog

### Before hooks

Each key is a kit topic, a `request_attrs.kind` and a `request_attrs.action`.

| kit topic | `kind` | `action` | Host | Increment |
|---|---|---|---|---|
| `kit.runtime.entity.pre_persisted` | `embedding_model` | `promote` | CLI | 1 |
| `kit.runtime.entity.pre_persisted` | `embedding_model` | `deprecate` | CLI | 1 |
| `kit.runtime.entity.pre_persisted` | `embedding_model` | `purge` | CLI | 1 |
| `kit.runtime.entity.pre_persisted` | `object` | `delete` | CLI or daemon (service layer) | 1 |
| `kit.runtime.entity.pre_persisted` | `pipeline` | `archive`, `delete` | daemon (already emitted) | 1: documented only |
| `kit.runtime.entity.pre_persisted` | `object` | `ingest` | daemon (worker) | 2, once the latency budget is measured |

`pre_validated` stays available for rules that must run before validation. The catalog uses `pre_persisted` so that hooks see validated input.

### After events

All of these topics already exist or pass `ValidateTopic`.

| Topic | Host | Increment |
|---|---|---|
| `ctxt.upgrade.embedding_model.{promoted,deprecated,purged}` | CLI | 1 |
| `ctxt.runtime.object.deleted` (the declared constant, replacing the two-segment `object.deleted`) | CLI or daemon | 1 |
| `dpkms.upgrade.embeddings_migration.{completed,failed}` | daemon | 1 |
| `ctxt.runtime.hook.failed` | both | 1 |
| `ctxt.runtime.object.ingested`, `ctxt.runtime.job.{completed,failed}`, `dpkms.upgrade.reingest.{completed,failed}` | daemon | 2 |

### What cannot be hooked before, within kit today

These events are **after-only**:

- job completion and failure
- migration and reingest progress and completion
- ambient capture signals
- adapter lifecycle

None of them is an entity mutation or a state transition, and kit exposes no veto seam for anything else. This is accepted as a limit. It is not a kit request.

## First increment

It proves both hook kinds end to end with no kit change and no webhooks.

1. **Storage.** Add one migration per engine:
   - `event_outbox (seq, id, topic, source, occurred_at, origin, actor, instance_id, payload)`, indexed on `seq`
   - `instance_meta (instance_id)`
2. **Kit seams.**
   - A `domain.Service[object]` wrapper routes `Service.DeleteObject`.
   - A `domain.Service[embedding_model]` wrapper routes the registry's `SetDefault`, `Deprecate` and `Purge`, as updates with `request_attrs.action` and a delete.
   - The pre phases keep kit's default topics, and the post phases are rebranded with `WithTopics` to the ctxt topics above.
   - Both processes stamp `ContextAttrsKey` with `{note, request_attrs: {kind, id, action, dry_run}}`.
   - Both processes wire `policy.Wire` onto the bus these services publish to. That makes `cel` rules for the new kinds work with no further change.
3. **Outbox write.** The repositories behind these services append the post event inside the mutation's transaction. `service.go:454` stops publishing the two-segment `object.deleted`.
4. **The `internal/hooks` dispatcher.**
   - One synchronous subscriber on each kit pre topic. It filters on `request_attrs`, runs the `exec` hooks from local `hooks.yaml`, and applies `timeout` and `on_failure`, which is open by default.
   - Every fail-open emits `ctxt.runtime.hook.failed`.
   - The catalog is `contracts/hooks/events.yaml`.
5. **Delivery.**
   - The daemon relays outbox rows to `svc.Bus`.
   - `GET /api/v1/events?after=<seq>&limit=` serves pull.
   - SSE sends `id: <seq>`, resumes from `Last-Event-ID`, and unsubscribes when a client disconnects.
   - `ctxt events deliver` drains the outbox with no daemon; `ctxt events tail` shows it.
6. **All new topics** are built with `bus.TopicOf` so they are validated when the process starts.
7. **Out of scope for this increment:** webhooks, both pre and post; the database hook registry (`ctxt hooks …`); the `/ws/bus` mirror; retention and `lagging`; ingest before hooks; the naming-audit renames.

### Test gates

- **Blocking and warning.**
  - An `exec` hook exiting 2 on (`pre_persisted`, `embedding_model`, `purge`) blocks the purge. Nothing is deleted, no outbox row is written, and the error names the hook.
  - Exit 1 warns and lets the purge proceed.
- **CEL rules through kit's existing loader.** A rule `on: kit.runtime.entity.pre_persisted`, with `when` reading `context.request_attrs.kind == "object"`, blocks a delete. The policy file loads unchanged, which proves no kit change is needed.
- **Failure policy.** A timeout with `open` proceeds and emits `hook.failed`. With `closed`, it blocks.
- **Dry runs** show "would be blocked" and write no row.
- **Atomicity.** A rollback leaves no outbox row, checked by fault injection.
- **Replay.**
  - Start a daemon after CLI purges: pull returns every event in `seq` order.
  - Reconnect SSE with `Last-Event-ID`: no gap, and any duplicates share an `id`.
- **Targeting.** `--instance B` writes to B's outbox only.
- **Conformance.** Every catalog topic passes `bus.ValidateTopic`.
- **Mutation checks.** Removing the in-transaction emit, the `request_attrs` stamp or the block short-circuit makes the tests fail.
- **Postgres.** The same suite runs under `-tags=integration`.
- **Live servers.** Never contact them. Use temporary databases and an isolated `HOME` / `XDG_*`.

## Follow-up work: the naming audit

ADR-075 lists every deviation with its file and line. The work groups into six items:

1. `ctxt.ambient.source.ready` → `readied` (six publish sites and the builder in `internal/ambient/ambient.go:149`), following the ADR-065 adapter precedent.
2. Replace the two-segment `svc.Bus` event types (`object.raw_stored`, `job.enqueued`, `object.updated`, `object.deleted`, `inbox.captured`, `inbox.triaged`) with 4-segment topics built by `TopicOf`, and drop the duplicate `job.enqueued`.
3. Rename the plugin event types, which contain hyphens and use actions that are not past tense (`ctxt.plugin.<name>.<noun>`), and the capability events `ctxt.refresh.trigger` and `ctxt.notification.create`.
4. Make the catalog name the Object explicitly for `embedding_model` and `embeddings_migration`, whose first underscore reads as a modifier.
5. Re-enable topic validation in `cmd/ctxt/cmd/lateral.go:191`, which uses `ModeOff`.
6. Consider strict enforcement (`kit.bus.enforce=strict`) in tests, so that deviations fail CI.

Renames break subscribers, so each rename is its own change with a release-notes row.

## What `docs/architecture.md` should say afterwards

It replaces the "kit/bus integration (recent)" section:

> **Events and hooks (ADR-075).** ctxt and dpkms host hooks on kit's seams, at the service layer of whichever process makes the change: the CLI for direct writes, the daemon for API and worker work.
>
> *Before hooks* are synchronous subscribers on kit's veto topics (`kit.runtime.entity.pre_validated|pre_persisted`, `kit.runtime.state.pre_transitioned`), keyed by `request_attrs.kind` and `action`. They take three forms: kit CEL policy, which fails closed on evaluation errors; local `exec` handlers; and `webhook` handlers. The last two share nerv's decision JSON and have per-hook timeouts and `on_failure`. Hooks can allow, warn or block, never rewrite.
>
> *After events* are ctxt-owned kit-grammar topics appended to the instance's `event_outbox` in the change's transaction. dpkms delivers them at least once, in order per subscription: by pull, resumable SSE, webhooks, and a best-effort `/ws/bus` mirror for trusted peers.
>
> `BUS_TOKEN` authenticates only that peer mesh. Hooks bind to the instance database, never to a URL.

Until the first increment lands, the accurate statement is narrower: domain events reach only `svc.Bus` and SSE, and `/ws/bus` carries policy traffic and whatever peers publish.

## Decisions the owner must make

1. **The contract home.** Accept kit's seams for the hook points, with nerv's decision JSON for `exec` and `webhook` handlers, rather than extending nerv or axon.
2. **What a before hook can do.** Confirm veto-only (`allow`/`warn`/`block`) with no `rewrite`, which kit's pre phases cannot express anyway.
3. **The default failure policy for `exec` and `webhook` hooks.** Open everywhere (the nerv precedent), or closed by default for destructive keys (`purge`, `delete`)? `cel` keeps kit's closed-on-error behavior regardless.
4. **Where hooks are registered.** Allow the instance database registry for `webhook` hooks (recommended: it binds to every writer), or keep everything in local config? `exec` stays local-only, and `cel` stays in kit's policy file.
5. **`/ws/bus`.** Mirror post topics onto it for peers (recommended), and confirm that `BUS_TOKEN` is never issued to external apps.
6. **Retention.** The default window (7 days is proposed), and what happens to a `lagging` subscription.
7. **The outbox and ADR-074's `sync_log`.** Keep them as separate tables (recommended: announcements versus replication operations), or share one log?
