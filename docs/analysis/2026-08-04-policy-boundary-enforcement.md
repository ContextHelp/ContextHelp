# Policy Boundary Enforcement — Findings and Design

> **Date:** 2026-08-04
> **Applies to:** dPKMS, ctxt
> **Companion:** [2026-08-04-storage-federation-addendum.md](2026-08-04-storage-federation-addendum.md), [2026-08-04-single-writer-enforcement.md](2026-08-04-single-writer-enforcement.md)

## Summary

The federation addendum's P0 claim was that entitlement, metering, and capability checks "live at the daemon HTTP boundary," so any direct-storage path bypasses access control by construction. Tracing the code shows the situation is different in shape and worse in substance:

1. **The `internal/registry` policy machinery is *outbound*, not inbound.** Entitlement, metering, and capability checks govern this instance's access to *remote* registries. They are not the daemon's access-control layer for its own content — and two of the three are not wired into any live code path at all.
2. **The daemon's inbound boundary has no access control today.** The HTTP middleware stack is request-ID, panic recovery, and CORS — nothing else. gRPC has no auth interceptor and ships with reflection enabled. The single authenticated endpoint in the entire surface is federation push, and its token is optional.
3. **The direct-storage bypass is therefore real but currently bypasses *nothing inbound*** — because there is nothing inbound to bypass. The one policy mechanism that does run in the daemon (the CEL policy engine on the event bus) is constructed only by `dpkms serve`; the CLI's direct path builds the same `service.Service` without it, so even that seam is boundary-asymmetric today.

The consequence for federation: "daemon-only mode for public/protected instances" is necessary but not sufficient. Enforcing that all access flows through the daemon is step two; step one is giving the daemon an inbound policy boundary worth routing through. Both are specified below, with the interaction against the single-writer flock design — including why the read-only bypass recommended there must be conditional on the instance's access class.

## 1. Where policy checks actually execute today

### 1.1 `internal/registry` — outbound consumer-side policy, partially dormant

| Check | What it governs | Where invoked | Status |
|---|---|---|---|
| `EntitlementChecker.FetchAndStore` | This instance's right to sync a *remote* registry | `internal/registry/sync.go:147` (pre-sync, when `cfg.EntitlementURL` set) | Live, outbound only |
| `EntitlementChecker.CheckNamespace` (`entitlement.go:111-142`) | Per-namespace grants against a stored entitlement | **No callers outside tests** | Dormant |
| `Meter.RecordEvent` / `ErrQuotaExhausted` (`metering.go:58-106`) | Quota enforcement on registry access events | **No callers outside tests** (`NewMeter` never constructed in `cmd/` or `internal/` non-test code) | Dormant |
| `CheckCapabilities` (`capability.go:23-75`) | Feature handshake with a remote registry | `internal/service/service.go:1976` | Live, advisory (warnings only) |

All four are policies this instance applies *as a client of someone else's registry*. None of them gate what a caller of *this* instance may read or write. The addendum's framing should be corrected: these components are the seed of a policy vocabulary (entitlements, quotas, capabilities) that a public instance will need to enforce *inbound*, but today they enforce it only outbound — and the two enforcement-shaped pieces (`CheckNamespace`, `Meter`) are unwired, the same pattern as `idxbridge` in the single-writer findings.

### 1.2 The daemon's inbound HTTP boundary — no authentication, no authorization

- Middleware stack is exactly three entries: `RequestID`, `Recoverer`, `CORS` (`internal/server/http/server.go:34-36`, `middleware.go:9-31`). No token check, no identity, no per-route policy.
- Every `/api/v1` route — objects, search, analyze, pipelines, capture, audit log, inbox — is registered bare (`server.go:65-190`).
- The **only** authenticated endpoint is `POST /api/v1/federation/push` (`server.go:181`), and its Bearer check applies only "if server has federation.token configured" (`handlers_federation.go:13-25`, `internal/config/config.go:156-158`). Empty token = open write endpoint.
- The MCP read-surface is mounted at `/api/v1/mcp` inside the same router (`server.go:188-189`, ADR-068) and inherits the same absence. There is no in-process/stdio MCP command in either binary — MCP is daemon-only, which is the right topology; it just shares the unprotected boundary.
- `policy.go`'s `withPolicyContext` (`internal/server/http/policy.go:22-28`) plumbs request *attributes* (the `X-Ctxt-Note` header) into the CEL engine's context. It carries no caller identity and performs no check itself.

