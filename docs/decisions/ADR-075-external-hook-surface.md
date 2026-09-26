# ADR-075 – External Hook Surface: Pre and Post Hooks on ctxt and dpkms Events

> **Status:** Proposed
> **Date:** 2026-09-27
> **Author:** jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **References:** ADR-007 (transactional outbox), ADR-023 (authentication), ADR-065 (adapters; its entity-topic amendment already routes policy-gated mutations through `domain.Service[T]`), ADR-068 (MCP read surface, `/ws/bus`), ADR-070 and ADR-071 (upgrade and embedding-model events), ADR-074 (sync log)
> **Plan:** [`docs/plans/2026-09-27-external-hooks.md`](../plans/2026-09-27-external-hooks.md) covers the survey, the naming audit, the first increment and the open decisions.
>
> kit paths below are relative to `poly-kit/go/` at `next` 1059039. ctxt paths are relative to the repository root.

---

## Context

The goal: **other applications can hook in before or after something happens in ctxt or in dpkms.** That covers actions an operator starts from the CLI (`ctxt embeddings purge`, `ctxt delete`, `ctxt capture`) and things that happen inside the daemon (ingest, job completion, migrations).

The owner's rule for this design: **kit is the authority on event naming and on the hook and policy machinery. ctxt conforms. No kit change is requested.**

### How ctxt reaches its data today

- **Two kinds of writer.**
  - Most mutating `ctxt` commands write the instance database directly. `newService` opens whatever `--instance` or config names, and `ctxt delete` calls `service.DeleteObject` itself.
  - The HTTP commands (`capture`, `analyze`, `import …`) pick their URL from `server.urls`. That choice ignores `--instance`, so an announcement sent by URL can land on a different instance from the one whose database changed.
- **Domain events stay on ctxt's in-process `events.LocalBus` (`svc.Bus`).** `GET /api/v1/events` streams that bus as SSE. The stream carries no event IDs and cannot be resumed.
- **`/ws/bus` carries no domain events.** It is a kit `NetworkAdapter` behind one shared `BUS_TOKEN` with no principal attached. Relay mode is off, and nothing is bridged onto it.
- **The veto seams already in use are kit's own.**
  - `domain.Service[Pipeline]` feeds the policy engine on the hub bus.
  - `internal/lateral/promote/handler.go:30` and `internal/lateral/reject/handler.go:12` evaluate kit's `pre_persisted` topic.

### kit's extension points, as they exist today

