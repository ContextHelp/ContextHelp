# CLI-originated bus events: getting them to the instance

> **Date:** 2026-09-27
> **Status:** design note, awaiting a decision. Nothing here is implemented.
> **Related:** [ADR-007](../decisions/ADR-007-transactional-outbox-ingestion.md) (transactional outbox), [ADR-070](../decisions/ADR-070-pipeline-and-index-versioning.md), [ADR-071](../decisions/ADR-071-embedding-index-versioning.md) ("Default-flip control")
> **Audience:** whoever decides and then implements the delivery path

`ctxt embeddings set-default`, `deprecate` and `purge` publish `ctxt.upgrade.embedding_model.{promoted,deprecated,purged}` after their change commits. They publish on a bus the CLI builds for that one call (`bus.New()` in `cmd/ctxt/cmd/embeddings_lifecycle.go`). The only thing that bus reaches outside the process is kit's env sink (`KIT_BUS_SINK=jsonl` plus `KIT_BUS_SINK_PATH`), and that is a local file. No dpkms instance ever sees the events.

Correctness does not depend on them, because the query path reads the default model on every query. They record operator actions. This note chooses how they should reach the instance whose database the command changed.

## What exists today

| Piece | Where | Behaviour that matters here |
|---|---|---|
| How the CLI reaches the instance | `openEmbeddingsBackend` → `newService` → `resolveStoragePath` (`cmd/ctxt/cmd/helpers.go`) | The lifecycle commands write the **database directly**. `--instance` / `CTXT_INSTANCE` resolve to a DB path through the local pidfiles; without them, `storage.path` or a Postgres DSN from config is used. No HTTP call is made. |
| How HTTP clients reach an instance | `clientEndpoints` / `--server` (`analyze`, `capture`, `import …`) | Resolved from `server.urls`, then `server.url`, then `http://127.0.0.1:8080`. This path **ignores `--instance`**. |
| CLI-to-daemon precedent | `ctxt embeddings migrate` | Enqueues an `embeddings_migrate` task job in the instance's `jobs` table. The dpkms worker runs it and publishes `dpkms.upgrade.embeddings_migration.*`. This is ADR-007's outbox used as the channel. |
| Server domain bus | `events.NewLocalBus()` → `svc.Bus` (`cmd/dpkms/cmd/serve.go`) | Receives every domain event: `ctxt.runtime.*` from the worker pool, `dpkms.upgrade.*` from the upgrade and migrate runners. Exposed as SSE on `GET /api/v1/events`. |
| Cross-process hub | `kitbus.New()` + `NewNetworkAdapter` on `GET /ws/bus` | Requires `DPKMS_BUS_TOKEN` or `BUS_TOKEN`: one static shared secret, with no principal attached. The policy engine's sync (veto-capable) subscribers sit on it. Runs **without relay mode**, so an event a peer publishes is not forwarded to the other peers. **No domain event is bridged from `svc.Bus`**, even though `docs/architecture.md` ("kit/bus integration") says ingest events go there. |
| kit network adapter | `poly-kit` `go/runtime/bus/network.go` | Forwarding is asynchronous and fire-and-forget: there is no ack, and the wire event has no ID. The dial is subject to `netpolicy` offline mode. The auth handshake waits up to 10 s for its ack. |
| Topic grammar | kit `bus.Validate` | `[Source].[Category].[Object].[Action]`. The source is the system that originated the event. Here that is `ctxt`, the actor, whichever process delivers it. |

"The instance's hub" therefore means two surfaces today. `svc.Bus` (with its SSE stream) is where sibling upgrade events such as `dpkms.upgrade.embeddings_migration.*` already go. `/ws/bus` carries policy traffic and whatever peers publish, and nothing else.

## Options

### (a) The CLI calls an instance API endpoint that emits the event server-side

There are two variants. In the first, the mutation itself moves server-side, for example `POST /api/v1/embeddings/{id}/default`. In the second, the CLI writes the database and then `POST`s a notification.

- **For:** one server-side emission point, and it reuses the HTTP API's inbound auth (`server.auth`, `RequireAuth`).
- **Against:**
  - No embeddings endpoints exist. The first variant rewrites the lifecycle commands as HTTP clients and needs authorization for destructive admin operations on non-private instances.
  - The second variant is an event-injection endpoint whose claims the server cannot verify.
  - Both require a running server. Today the commands work against `storage.path` with no daemon at all.
  - The worst problem is targeting. The database comes from `--instance` or config, while the URL comes from `clientEndpoints`, so the two can name different instances. `ctxt --instance work embeddings purge X` would change `work`'s database and then announce the change to whatever listens on `:8080`.

### (b) The CLI connects to the instance's `/ws/bus` with `BUS_TOKEN`

- **For:** kit already ships the client (`bus.WithNetwork`, `AuthFromEnv`), and the change on the CLI side is small.
- **Against:**
  - It has the same database-versus-URL targeting split as (a).
  - Every CLI user would need to hold the hub's single shared secret. That secret has no principal and grants publish rights onto the bus where the policy engine's veto subscribers run.
  - Delivery is fire-and-forget with no ack and no event ID. A server that is down, or offline mode, silently loses the event.
  - Each call pays for a dial plus an auth round-trip.
  - Without relay mode, peers such as aps and tlc still never see the event.
  - The event misses `svc.Bus` and SSE, where the related `dpkms.upgrade.*` events already go.