### 1.3 gRPC — same absence, plus reflection

`internal/server/grpc/server.go:27-47`: the only interceptor is panic recovery (`:29`), and server reflection is registered unconditionally (`:44`). The gRPC listener binds the same address as HTTP (`cmd/dpkms/cmd/serve.go:347`), so `dpkms serve --public` exposes an unauthenticated, fully-reflectable gRPC API (analyze, jobs, query, entities) to the network.

### 1.4 Bind selection — the `--public` switch

`serve.go:324-327`: bind is `127.0.0.1`, flipped to `0.0.0.0` by the `--public` flag / `server.public` config (`serve.go:93,104,131`; `internal/config/config.go:434-439` — `ServerConfig.Public`). There is:

- no warning when `--public` is set without any auth configured;
- no validation coupling `server.public` to `federation.token` or any future auth setting (`internal/config/validate.go` and `lint.go` never mention `public`);
- a latent mismatch: `findFreePort` probes availability on `127.0.0.1` (`serve.go:614-616`) even when the server will bind `0.0.0.0`.

The cookie bridge is the one component correctly pinned to loopback regardless of `--public` (`serve.go:355`).

### 1.5 The CEL policy engine — daemon-only by construction, absent from the direct path

The one live *inbound-ish* policy mechanism is `kit/runtime/policy` wired over the event bus: `dpkms serve` constructs it before the service (`serve.go:267-271`, `internal/policy/policy.go:79-100`) and injects its publisher via `service.NewWithOptions(..., WithPolicyPublisher(...))` (`serve.go:273-274`). Misconfig fails loud — "the daemon never serves traffic against an unenforced ruleset" (`serve.go:261-263`).

The CLI's direct path constructs the *same* `service.Service` with **no** policy publisher and no bus: `service.New(driver, queue, pipes, engine, "", nil, *cfg)` (`cmd/ctxt/cmd/helpers.go:88`; identically `cmd/dpkms/cmd/helpers.go:59`). `service.New` passes `nil` options (`internal/service/service.go:79-81`). So the pre-persist policy veto seam exists when the daemon touches an object and silently does not exist when the CLI touches the same row in the same file. This is the concern in miniature, already observable in a single-user install: policy enforcement is a property of the *entry point*, not of the data.

### 1.6 Scaffolding that assumes a boundary that doesn't exist yet

`internal/security/events.go` defines `RecordAuthFailure` (`:172-187`) and `RecordACLDenial` (`:189-196`) with per-principal sliding-window alerting, and `internal/config/config.go:209-217` exposes their thresholds. Neither is called from anywhere outside the package — alerting for an authn/authz layer that has not been built. Same for the promises in `docs/ctxt/user-stories.md`: story 12 (`:21`, public vs localhost bindings — the exact activation condition for this concern), story 58 (`:79`, deny-by-default agent tag access), stories 60/61/63 (`:83-86`, REST/gRPC access with per-agent context windows). No per-agent identity exists in any handler, so 58 and 63 currently have no substrate to attach to.

## 2. Direct-storage entry points (the bypass inventory)

Every path below opens the database file without passing any daemon boundary — and per §1, also without the CEL policy engine:

| Entry point | Evidence | Surface |
|---|---|---|
| `ctxt` CLI, all commands | `newService()` at `cmd/ctxt/cmd/helpers.go:46-97`; 29 files call it (find/show/list/export/edit/delete/link/ingest/compose/registry/…) | Read + write + DDL (`driver.Init` runs migrations, `helpers.go:68`) |
| `dpkms` CLI utility commands | Parallel `newService()` at `cmd/dpkms/cmd/helpers.go:26-60` (jobs, detector, …) | Read + write + DDL |
| `dpkms housekeeping` | `sqlite.New` at `cmd/dpkms/cmd/housekeeping.go:31`; `VACUUM` + `wal_checkpoint(TRUNCATE)` | Exclusive maintenance |
| **Federation `LocalPusher`** | `internal/federation/local.go:63-72` — opens a *target instance's* DB via `storageutil.NewDriver`, runs `Init` (migrations!) on it, and upserts objects, edges, entities, and the target's watermark row directly | Cross-instance write |
| Snapshot/backup path | `internal/storageutil/snapshot.go:14` (`sql.Open` on the source path) | Read (full-content export) |

Notes on scope:

- **Plugins and extensions are clean.** No `plugins/` or `extensions/` code opens storage; the Raycast extension and browser capture flow talk to the HTTP API. The plugin execution model runs inside the daemon process and reaches storage through `service.Service` — inside the (future) boundary, not around it.
- **`LocalPusher` is the sharpest federation-frame violation.** Remote federation correctly goes through the receiving daemon (`internal/federation/remote.go:101` → `POST /api/v1/federation/push`) where the target's token check applies. Local federation writes into the peer's file directly — bypassing the peer's federation token, its (future) entitlement checks, its policy engine, and its single-writer lock. A "local target" is still *another instance* with its own access class; the asymmetry between `remote.go` and `local.go` is exactly the class of hole this document exists to close.
- The single-writer findings are confirmed as stated: `idxbridge` has no importers outside `internal/idxbridge/`, `newService` opens directly and migrates unconditionally, and no read-only (`mode=ro`) open exists anywhere in the CLI.

## 3. Defining "access != private"

There is currently exactly one bit of access posture in the system: `server.public` (`config.go:437`). That is not enough to express the federation model (public / access-controlled / private instances), and it describes the *daemon's bind*, not the *instance's data*. The access class must be a property of the instance — durable, visible to processes other than a running daemon — because the enforcement question ("may this process open the file directly?") must be answerable when the daemon is **down**. Otherwise `dpkms shutdown` becomes the universal policy bypass.

**Proposed config:**

```yaml
server:
  access: private | protected | public   # default: private
```

- `private` — loopback-only, single-operator. Direct CLI access permitted (subject to the single-writer flock).
- `protected` — network-reachable with authentication required (team/business instance, access-controlled federation source).
- `public` — network-reachable, anonymous read surface possible, entitlement-gated namespaces.

**Derivation and validation rules** (in `internal/config/validate.go`, failing loud at load — same philosophy as `policy.Init`):

1. `server.public: true` (and the `--public` flag) requires `access != private`. `--public` alone becomes shorthand for `access: public` with a deprecation note; the contradiction `access: private` + `--public` is a hard config error.
2. `access != private` requires inbound auth to be configured (see §5). `dpkms serve` must **refuse to start** a protected/public instance with no credentials configured — the current silent `0.0.0.0` bind with zero auth (§1.4) is the failure mode this rule deletes.
3. A non-empty `federation.token` on the receive side is a hint, not a class: it does not flip the class by itself, but linting should flag `access: private` + configured inbound federation token as probably-wrong.

**Durable marker.** On startup (and on config change), a daemon whose instance is `protected` or `public` writes an access marker the CLI can read without opening the database:

- Location: `<dbpath>.access` sidecar, next to the `<dbpath>.lock` sidecar from the single-writer design. JSON body: `{access, instance, daemon_addr, updated_at}`.
- Written atomically (temp + rename), before `driver.Init` in `serve.go` (the same pre-init slot as the flock acquisition at `serve.go:144-148`).
- **Never removed on shutdown.** The marker outlives the daemon precisely so that a stopped daemon does not reopen the direct path. It is downgraded to `private` only by an explicit operator action (`dpkms instance set-access private`), which is a local admin command and may require the daemon stopped.
- The pidfile (`serve.go:401-411`, `internal/pidfile/pidfile.go:31-39`) additionally gains an `Access` field for discovery/UX (`dpkms ps` showing the class), but the pidfile is *not* the enforcement source — it vanishes with the process and its write is already tolerated-as-nonfatal (`serve.go:412-414`).