| # | Extension point | Where in kit |
|---|---|---|
| K1 | **Topic grammar.** A topic is `[Source].[Category].[Object].[Action]`: exactly 4 segments, each `^[a-z][a-z0-9_]*$`, no wildcards, at most 128 characters. | `runtime/bus/validate.go:65-102` and `:132-158`; `runtime/bus/event.go:8-19` |
| K2 | **Past-tense Action.** The Action segment must end in `ed` or appear in `pastTenseWhitelist`. The whitelist is the documented way to extend it. | `runtime/bus/topics.go:14-49`, `:51-99`, `:169-177` |
| K3 | **What `Publish` checks.** `Publish` calls `Validate`, which checks shape only, and the default mode is warn, not strict. The past-tense rule is enforced only by `ValidateTopic`, at construction time (`TopicOf`, `PrefixTopics`, the domain `WithTopicPrefix`). The contract names `ValidateTopic` as the authority. | `runtime/bus/bus.go:131`, `:191`, `:258`; `runtime/bus/config.go:31-58`; `docs/contracts/event-topics.md:4`, `:18-24` |
| K4 | **Object modifier.** The first underscore in the Object segment separates the object from a modifier. | `runtime/bus/builder.go:118-131`; `runtime/bus/doc.go:18` |
| K5 | **No registry of sources or categories.** Sources and categories appear only as examples. Actions are constrained only by K2. | `docs/contracts/event-topics.md:26-31` |
| K6 | **Before and after naming.** "Before" is a `pre_` prefix on a past-tense Action: `pre_validated`, `pre_persisted`, `pre_transitioned`. "After" is either a bare past tense (`created`, `updated`, `deleted`) or `post_transitioned`. Pre phases are synchronous and can veto. Post phases are best effort, and their errors are swallowed. | `runtime/domain/service_topics.go:9-48`; `runtime/domain/statemachine_topics.go:9-27`; `runtime/domain/service.go:174-231`, `:248-253`; `runtime/domain/statemachine.go:84-130`; `docs/adopters/reference/domain-events.md:47-66` |
| K7 | **The veto-able topics are fixed.** The policy engine evaluates exactly `kit.runtime.state.pre_transitioned`, `kit.runtime.entity.pre_validated` and `kit.runtime.entity.pre_persisted`. A rule whose `on:` names any other topic is rejected when the policy file loads. There is no public API or configuration to add topics. | `runtime/policy/config.go:31-41`, `:91-93`; `runtime/policy/subscriber.go:12-35` |
| K8 | **How a host plugs into K7.** It emits on those kit-owned topic families, either through `domain.Service[T]` / `domain.StateMachine` or by calling the exported `Engine.Decide(topic, activation)`. It describes the request through `policy.ContextAttrsKey` (`note`, `request_attrs`). The `resource` and `entity` bindings read `kind` and `op` from the payload. | `runtime/policy/policy.go:246-259`; `runtime/policy/config.go:11-24`; `runtime/policy/subscriber.go:58-121`, `:144-203` |
| K9 | **Rebranding topics.** `WithTopicPrefix` / `WithSMTopicPrefix` rebrand every phase, pre phases included, and a rebranded pre topic falls out of K7. `WithTopics` / `WithSMTopics` override single phases, so post phases can be rebranded while pre phases keep their kit defaults. kit recommends overriding the prefix when "the topic shape is part of a public contract you ship". | `runtime/domain/service_topics.go:55-108`; `runtime/domain/statemachine_topics.go:40-80`; `docs/contracts/event-topics.md:66-83` |
| K10 | **Payload gaps.** A delete's pre payload carries no entity, and a transition's pre payload carries only `From`, `To` and `Force`. The kind and identity have to arrive through K8's `request_attrs`. | `runtime/domain/service.go:174-184`; `runtime/domain/statemachine.go:78-82`, `:116` |
| K11 | **Policy evaluation errors deny.** The engine fails closed, and composition is deny-overrides. | `runtime/policy/policy.go:246-300` |
| K12 | **No veto over the network.** The network adapter forwards asynchronously (`SubscribeAsync("#")`) and discards the result of re-publishing an inbound event. A remote peer cannot veto anything. | `runtime/bus/network.go:127`, `:404-460` |
| K13 | **`ai/ext/hook` is not a topic convention to follow.** It is an in-process, priority-ordered hook bus for kit extensions. Its hook names (`before_init`, `after_run`, …) are not past tense, and kit mirrors them onto the bus with validation skipped. | `ai/ext/hook/hook.go:20-36`, `:208-226`; `docs/contracts/event-topics.md:133-146` |

Also relevant, outside kit:

- **nerv, axon and xat** cover hooks for AI-assistant host CLIs. axon's catalog states that "hop-top's own event vocabularies do NOT belong" (`poly-axon` `spec/events.yaml:6-11`). ctxt appears there only as a consumer of nerv's `MemoryRead` / `MemoryWrite`.
- **nerv's handler contract** is prior art for external handlers: a JSON envelope on stdin, a decision on stdout, and exit codes 0/1/2 with fail-open on error.

---

## Decision

**ctxt and dpkms become hook hosts on kit's existing seams.**

- **Before hooks** are synchronous subscribers on the three kit-owned veto topics (K7). ctxt emits those topics for its own mutations and describes each request through `policy.ContextAttrsKey` (K8).
- **After events** use ctxt-owned topics that pass `ValidateTopic`. They are written to a transactional outbox in the instance database and delivered at least once.
- **ctxt mints no `pre_*` topics of its own.**

### 1. Naming

- **Before hooks never get their own topic.** A before hook is keyed by a kit topic plus a request descriptor:
  - Topics: `kit.runtime.entity.pre_validated`, `kit.runtime.entity.pre_persisted` (entity create, update and delete) and `kit.runtime.state.pre_transitioned`.
  - Descriptor: ctxt stamps `request_attrs = {kind, id, action, dry_run}` on every pre phase it raises (K8, K10). Hooks and CEL rules select on `context.request_attrs.kind` and `context.request_attrs.action`. ctxt already follows this precedent: `internal/policy/policies_default.yaml` gates a pipeline archive on `context.request_attrs.action == "archive"`.
  - `kind` ∈ `object`, `embedding_model`, `pipeline`.
  - `action` is the operator verb: `delete`, `purge`, `promote`, `deprecate`, `archive`.
