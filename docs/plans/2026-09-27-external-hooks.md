# External hooks on ctxt and dpkms: survey, first increment, later increments

> **Date:** 2026-09-27
> **Decision record:** [ADR-075 – External Hook Surface](../decisions/ADR-075-external-hook-surface.md) (Accepted 2026-09-26). It holds the owner's seven decisions, kit's extension points (K1–K13, cited by file and line) and the full naming audit.
> **Status:** accepted design. Nothing here is implemented yet.
> **Audience:** whoever implements the first increment and the later ones

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
| **A. Before hooks on kit's veto topics plus `request_attrs`; after events on conformant ctxt topics through an outbox** | **Chosen** | Needs no kit change, `cel` works today, and binding to the database is correct everywhere. |
| A′. ctxt-owned `pre_*` topics (the previous revision) | Withdrawn | Not veto-able under kit policy, so the owner's rule excludes it. |
| B. nerv as the dispatcher | Rejected | Out of scope, and runs per machine. |
| C. Everything over `/ws/bus` | Rejected | No veto path, a shared secret, no durability, and targeting by URL. |
| D. The daemon as the only host | Deferred | Breaks running with no daemon. |
| E. Server-side change detection | Rejected | Loses intent. |
| F. One `jobs` row per event | Superseded by A | No ordering and no replay. |

## Hookable events: the initial catalog

### Before hooks

Each key is a kit topic, a `request_attrs.kind` and a `request_attrs.action`.

| kit topic | `kind` | `action` | Host | Failure class | Increment |
|---|---|---|---|---|---|
| `kit.runtime.entity.pre_persisted` | `embedding_model` | `promote` | CLI | open | 1 |
| `kit.runtime.entity.pre_persisted` | `embedding_model` | `deprecate` | CLI | open | 1 |
| `kit.runtime.entity.pre_persisted` | `embedding_model` | `purge` | CLI | closed | 1 |
| `kit.runtime.entity.pre_persisted` | `object` | `delete` | CLI or daemon (service layer) | closed | 1 |
| `kit.runtime.entity.pre_persisted` | `pipeline` | `archive`, `delete` | daemon (already emitted) | `archive` open, `delete` closed | 1: documented only |
| `kit.runtime.entity.pre_persisted` | `object` | `ingest` | daemon (worker) | open | later, once the latency budget is measured |

`pre_validated` stays available for rules that must run before validation. The catalog uses `pre_persisted` so that hooks see validated input.

The failure class applies to `exec` and `webhook` hooks (ADR-075 decision 3): a failing hook blocks a `closed` action and is skipped on an `open` one. `cel` rules always deny on evaluation error.

### After events

All of these topics already exist or pass `ValidateTopic`.

| Topic | Host | Increment |
|---|---|---|
| `ctxt.embeddings.model.{promoted,deprecated,purged}` | CLI | 1 |
| `ctxt.runtime.object.deleted` (the declared constant, replacing the two-segment `object.deleted`) | CLI or daemon | 1 |
| `dpkms.embeddings.migration.{completed,failed}` | daemon | 1 |
| `ctxt.runtime.hook.failed` | both | 1 |
| `ctxt.runtime.hook.gap_detected` | both | 1 |
| `ctxt.runtime.object.ingested`, `ctxt.runtime.job.{completed,failed}`, `dpkms.upgrade.reingest.{completed,failed}` | daemon | later |

### What cannot be hooked before, within kit today

These events are **after-only**:

- job completion and failure
- migration and reingest progress and completion
- ambient capture signals
- adapter lifecycle

None of them is an entity mutation or a state transition, and kit exposes no veto seam for anything else. This is accepted as a limit. It is not a kit request.

## First increment

It proves before hooks (`exec`) and after events end to end, with no kit change and no webhooks.

1. **Storage.** Add one migration per engine:
   - `event_outbox (seq, id, topic, source, occurred_at, origin, actor, instance_id, payload)`, indexed on `seq`
   - `instance_meta (instance_id)`