Choosing a sidecar over an in-database row is deliberate: reading a marker row would itself require opening the database — the act being gated. The sidecar breaks the circularity; the flock sidecar precedent already establishes the pattern and the location.

## 4. Enforcement design: mandatory daemon-only mode

### 4.1 The gate

Extend the single-writer choke point. `newService()` (`cmd/ctxt/cmd/helpers.go:46`) — and its `dpkms` twin — gains, *before* `storageutil.NewDriver`:

1. Read `<dbpath>.access`. Absent marker ⇒ `private` (backward compatible; every existing install behaves as today).
2. `private` ⇒ proceed to the flock protocol from the single-writer design unchanged (non-blocking acquire; read-only `mode=ro` bypass for read commands).
3. `protected` or `public` ⇒ **refuse direct open unconditionally — reads included.** No flock probe, no `mode=ro` carve-out, no migration ladder. The only permitted path is the daemon API.

The same check goes into `LocalPusher.Push` (`internal/federation/local.go:63`): before opening a local target, read the *target's* access marker; a non-private target must be pushed to via its daemon endpoint (i.e., local targets of class ≠ private are configured with a URL, not a path — config validation can enforce this shape), or the push fails with the same error class. `dpkms housekeeping` and the snapshot path get the identical gate with an explicit, audited override (§4.4).

### 4.2 Why the read-only bypass must die for protected instances

The single-writer design correctly argues that WAL readers are safe alongside one writer, so `mode=ro` reads should never be blocked *by the lock*. That reasoning is about **integrity**. For a protected/public instance the binding constraint is **confidentiality and authorization**: story 58's deny-by-default agent tags, story 63's per-agent context windows, and inbound entitlements all mean that *what a caller may read depends on who the caller is*. A `mode=ro` open answers "is this read safe?" and never asks "is this read allowed?". A read bypass on a protected instance is therefore a policy bypass with full-corpus scope — strictly worse than the write bypass, because it exfiltrates silently and leaves no job rows behind. Hence the rule in §4.1: the ro carve-out is a *private-class privilege*, keyed off the access marker, not a global property of read commands. The two designs compose cleanly because both decisions sit in the same pre-open gate and read the same pair of sidecars.

### 4.3 Failure UX

Same principles as the lock-held error (name the blocker, state the resolution, distinct exit code, never silently degrade):

```
ctxt: instance "acme-main" is access-controlled (protected) — direct database access is disabled
  → use the daemon API: ctxt --instance acme-main … (daemon at 127.0.0.1:8420, running)
  → daemon not running? start it: dpkms serve
  → operator override for maintenance: dpkms instance set-access private (requires daemon stopped)
```

Distinct exit code from the lock-contention code, so scripts can distinguish "busy, retry" from "forbidden by posture, do not retry." When the daemon is up (pidfile present + alive), commands that have a daemon-API equivalent should eventually auto-route (§6); until routing exists, the error is the contract.

### 4.4 Honesty about the enforcement tier

An access-marker check in *our* CLI is cooperative enforcement: it stops accidental bypass (operators, scripts, well-behaved tools — the overwhelmingly dominant risk today), not a hostile local process with filesystem read access to the `.db` file. For genuinely public/protected deployments the design must state the OS tier explicitly:

- Run the daemon as a dedicated user; `chmod 0600` the database, WAL, blob directory, and sidecars. Direct access then fails at `open(2)` for every other uid — the kernel enforces daemon-only mode, and the CLI's marker check becomes a *better error message* in front of `EACCES` rather than the boundary itself.
- `dpkms doctor` (or serve-time preflight) should verify this: a `protected`/`public` instance whose DB file is readable by other users gets a loud warning, and `--strict` deployments a refusal.
- The maintenance override (`housekeeping`, snapshot/backup) is an operator action performed as the daemon's uid; it is gated by the flock (daemon stopped or lock yielded), logged to the audit log, and does not require downgrading the access class.