- **After events are ctxt-owned topics** that pass `ValidateTopic` (K1–K2). They keep every topic that already passes: `ctxt.runtime.object.deleted`, `ctxt.embeddings.model.{promoted,deprecated,purged}`, `dpkms.embeddings.migration.{completed,failed}`, and so on.
  - When a mutation goes through `domain.Service[T]`, its post phases are rebranded to these topics with `WithTopics` (K9), and the pre phases stay on kit's defaults so policy still sees them.
  - Hook-system events are `ctxt.runtime.hook.failed` and `ctxt.runtime.hook.gap_detected` (a subscriber fell behind retention).
- **Every new topic is built through `bus.TopicOf` or `PrefixTopics`**, so `ValidateTopic` checks it when the process starts, as `internal/lateral/events/catalog.go` already does. Constants built by string concatenation are not added.
- **Catalog.** A spec file, `contracts/hooks/events.yaml`, lists what can be hooked:
  - each before-hook key (kit topic, `kind`, `action`), with which process hosts it and its default failure policy
  - each after topic, with its payload schema and which fields are redacted
- **Existing topics that violate kit's rules** are listed as follow-up work under Consequences. This ADR renames nothing.

### 2. Hook hosts

- **The host is `internal/service`**, the layer both processes share. The CLI builds it in `newService`, and the daemon builds it in `dpkms serve` for HTTP, gRPC, MCP and the worker pool.
- **Mutations that hooks cover go through kit's seams:**
  - **Object delete** goes through `domain.Service[object]`.
  - **The embedding-model lifecycle** is modeled as entity mutations. Promote and deprecate are updates with `request_attrs.action`, and purge is a delete. This mirrors how a pipeline archive is plumbed through an update. The mutations go through a `domain.Service[embedding_model]` wrapper around the registry.
  - **Wiring.** Both processes wire the same `policy.Wire` and ctxt's dispatcher onto the bus that these services publish to.
- **Which process runs the hooks for an action:**
  - A CLI action that writes directly runs its hooks in the CLI process.
  - An action that goes over HTTP runs them in the daemon.
  - Daemon-internal work runs them in the daemon.
- **Nothing is announced by URL**, so `--instance`, remote Postgres and running with no daemon all resolve to the right database.

### 3. Before hooks: veto only, and only in process

- **Decisions.** A hook returns `allow`, `warn` or `block`.
  - A `block` becomes the veto error that kit's pre phase already returns (K6), surfaced as a conflict error that names the hook.
  - **No `rewrite`.** kit's pre phases can veto but have no channel for returning a modified payload. Rewriting would also break the destructive-command confirmation token and the audit trail.
- **Handler kinds:**
  - **`cel`** is kit policy, used exactly as kit ships it: rules in the existing policy file, with `on:` set to one of the three kit topics. Evaluation errors deny (K11).
  - **`exec`** is a local command. It receives the envelope on stdin, writes the decision JSON on stdout, and exits 0 (allow), 1 (warn), 2 (block), or 4 and above for an error. These are nerv's codes, minus `rewrite`.
  - **`webhook`** is an HTTPS POST whose response body is the same decision JSON.
- **ctxt's dispatcher** runs the `exec` and `webhook` hooks. It is one synchronous subscriber on each kit pre topic, and it filters on `request_attrs`.
- **Order.** Policy runs first and short-circuits, then the dispatcher's `exec` hooks, then its `webhook` hooks. Decisions combine deny-overrides, as K11 does.
- **Timeouts and failure** (for `exec` and `webhook` only):
  - Each hook has a `timeout`: 5 s by default, 30 s at most.
  - `on_failure: open|closed` defaults to open, as nerv does. Every fail-open is recorded as `ctxt.runtime.hook.failed`.
  - `cel` keeps kit's closed behavior (K11).
- **Dry runs.** The pre phases run with `request_attrs.dry_run = true`. A block is reported as "would be blocked", and no outbox row is written.
- **Pre phases never leave the process.** A bus peer cannot veto (K12), so `/ws/bus` is not a before-hook transport.

### 4. After hooks: transactional outbox, delivered at least once

