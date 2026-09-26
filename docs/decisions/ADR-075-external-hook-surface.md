# ADR-075 – External Hook Surface: Pre and Post Hooks on ctxt and dpkms Events

> **Status:** Proposed
> **Date:** 2026-09-27
> **Author:** jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **References:** ADR-007 (transactional outbox), ADR-023 (authentication), ADR-068 (MCP read surface, `/ws/bus`), ADR-070 and ADR-071 (upgrade and embedding-model events), ADR-074 (sync log)
> **Plan:** [`docs/plans/2026-09-27-external-hooks.md`](../plans/2026-09-27-external-hooks.md) covers the survey, first increment and open decisions.

---

## Context

The goal: **other applications can hook in before or after something happens in ctxt or in dpkms.** That includes actions an operator starts from the CLI (`ctxt embeddings purge`, `ctxt delete`, `ctxt capture`) and things that happen inside the daemon (ingest, job completion, migrations).

Today there is no such surface.

- **Two kinds of writer.**
  - Most mutating `ctxt` commands write the instance database directly. `newService` opens the database that `--instance` or config names, and `ctxt delete` calls `service.DeleteObject` itself.
  - The HTTP commands (`capture`, `analyze`, `import …`) instead choose a URL from `server.urls`. That choice ignores `--instance`, so a notification sent by URL can reach a different instance from the database that changed.
- **Domain events go only to an in-process bus.**
  - The daemon publishes them on `svc.Bus` (an `events.LocalBus`), which `GET /api/v1/events` streams as SSE. The stream has no event IDs and no resume: a client that disconnects loses what it missed.
  - CLI processes publish only to their own short-lived bus. Its only outlet is a local `KIT_BUS_SINK` file.
  - Two declared topics, `ctxt.runtime.object.{updated,deleted}`, are never published at all.
- **`/ws/bus` is not usable as a hook surface.**
  - It is a kit `NetworkAdapter` guarded by one shared `BUS_TOKEN`, with no principal behind it. Relay is off.
  - Domain events are never bridged onto it.
  - Forwarding is fire-and-forget. `readLoop` discards the error that `Publish` returns for an inbound event, so a remote peer can never veto anything.
  - The only veto-capable subscribers are the in-process kit policy (CEL) engine's.
- **Prior art.**
  - **kit.** `go/runtime/domain` and `go/runtime/policy` define `pre_*` topics (`kit.runtime.entity.pre_persisted`, `kit.runtime.state.pre_transitioned`). They are synchronous and veto-able, compose as deny-overrides, and have after-event counterparts that are best effort. kit policy is wired to exactly those three kit topics.
  - **nerv.** nerv defines a handler contract: an envelope on stdin, and on stdout one of `allow | warn | block | rewrite`, with exit codes 0/1/2/3. Errors and timeouts fail open, and timeouts are per hook. But nerv, axon and xat are scoped to AI-assistant host CLIs. axon's catalog states that "hop-top's own event vocabularies do NOT belong". ctxt appears in nerv only as a *consumer* of nerv events (`MemoryRead`, `MemoryWrite`).

The design has to work whether the CLI talks to a local SQLite instance or a remote Postgres one, whether a daemon is running or not, and under `--instance`. It must not reintroduce the problem of resolving the database and the URL separately.

---

## Decision

**ctxt and dpkms become hook hosts through a kit-native contract.**

- Hookable events use kit's topic grammar, with `pre_` actions for before hooks.
- Before hooks run synchronously at the service layer, in whichever process performs the mutation. They can veto, but they cannot modify the payload.
- After events are appended to a transactional outbox in the instance database. The daemon delivers them at least once, in order, to pull, SSE, webhook and `/ws/bus` consumers.
- The external handler decision JSON is a subset of nerv's handler contract, so a nerv handler script can serve as a ctxt hook.

### 1. Naming and catalog

- **After** events keep kit's bare past-tense action: `ctxt.upgrade.embedding_model.purged`. Every existing topic stays as it is.
- **Before** events put `pre_` in the Action segment: `ctxt.upgrade.embedding_model.pre_purged`, `ctxt.runtime.object.pre_deleted`. This is kit's own convention (`pre_persisted`, `pre_transitioned`) and keeps four segments. A `.before`/`.after` suffix would add a fifth segment and fail `bus.Validate`.
- **Source** is the system whose action it is. `ctxt.*` covers operator and knowledge-object actions, whether the CLI or the daemon hosts them. `dpkms.*` covers substrate work such as migrations and reingest.
- **Catalog.** A spec file, `contracts/hooks/events.yaml`, is the only list of hookable events. Each entry gives:
  - the topic and whether it is pre or post
  - the host or hosts that raise it
  - the payload schema, and which fields are redacted
  - whether it can be vetoed, and its default failure policy

  An event that is not in the catalog cannot be hooked.

### 2. Hook hosts