This tiering keeps the claim precise: the sidecar gate makes daemon-only mode *mandatory within the toolchain*; file ownership makes it *mandatory on the host*.

## 5. The boundary must be worth routing through

Forcing all access through the daemon is empty while §1 holds — a protected instance whose HTTP surface is anonymous has moved the bypass from `open(2)` to `curl`. Closing the direct path and hardening the inbound boundary are one deliverable in the federation frame:

1. **Authentication middleware** on the HTTP router for `access != private`: bearer/API-key auth as the floor, positioned before the route table (`server.go:34-36`). The gRPC server needs the mirror-image unary/stream auth interceptor, and reflection (`grpc/server.go:44`) must be disabled outside `private`.
2. **Identity → policy plumbing.** The authenticated principal goes into the request context alongside `withPolicyContext`'s attributes (`policy.go:22-28`), giving the CEL engine and the service layer a caller identity. This is the substrate stories 58 (deny-by-default tags) and 63 (per-agent context windows) require, and what makes `internal/security`'s dormant `RecordAuthFailure`/`RecordACLDenial` (`events.go:172-196`) finally callable.
3. **Inbound reuse of the registry vocabulary.** When this instance *serves* federation (public registry, entitlement-gated namespaces), the dormant `CheckNamespace` matcher and `Meter` quota logic are the right shapes to wire behind the boundary — per-principal instead of per-remote-registry. That they already persist to the same database (`storage.EntitlementStore`, `storage.MeteringStore`) is the addendum's "commercial distribution on the same substrate" point, made real.
4. **Federation token becomes mandatory-by-class**: `access != private` ⇒ `federation.token` (or successor credential) required, closing the optional-token hole at `handlers_federation.go:17-19`.
5. Fix `findFreePort` to probe on the actual bind address (`serve.go:614-616` vs `:324-327`) while in the neighborhood.

Story 43 (concurrency-safe storage for multiple agents, `user-stories.md:60`) is satisfied *through this boundary*: many agents hit one daemon over REST/gRPC/MCP; the daemon is the single writer; SQLite never sees cross-process write concurrency. The story's promise does not require — and on protected instances must not permit — concurrent direct DB access.

## 6. Sequencing relative to the single-writer work

The two designs share a choke point and should land as one ladder:

1. **Flock gate** (single-writer step 1) — integrity backstop, ships independently, benefits every install today.
2. **Access class + marker + CLI refusal gate** (§3, §4.1-4.3) — small delta on top: one config field, one sidecar, one branch ahead of the flock probe, the `LocalPusher`/housekeeping call sites. Lands *before* any instance is operated as protected/public; there is nothing to migrate because no such instances can exist correctly yet.
3. **Inbound authn + identity plumbing** (§5.1-5.2) + serve-time refusal of `access != private` without credentials. From this point `--public` is safe to actually use.
4. **Read/write split in `newService`** (single-writer step 2) — the `mode=ro` bypass ships already conditional on `access == private` per §4.2, so no later retrofit.
5. **`idxbridge` wiring extended to the write surface** (single-writer step 3) — the refusal in §4.3 becomes transparent routing: on a protected instance the CLI is simply a daemon client, carrying credentials like any other. Deny-by-default agent tags (story 58) and per-agent windows (story 63) attach to the identity layer from step 3.
6. **Inbound entitlement/metering enforcement** (§5.3) — activates the dormant `internal/registry` machinery behind the boundary for public registry serving.

Steps 1-2 are the mandatory floor the addendum called "non-negotiable," and they are deliberately tiny: the expensive parts (auth, routing, per-agent policy) can follow at their own pace because the refusal gate makes the interim state safe — a protected instance without full routing is *inconvenient* (some CLI commands error with instructions), never *open*.