2. **Kit seams.**
   - A `domain.Service[object]` wrapper routes `Service.DeleteObject`.
   - A `domain.Service[embedding_model]` wrapper routes the registry's `SetDefault`, `Deprecate` and `Purge`, as updates with `request_attrs.action` and a delete.
   - The pre phases keep kit's default topics, and the post phases are rebranded with `WithTopics` to the ctxt topics above.
   - Both processes stamp `ContextAttrsKey` with `{note, request_attrs: {kind, id, action, dry_run}}`.
3. **Outbox write.** The repositories behind these services append the post event inside the mutation's transaction. `service.go:454` stops publishing the two-segment `object.deleted`.
4. **The `internal/hooks` dispatcher.**
   - One synchronous subscriber on each kit pre topic. It filters on `request_attrs` and runs the `exec` hooks from local `hooks.yaml`, using nerv's decision JSON and exit codes (exit 3 and 4+ are errors; the exit code wins over the JSON).
   - Each hook has a `timeout` (5 s default, 30 s max). A failure blocks `purge` and `delete` (closed) and is skipped on every other action (open).
   - Every failure emits `ctxt.runtime.hook.failed`, in its own transaction.
   - The catalog is `contracts/hooks/events.yaml`.
5. **Delivery.**
   - The daemon relays outbox rows to `svc.Bus`.
   - `GET /api/v1/events?after=<seq>&limit=` serves pull.
   - SSE sends `id: <seq>`, resumes from `Last-Event-ID`, and unsubscribes when a client disconnects.
   - `ctxt events deliver` drains the outbox with no daemon; `ctxt events tail` shows it.
6. **Retention.**
   - A prune pass (the daemon on a timer, and `ctxt events deliver`) deletes rows older than 7 days.
   - A pull or SSE request for a `seq` below the retention floor gets `410 Gone` naming the oldest retained `seq`, and `ctxt.runtime.hook.gap_detected` is emitted. The client resyncs from current state, then resumes from the head.