- **The host is the service layer.** `internal/service` is the choke point that both processes share. The CLI builds it in `newService`, and the daemon builds it in `dpkms serve` for HTTP, gRPC, MCP and the worker pool.
- A `hooks.Host` built into both runs the before hooks and writes the outbox row, inside the same call as the mutation.
- The embeddings lifecycle commands move behind the same host. Today they reach the registry directly.
- **Which process hosts an action:**
  - A CLI action that writes directly is hosted by the CLI process.
  - A CLI action that goes over HTTP (`capture`) is hosted by the daemon.
  - Daemon-internal work (ingest, jobs, migrations) is hosted by the daemon.
- Nothing is announced by URL. The event lands in the database that changed, so `--instance`, remote Postgres and running with no daemon are all correct by construction.

### 3. Before hooks: veto only, and only in process

- **Decisions.** A handler returns `allow`, `warn` (proceed, and show the message) or `block` (refuse, and show the message).
  - **No `rewrite`.** A modified payload would break the confirmation token that kit derives from destructive command arguments. It would also detach the audit record from what the operator typed, and let a remote party retarget a destructive operation.
  - **Observe-only is not a before-hook kind.** Observers subscribe to the after event.
- **Handler kinds:**
  - `cel`: a kit policy rule, evaluated in process.
  - `exec`: a local command. It receives the envelope on stdin, writes the decision JSON on stdout, and exits 0 (allow), 1 (warn), 2 (block), or 4 and above for an error. These are nerv's codes, minus `rewrite`.
  - `webhook`: an HTTPS POST whose response body is the same decision JSON.
- **Composition.**
  - Evaluation order is `cel`, then `exec`, then `webhook`, so a cheap block short-circuits the external calls.
  - Decisions combine deny-overrides, as kit policy does: the first `block` wins and `warn` messages accumulate.
  - A block surfaces to the operator as a conflict error that names the hook.
- **Timeouts and failure.**
  - Each hook sets `timeout`: the default is 5 s and the maximum is 30 s. nerv allows up to 300 s, but these hooks sit on operator and API paths.
  - A timeout, error, unreachable endpoint or `--offline` counts as a hook failure. What happens next is the hook's `on_failure` policy:
    - `open` logs the failure and proceeds. This is the default, matching nerv.
    - `closed` blocks.
  - The catalog may set a stricter default per event.
  - Every failure is recorded as a post event, `ctxt.runtime.hook.failed`, so a fail-open is never silent.
- **Dry runs.**
  - Before hooks run with `dry_run: true`. A block is reported as "would be blocked by <hook>".
  - Handlers must have no side effects on a dry run, as kit's `sideeffect` convention requires.
  - No after event is written.
- **Pre topics never leave the process.** They are not written to the outbox and not forwarded to `/ws/bus`, because no transport there carries a reply.

### 4. After hooks: transactional outbox, delivered at least once

- **The outbox table.** Each instance database gets an `event_outbox` table with these columns:
  - `seq`, monotonic per instance
  - `id`, a UUIDv7
  - `topic`, `source`, `occurred_at`
  - `origin`: `ctxt` or `dpkms`
  - `actor`: the principal or OS user
  - `instance_id`
  - `payload`
- **Writing.** The hosting process appends the row **in the same transaction** as the mutation. This is ADR-007's outbox pattern applied to announcements. It is kept separate from ADR-074's `sync_log`, which holds replication operations rather than notifications.
- **Delivery.**
  - The daemon's dispatcher reads the outbox in `seq` order and delivers each event to every subscription.
  - The guarantee is **at least once**. Consumers dedupe on `id`.
  - Order is kept **per subscription**, in `seq` order. A failing subscription blocks only itself, with head-of-line backoff, and every other subscription keeps flowing.
- **While an app is down**, its cursor stays where it was, and the app catches up when it returns.
  - This holds within the retention period. The default is 7 days, and it can be configured.
  - Past retention the subscription is marked `lagging`, and a `ctxt.runtime.hook.gap_detected` event records the hole.
  - With no daemon running, rows wait. `ctxt events deliver` drains them without one.
- **Transports**, all fed from the outbox:
  - **Pull.** `GET /api/v1/events?after=<seq>` returns a page of events, and the consumer keeps its own cursor.
  - **SSE.** The existing `GET /api/v1/events` stream gains `id: <seq>` and resumes from `Last-Event-ID`.
  - **Webhook push.** A registered subscription, with a server-held cursor.
  - **`/ws/bus` mirror.** Post topics `ctxt.*` and `dpkms.*` are forwarded to connected peers, live and best effort, with no replay.
- **Local audit copies.** The CLI's local `KIT_BUS_SINK` publish stays as a local audit copy. It never reaches an instance, so it does not duplicate the delivered event.

### 5. Registration, authentication and trust

- **Two sources of hooks.** Both layer the way nerv's configs do: all layers apply, and a lower layer never shadows a higher one.
  - **Instance registry.** A database table, managed with `ctxt hooks add|list|rm`. It holds `cel` and `webhook` hooks, and they bind to every writer of that database. `ctxt hooks add` and `rm` are marked destructive-shared and are policy-gated.
  - **Local config.** `$XDG_CONFIG_HOME/contexthelp/hooks.yaml`, plus a project layer. It holds `exec`, `cel` and `webhook` hooks, which apply to processes of that user on that machine.