- **The outbox table.** `event_outbox` has these columns: `seq`, `id` (a UUIDv7), `topic`, `source`, `occurred_at`, `origin`, `actor`, `instance_id` and `payload`.
- **Writing.** The repository behind the kit service writes the row inside the mutation's transaction. kit's own post publish is best effort and runs after the write (K6), so it cannot give this guarantee. This is ADR-007's outbox pattern applied to announcements. The table is kept separate from ADR-074's `sync_log`.
- **Delivery.** The daemon's dispatcher reads rows in `seq` order and delivers each one to every subscription, at least once. Consumers dedupe on `id`.
- **Ordering.** Order holds per subscription. A failing subscription holds up only itself, retrying with backoff.
- **While an app is down,** its cursor stays where it was until retention runs out. The proposed retention is 7 days. After that the subscription is marked `lagging`, and `ctxt.runtime.hook.gap_detected` is emitted.
- **With no daemon running,** rows wait. `ctxt events deliver` drains them without one.
- **Transports:**
  - pull: `GET /api/v1/events?after=<seq>`
  - SSE on `GET /api/v1/events`, resumable through `Last-Event-ID`
  - webhook push, using kit's `runtime/notify` webhook sink behind the outbox cursor
  - a live, best-effort mirror of post topics onto `/ws/bus` for trusted peers
- **Local audit copies.** The CLI's local `KIT_BUS_SINK` publish stays as a local audit copy.

### 5. Registration, authentication and trust

- **Instance registry.** A database table, managed with `ctxt hooks add|list|rm`, which are policy-gated through the same `pre_persisted` seam. It holds `webhook` hooks. `cel` rules stay in kit's policy file.
- **Local config.** `$XDG_CONFIG_HOME/contexthelp/hooks.yaml`, plus a project layer, holds `exec` and `webhook` hooks.
- **`exec` hooks never come from the database.**
- **Webhooks** must use HTTPS except on loopback. Requests are signed with a per-subscription HMAC-SHA256 over the timestamp plus the body, and receivers enforce a replay window.
- **Pull and SSE consumers** authenticate through the existing `/api/v1` `RequireAuth` (ADR-023).
- **`BUS_TOKEN`** stays a peer credential and is never issued to external apps.
- **Payloads carry IDs and metadata only.**

### 6. Instance identity

The database stores an `instance_id`, and every event carries it.

---

## Rationale

- **Conformance over invention.** kit's three veto topics plus `request_attrs` can already describe every before hook the goal needs. ctxt does exactly this for pipeline archive and delete, and for lateral promote and reject. Minting `ctxt.*.pre_*` topics would pass the grammar, but kit policy would reject them (K7), so they could never be gated. That is kit adapting to ctxt, which the owner ruled out.
- **Binding to the database, not a URL,** is the only way to be correct across `--instance`, remote Postgres and running with no daemon.
- **kit, not nerv,** is the contract for hop-top's own events. axon excludes them by rule. nerv's decision JSON is still reused, so existing handlers carry over.
- **Only the outbox can reach an app that is down.** A bus cannot, because kit's post phases are best effort (K6).

### Alternatives considered

- **ctxt-owned `pre_*` topics**, the previous version of this ADR: withdrawn. They pass K1–K2 but are not veto-able under K7, and making them veto-able would require a kit change.
- **Rebranding every phase with `WithTopicPrefix`**: rejected. It moves the pre phases off K7.
- **kit's `ai/ext/hook` bus**: rejected. It is in process, meant for extension lifecycles, and its hook names are not conformant topics (K13).
- **nerv as the dispatcher**: rejected. It is out of nerv's and axon's scope, runs per machine, and has no host on the daemon side.
- **Everything over `/ws/bus`**: rejected. There is no veto path (K12), one shared secret with no principal, no durability, and announcements would still be targeted by URL.
- **The daemon as the only host**: deferred. It breaks running with no daemon.
- **Server-side change detection**: rejected. It loses intent.
- **One `jobs` row per event**: superseded by the outbox.
- **Rewrite-capable before hooks**: rejected. kit's pre phases have no channel for it.

---

## Consequences

### Positive

- There is no kit dependency to wait on. Every before hook lands on a seam kit already evaluates, so `cel` rules work today.
- One contract covers both processes and both kinds of origin. After-event delivery survives an app being down, the daemon being down, and remote instances.
- SSE becomes resumable, and `/ws/bus` carries domain events.

### Negative

- **Before-hook keys are less self-describing.** An external author must match on the kit topic plus `request_attrs.kind` and `request_attrs.action`, not on a single topic name. The catalog documents every valid key.
- **Hookable mutations have to go through kit's domain seams.** The embedding-model registry and object delete gain `domain.Service` wrappers.
- **Performance and connectivity.** Every mutation pays an outbox insert and its before-hook latency. A webhook before hook needs network access.
- **Two registry sources** to reason about.

### What cannot be expressed within kit today

These limits are accepted, with no kit change requested.