### (c) The server derives the events from registry changes it observes

This means polling `embedding_models`, `PRAGMA data_version`, or Postgres `LISTEN/NOTIFY` fed by a trigger.

- **For:**
  - It catches every writer: CLI, `psql`, another server on a shared Postgres.
  - It needs no new credential and has no targeting problem.
- **Against:**
  - It sees state, not intent. The coverage threshold used (`min_coverage`) and a purge's row count are gone once the change commits.
  - Transitions made while the server was down collapse: A→B→A shows as no change.
  - Telling "rescheduled" apart from "newly deprecated" means diffing timestamps.
  - Doing it robustly means recording each change as it happens, which is option (d) built the hard way.

### (d) CLI records, server announces: an outbox row in the instance's database

The CLI enqueues a small task job, `bus_relay`, in the database it just changed. Its payload is the topic, the source, the payload and the time of the change. A dpkms worker handler publishes it on `svc.Bus`. This is the path `embeddings migrate` already uses.

- **For:**
  - **Targeting is correct by construction.** The event travels with the database it describes, local SQLite and remote Postgres alike.
  - **No new credential.** Being able to change the registry already implies being able to write `jobs`.
  - **Offline-safe.** With no daemon running, the row waits and is delivered on the next start, stamped with the original time.
  - **Durable and retried.**
  - **Intent is preserved.** The payload is the one the CLI builds today.
  - **Consistent with its siblings.** It lands next to `dpkms.upgrade.embeddings_migration.*` on the same bus and the same SSE stream.
- **Against:**
  - Delivery is late when no daemon runs, and never happens if none ever does. The rows are inert.
  - It adds one job row per operator action. That is fine at operator-action rates and wrong for high-rate events.
  - A daemon older than the handler fails the job with no handler (visible in `ctxt jobs`, harmless).
  - It is at-least-once, so it needs an event ID for dedupe (see below).

## Recommendation: (d)

(d) is the only option in which "which instance" is answered by the same resolution that chose the database. It survives a daemon that is down and remote Postgres, and it needs no new credential. It is also the pattern the codebase already uses for CLI-to-daemon work. That makes it a reuse of ADR-007 rather than a new architectural pattern, so this note is a design doc and not an ADR (see `docs/decisions/README.md`, "Small implementation details typically belong in design docs"). Bridging to `/ws/bus` is a different matter: it would change what the hub carries for every domain event, and that would need its own ADR (decision 2 below).

### How (d) handles the cross-cutting concerns

- **`--instance` targeting:** it is inherited. The row goes into whichever database `openEmbeddingsBackend` opened.
- **Remote instances:** Postgres-backed instances work unchanged. `FOR UPDATE SKIP LOCKED` claims each row once, so with several servers on one database, exactly one relays it (to its own hub).
- **Dry run:** nothing is enqueued. Enqueue under exactly the conditions that gate the publish today: set-default `Changed && !dryRun`, deprecate `changed && !dryRun`, purge `!dryRun`.
- **Offline (`--offline`):** there is no network I/O, so the command behaves the same.
- **Duplicate emission:**
  - The CLI's local publish stays, for `KIT_BUS_SINK` audit files only. It never reaches an instance, so it does not duplicate the hub copy.
  - The server copy comes only from the relay handler. The CLI must never also dial `/ws/bus`.
  - Worker retries can publish twice. Set the CloudEvent `id` to the job ID, so subscribers dedupe on `id`.
- **Atomicity:**
  - The registry methods (`SetDefault`, `Deprecate`, `Purge`) own their transactions.
  - The first cut enqueues after commit, which matches today's rule for the publish: a failure is reported and the change stands.
  - Moving the enqueue inside the registry transaction (an `emit(tx)` hook) closes the crash window. Do it only if that gap matters.
- **Topic and source:** unchanged. The event stays `ctxt.upgrade.embedding_model.*` with source `ctxt`, because ctxt is the actor and the daemon only delivers. The relay handler publishes only topics from an allowlist (`ctxt.upgrade.embedding_model.*`), so it cannot be used to inject arbitrary topics.

## Decisions needed from the owner

1. **Accept (d)?** If yes, a follow-up task implements the `bus_relay` task type and handler, the CLI enqueue, and tests: a relay after a daemon restart, dry run enqueuing nothing, and `id` stable across a retry.
2. **Should domain events reach `/ws/bus` at all?** Today none do: not these, not `ctxt.runtime.*`, not `dpkms.upgrade.*`. Either:
   - **(i)** keep `svc.Bus`/SSE as the domain surface and correct `docs/architecture.md`; or
   - **(ii)** add a filtered one-way bridge from `svc.Bus` to the hub for `ctxt.*` and `dpkms.*`, with the event ID carried in the payload since kit's `Event` has none, and turn on relay mode if peers such as aps and tlc should see the events.

   Choice (ii) is cross-cutting and warrants an ADR.
3. **Rows no daemon will ever read** (pure CLI use with no `dpkms serve` on that database): keep them (the default), or skip the enqueue when no instance serves the database?