- **`exec` hooks never come from the database.** Anyone who can write the database would otherwise run code on every CLI user's machine.
- **Webhooks:**
  - HTTPS is required, except on loopback.
  - Each subscription has its own secret, and requests are signed with HMAC-SHA256 over the timestamp plus the body.
  - Receivers reject requests outside a replay window.
  - The instance's network policy applies.
- **Pull and SSE consumers** authenticate through the existing `/api/v1` `RequireAuth` (ADR-023). Scoping topics per principal is a later step.
- **`BUS_TOKEN`** remains a credential for a trusted mesh of peers. It is never issued to external apps, and `/ws/bus` is not a hook API.
- **Payloads carry IDs and metadata, never object bodies.** The catalog marks what is redacted.

### 6. Instance identity

Each database stores an `instance_id`, and every event carries it. That lets one consumer subscribe to several instances. It also lets a CLI check that a daemon it reaches over HTTP owns the database it resolved: the first step toward closing the split between how the database and the URL are chosen.

---

## Rationale

- **Correct targeting.** Hooks and events bind to the database, never to a URL. That is the only way to be correct across `--instance`, remote Postgres and running with no daemon, because the database is what every writer shares.
- **kit, not nerv, is the contract for hop-top's own events.** axon and nerv exclude hop-top vocabularies by rule, and nervd is per machine: a remote dpkms cannot reach a user's nervd. Borrowing nerv's decision JSON and exit codes still lets people reuse the handlers they already have.
- **Veto-only before hooks** give the power the goal needs ("run before, and stop it") without the audit and security costs of rewriting a payload. kit policy already works this way.
- **The outbox is the reliability mechanism.** A bus, whether in process or `/ws/bus`, cannot deliver to an app that is down. A cursor over an ordered table can, and every transport (pull, SSE, webhook, bus mirror) becomes a view of it.

### Alternatives considered

- **nerv as the dispatcher**, with ctxt and dpkms emitting nerv envelopes to nervd: rejected. It is outside nerv's and axon's scope, it is per machine, and it has no daemon-side host.
- **Everything over `/ws/bus`**, extending kit's adapter with a request/reply channel: rejected.
  - It needs one shared secret and has no principal, so any holder can publish into the policy bus.
  - Nothing is durable.
  - Notifications sent by URL can reach the wrong instance.
  - It would still need a kit protocol change.
- **The daemon as the only host**, with every CLI write routed through the API: rejected for now. It breaks running with no daemon and reintroduces targeting by URL. Revisit it if direct database writes from the CLI are ever retired.
- **Server-side change detection**: rejected. It loses intent (the thresholds and row counts), and it collapses transitions that happen while the daemon is down.
- **One `jobs` row per event**: rejected. This was the earlier version of this design. It gives no ordering and no replay, and it covers only CLI-originated events.
- **Rewrite-capable before hooks**: rejected, as §3 explains.

---

## Consequences

### Positive

- One contract for both processes and for both CLI-originated and daemon-originated events.
- After-hook delivery that survives an app being down, the daemon being down, and remote instances.
- SSE becomes resumable.
- `/ws/bus` finally carries domain events, which is what `docs/architecture.md` already claims.
- Existing nerv handler scripts work as `exec` hooks.

### Negative

- Every hookable mutation pays an outbox insert, and CLI operations pay the latency of their before hooks, bounded by the timeouts.
- A CLI with a `webhook` before hook needs network access, and without it the hook's failure policy applies.
- There are two registry sources to reason about.
- A database writer can register a webhook that receives event metadata. This is mitigated by policy-gating `ctxt hooks`, by redacting payloads, and by the fact that database access is already full trust.

### Neutral or considerations

- **kit policy** watches exactly three kit topics and rejects unknown ones. To use `cel` hooks on `ctxt.*.pre_*`, kit has to accept an adopter's topic set. That is a kit change, tracked in kit.
- **Hot paths.** Ingest and `capture` before hooks are deferred until their latency budget is measured.

---

## Implementation Notes

The first increment, the migrations, the test gates and the proposed wording for `docs/architecture.md` are in the plan. Two defects the survey found are handled separately:

- the SSE handler never unsubscribes from `LocalBus`
- `ctxt.runtime.object.{updated,deleted}` are declared but never published

---

## References

- kit: `go/runtime/bus` (grammar, network adapter), `go/runtime/policy` (CEL veto on `pre_*`), `go/runtime/domain` (pre and post phases), `go/runtime/notify` (webhook sink, retry, dead-letter)
- nerv: `spec/handler-contract.md` (decision JSON, exit codes, merge rules, timeouts); axon: `spec/events.yaml` (the host-CLI scope rule)
- ADR-007, ADR-023, ADR-068, ADR-070, ADR-071, ADR-074