1. **A named before hook per ctxt action.** It cannot exist (K7). Before hooks are always one of three kit topics plus `request_attrs`.
2. **Before hooks for events that are not mutations** cannot be vetoed. Examples are job completion, migration progress and ambient capture signals, none of which is an entity mutation or a state transition. They are after-only.
3. **Before hooks on ingest**, which would fire `pre_persisted` on the ingest path, are feasible, but they sit on the hot path. They are deferred until their latency budget is measured.
4. **A remote veto over `/ws/bus`** is impossible (K12). Remote before hooks are webhooks called by the host.
5. **Rewriting a payload** is impossible (K6 has no channel for it).

### Naming audit: follow-up work

Every topic below was checked against K1–K4. This ADR fixes none of them.

**Topics that fail `ValidateTopic` (K2), or fail the grammar outright (K1):**

- **`ctxt.ambient.source.ready`.** "ready" is not past tense. It is published at `internal/ambient/filewatch/filewatch.go:149`, `internal/ambient/screenshot/screenshot.go:170`, `internal/ambient/foreground/foreground.go:157`, `internal/ambient/meeting/meeting.go:208`, `internal/ambient/browserhistory/browserhistory.go:148` and `internal/ambient/clipboard/clipboard.go:143`, and built by the string concatenation in `internal/ambient/ambient.go:149-151`. The ADR-065 adapter lifecycle already renamed the same word to `readied` (`internal/adapter/events.go:21-27`).
- **Two-segment event types on `svc.Bus`.** These bypass kit validation entirely, because `events.LocalBus` never validates:
  - `object.raw_stored` at `internal/service/service.go:205`
  - `job.enqueued` at `internal/service/service.go:294` and `:722`. It also duplicates `ctxt.runtime.job.enqueued`, which `internal/jobs/worker.go:440` publishes.
  - `object.updated` at `internal/service/service.go:440` and `object.deleted` at `:454`. The conformant constants `ctxt.runtime.object.{updated,deleted}` are declared at `internal/events/topics.go:13-14` but never used.
  - `inbox.captured` at `internal/service/service_inbox.go:54` and `inbox.triaged` at `:120`.
- **Plugin event types.** Their segments contain hyphens (invalid characters under K1), and some actions are not past tense:
  - `ctxt.plugin.content-monitor.{seen,change,error}` at `plugins/content-monitor/plugin.go:194`, `:226` and `:239`
  - `ctxt.plugin.dir-watcher.{file,error}` at `plugins/dir-watcher/plugin.go:251` and `:264`
  - `ctxt.plugin.rss-feed.{error,item}` at `plugins/rss-feed/plugin.go:105` and `:268`
- **Plugin capability events.** `ctxt.refresh.trigger` (`internal/plugin/capability.go:88`) and `ctxt.notification.create` (`:98`) have three segments and imperative actions.

**Validation is switched off:** `cmd/ctxt/cmd/lateral.go:191` builds its bus with `bus.WithEnforce(bus.ModeOff)`.

**Already conformant:**

- the other `internal/events/topics.go` topics
- `internal/lateral/events/catalog.go`, which is built with `TopicOf`
- `ctxt.ingest.object.{captured,persisted}`
- the `ctxt.ambient.meeting.*`, `ctxt.ambient.{event,enqueue,session}.*` and `ctxt.ambient.source.{started,stopped,failed}` topics
- `dpkms.adapter.lifecycle.*` and `dpkms.<protocol>.entity.*`
- the `kit.runtime.entity.*` topics from `domain.Service[Pipeline]`
- the proposed `ctxt.runtime.hook.{failed,gap_detected}`

**Not bus topics:** the `security.*` event kinds in `internal/security/events.go:28-32` are stored records. They would need 4-segment names if they ever become hookable.

---

## Implementation Notes

The plan holds the first increment, its test gates and the proposed wording for `docs/architecture.md`.

---

## References

- kit: `runtime/bus` (K1–K5, K12), `runtime/domain` (K6, K9, K10), `runtime/policy` (K7, K8, K11), `ai/ext/hook` (K13), `runtime/notify`, `docs/contracts/event-topics.md`, `docs/adopters/reference/domain-events.md`
- nerv: `spec/handler-contract.md`. axon: `spec/events.yaml` (the scope rule).
- ctxt: `internal/adapter/events.go:32-55` (the ADR-065 precedent for routing policy-gated mutations through `domain.Service[T]`), `internal/policy/policies_default.yaml`, `internal/lateral/promote/handler.go:30`
- ADR-007, ADR-023, ADR-065, ADR-068, ADR-070, ADR-071, ADR-074