7. **All new topics** are built with `bus.TopicOf` so they are validated when the process starts.
8. **`docs/architecture.md`.** Replace the "kit/bus integration (recent)" section with the wording below, limited to what this increment ships.
9. **Out of scope for this increment:** webhooks, both pre and post, and the admin-only database registry (`ctxt hooks …`); `cel` rules on the new keys; the `/ws/bus` mirror; ingest before hooks; the naming-audit renames. See [Later increments](#later-increments).

### Test gates

- **Blocking and warning.**
  - An `exec` hook exiting 2 on (`pre_persisted`, `embedding_model`, `purge`) blocks the purge. Nothing is deleted, no outbox row is written, and the error names the hook.
  - Exit 1 warns and lets the purge proceed.
- **Failure policy.** A hook that times out on `purge` blocks it; one that times out on `promote` is skipped and the promote proceeds. Both emit `hook.failed`. Exit 3, exit 4+ and unparseable JSON count as failures.
- **Dry runs** show "would be blocked" and write no row.
- **Atomicity.** A rollback leaves no outbox row, checked by fault injection.
- **Replay.**
  - Start a daemon after CLI purges: pull returns every event in `seq` order.
  - Reconnect SSE with `Last-Event-ID`: no gap, and any duplicates share an `id`.
- **Targeting.** `--instance B` writes to B's outbox only.
- **Retention.** Rows older than 7 days are pruned. A pull below the floor gets `410 Gone` and emits `gap_detected`; it never returns a silent gap.
- **Conformance.** Every catalog topic passes `bus.ValidateTopic`.
- **Mutation checks.** Removing the in-transaction emit, the `request_attrs` stamp, the block short-circuit or the failure-class lookup makes the tests fail.
- **Postgres.** The same suite runs under `-tags=integration`.
- **Live servers.** Never contact them. Use temporary databases and an isolated `HOME` / `XDG_*`.

## Later increments

- **Webhooks** (before and after).
  - `webhook` handlers in the dispatcher: HTTPS except on loopback, same decision JSON, same timeout and failure class.
  - Webhook push for after events, using kit's `runtime/notify` webhook sink behind a per-subscription outbox cursor. Requests signed with a per-subscription HMAC-SHA256 over timestamp plus body. A subscription that falls behind retention is marked `stale` and stops until reset.
  - The instance database registry, `ctxt hooks add|list|rm`, holding `webhook` hooks only. Adding or removing one goes through the daemon's authenticated API and requires the `admin` role; it is also gated on `pre_persisted`.
- **`cel` rules on the new keys.** Wire `policy.Wire` in the CLI process as well, onto the bus the new services publish to. Test gate: a rule `on: kit.runtime.entity.pre_persisted`, with `when` reading `context.request_attrs.kind == "object"`, blocks a delete in both processes, and the policy file loads unchanged, which proves no kit change is needed.
- **`/ws/bus` mirror.** A live, best-effort copy of after events for trusted peers. `BUS_TOKEN` stays peer-only.
- **Ingest before hooks**, once their latency budget is measured.

## Follow-up work: the naming audit

ADR-075 lists every deviation with its file and line. The work groups into five items:

1. `ctxt.ambient.source.ready` → `readied` (six publish sites and the builder in `internal/ambient/ambient.go:149`), following the ADR-065 adapter precedent.
2. Replace the two-segment `svc.Bus` event types (`object.raw_stored`, `job.enqueued`, `object.updated`, `object.deleted`, `inbox.captured`, `inbox.triaged`) with 4-segment topics built by `TopicOf`, and drop the duplicate `job.enqueued`.
3. Rename the plugin event types, which contain hyphens and use actions that are not past tense (`ctxt.plugin.<name>.<noun>`), and the capability events `ctxt.refresh.trigger` and `ctxt.notification.create`.
4. Re-enable topic validation in `cmd/ctxt/cmd/lateral.go:191`, which uses `ModeOff`.
5. Consider strict enforcement (`kit.bus.enforce=strict`) in tests, so that deviations fail CI.

Renames break subscribers, so each rename is its own change with a release-notes row.

## What `docs/architecture.md` should say afterwards

It replaces the "kit/bus integration (recent)" section:

> **Events and hooks (ADR-075).** ctxt and dpkms host hooks on kit's seams, at the service layer of whichever process makes the change: the CLI for direct writes, the daemon for API and worker work.
>
> *Before hooks* are synchronous subscribers on kit's veto topics (`kit.runtime.entity.pre_validated|pre_persisted`, `kit.runtime.state.pre_transitioned`), keyed by `request_attrs.kind` and `action`. They take three forms: kit CEL policy, which fails closed on evaluation errors; local `exec` handlers; and `webhook` handlers. The last two share nerv's decision JSON and have per-hook timeouts; a failing one blocks destructive actions (`purge`, `delete`) and is skipped on all others. Hooks can allow, warn or block, never rewrite.
>
> *After events* are ctxt-owned kit-grammar topics appended to the instance's `event_outbox` in the change's transaction and kept for 7 days. dpkms delivers them at least once, in order per subscription: by pull, resumable SSE, webhooks, and a best-effort `/ws/bus` mirror for trusted peers. A subscriber that falls behind retention is marked stale and must resync.
>
> `BUS_TOKEN` authenticates only that peer mesh. Hooks bind to the instance database, never to a URL.

Today the accurate statement is narrower: domain events reach only `svc.Bus` and SSE, and `/ws/bus` carries policy traffic and whatever peers publish. The first increment writes the parts it ships (the `exec` hooks, the outbox, pull and SSE); later increments extend the section.

## Resolved decisions

The owner accepted ADR-075 on 2026-09-26. Its Decision section records these in full.

1. **Contract home.** kit's existing hook points as they are, with nerv's decision format for `exec` and `webhook` handlers (exit 0/1/2; 4 and above is an error). No kit change, ever.
2. **Veto only.** Before hooks allow, warn or block; they never rewrite the payload.
3. **Failure policy.** `exec` and `webhook` hooks fail closed on destructive actions (`purge`, `delete`) and open otherwise. `cel` always denies on evaluation error.
4. **Registration.** `exec` hooks only in local config. `webhook` hooks may be registered in the instance database, which requires admin authentication.
5. **`/ws/bus`.** Mirror after events onto it, best effort. `BUS_TOKEN` is never issued to external apps.
6. **Retention.** 7 days. A subscriber that falls behind is marked stale and must resync.
7. **Outbox and `sync_log`.** Separate tables.
