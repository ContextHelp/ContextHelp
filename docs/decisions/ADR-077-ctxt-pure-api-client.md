# ADR-077 – ctxt Is a Pure dpkms API Client; Instances Resolve to Endpoints

> **Status:** Proposed
> **Date:** 2026-09-27
> **Author:** jadb
> **Applies to:** ctxt CLI (every command), dPKMS HTTP and gRPC surface, client endpoint configuration
> **Supersedes:** None
> **Amends:** ADR-075 §2 "Hook hosts" (the CLI stops being a host; see Consequences)
> **References:** ADR-023 (authentication and authorization), ADR-056 (unified enqueue API), ADR-066 (ambient capture substrate), ADR-070 (pipeline and index versioning), ADR-071 (embedding index versioning), ADR-074 (same-owner device sync), ADR-075 (external hook surface)
> **Plan:** [`docs/plans/2026-09-27-ctxt-remote-client.md`](../plans/2026-09-27-ctxt-remote-client.md) holds the per-command audit, the endpoint map, the missing endpoints and the phased tasks.

---

## Context

dpkms often runs on another machine: an always-on host reached over a private network, with `server.access: protected` and static bearer tokens (ADR-066 already treats this as the canonical topology). ctxt on the operator's laptop has to work fully against it. Today ctxt reaches its data three different ways:

- **Over HTTP.** `status`, `log`, `upgrade status`, `feed *`, `import *` and `capture [source]` resolve one endpoint (`serverEndpoint`, `cmd/ctxt/cmd/server_endpoint.go:33`; `captureEndpoints`, `cmd/ctxt/cmd/capture.go:289`) and send requests with the configured token. These work remotely.
- **HTTP with a local fallback.** `analyze` (and bare `ctxt <content>`), `capture tabs` and `capture history` try the configured instances, then write into a local database through `localDirectAnalyze` (`cmd/ctxt/cmd/write_gate.go:26`, wired at `analyze.go:230-233`, `capture_tabs.go:322-325`, `capture_history.go:526-529`). The fallback also runs after a 401 or 403 (`internal/idxbridge/analyze.go:78-87`). The user sees "Queued locally; the job will run when the daemon starts" (`analyze.go:264`, `:270`), but when dpkms is remote no daemon ever reads that database. `list --q` searches remotely but opens the local store first (`list.go:137`) and falls back to the local corpus after an auth rejection (`internal/idxbridge/idxbridge.go:309-316`).
- **Straight into the store.** About sixty leaf commands open the storage driver through `newService()` (`cmd/ctxt/cmd/helpers.go:50`). Every one of them, read-only ones included, runs migrations (`helpers.go:72`), loads detectors (`:88`) and caches the default registry manifest (`:96`). With no instance selected they create and fill `db.sqlite` under the data directory (`internal/config/config.go:1080`). A named instance resolves only through local pidfiles (`dbPathForInstance`, `helpers.go:177-201`), so a remote instance can never be selected. The HTTP commands ignore `--instance` altogether (`clientEndpoints`, `helpers.go:109-129`).

The result is three behaviours that depend on which command the operator typed. The worst of them silently puts captured content into a store nobody processes.

Authorization is also missing. The static provider attaches roles to the principal (`internal/auth/static.go:54-59`), but nothing checks them: `Principal.HasRole` (`internal/auth/auth.go:72`) has no caller. Any valid token can install steps, create server-side watches or delete pipelines.

---

## Decision

**ctxt never opens the dpkms store. Every ctxt command reaches dpkms through its API, over one code path, whether dpkms is local or remote. There is no local or remote mode. Instances are names for endpoints (a URL plus a token). dpkms enforces per-route authorization on every request.**

### 1. One client path

- Every ctxt command that reads or changes knowledge calls the dpkms REST API (`/api/v1`) through one client package. ctxt stops importing the storage drivers, the storage factory and the database lock. A dependency test on `./cmd/ctxt` enforces this.
- gRPC stays a dpkms surface for other consumers. ctxt does not use it.

### 2. No fallback, loud failure

There is no local write, local read or local search fallback. Failures map onto kit's exit classes:

| Condition | Exit |
|---|---|
| Nothing answers at the endpoint (dial or DNS failure) | 70 `PREREQUISITE` |
| 401 or 403 | 5 `UNAUTHORIZED` |
| 404 | 3 `NOT_FOUND` |
| 409, or a policy veto | 4 `CONFLICT` |
| 400 or 422 | 2 `USAGE` |
| 5xx, or a timeout after connecting | 6 `TRANSIENT` |

A 401 or 403 always ends the request. It is never retried against another instance.

### 3. Instance → endpoint model

- **Named entries.** A `server.urls` entry takes an optional, unique `name`:

  ```yaml
  server:
    token: <default token>
    urls:
      - name: home
        url: https://dpkms.example.ts.net:7700
        token: <token for home>
      - name: laptop
        url: http://127.0.0.1:8080
  ```

- **Resolution order.** It extends today's `serverEndpoint()` order with the instance selector, which HTTP commands currently ignore:
  1. `--server <url>` pins that URL. The token comes from a `server.urls` entry with the same URL, else `server.token`. This is unchanged.
  2. `--instance <name|port>` or `CTXT_INSTANCE`:
     - first, the `server.urls` entry with that `name`;
     - otherwise, a running local dpkms whose pidfile name or port matches, as `http://127.0.0.1:<port>` with `server.token`;
     - otherwise, exit 70, and the error lists the configured names.
  3. The current-instance state file written by `ctxt instance use`, resolved as in step 2. A stale selection is an error, not a silent fall-through.
  4. `server.urls` in order. The first entry is the primary.
  5. `server.url`.
  6. The default, `http://127.0.0.1:8080`.
- **Failover.** Steps 4 to 6 may yield several endpoints. The client moves to the next one only after a dial or DNS failure, when no request byte was sent. It then prints to stderr which instance answered. Steps 1 to 3 always yield exactly one endpoint.
- **Tokens come only from config,** never from flags. This is unchanged.
- **`ctxt instance list | use | current`** shows and selects named endpoints as well as running local instances. `current` prints the resolved URL, the layer that chose it, and whether a token is attached.

### 4. Authorization: route scopes, role bundles

This follows ADR-023's capability model:

- **Every `/api/v1` route declares a required scope** in ADR-023's `verb:resource` grammar, for example `read:objects`, `write:objects`, `delete:objects`, `write:inbox`, `process:inbox`, `sync:registries`, `admin:plugins`, `admin:embeddings`, `admin:audit`.
- **Every gRPC method declares one too,** through a matching interceptor, so gRPC cannot be used to bypass the HTTP checks.
- **The static provider's roles are fixed bundles of scopes:**
  - `admin` holds every scope.
  - `reader` holds every `read:*` scope.
- A principal without the scope gets 403. A route with no declared scope fails a test, so every new route has to choose one.
- **Private instances** (no auth provider, loopback only) grant every scope, as today.
- **`GET /api/v1/whoami`** returns the principal ID and its effective scopes, so `ctxt status` and `ctxt instance current` can show who the client is.

### 5. Where each operator command lives

The rule follows the ctxt/dpkms boundary (`docs/dpkms-or-ctxt.md`): ctxt decides intent, and dpkms executes and persists.

- **Commands stay in ctxt and call admin-scoped endpoints when they:**
  - express a decision about what knowledge the instance holds or how it is retrieved, such as the embedding-model lifecycle, registry subscriptions and knowledge lint;
  - or need a component that is only reachable where dpkms runs, such as the embedding provider probe.
- **Commands move to the `dpkms` CLI when they are substrate maintenance** that takes a raw predicate over the storage schema or runs a long in-process worker against the database. The only case is `ctxt upgrade run`, which becomes `dpkms upgrade run`, with its `--where` SQL escape hatch. `ctxt upgrade plan` and `ctxt upgrade status` stay as reads.

### 6. Brain settings travel with the request

Profile definitions, search strategy, resurfacing and lint thresholds are ctxt configuration. ctxt resolves them locally and sends the values it resolved in the request body, for example `rrf_k`, the weights and pools and `min_score` for search. dpkms applies what it receives. It does not read a second copy of the brain's configuration.

### 7. Local capture stays local

- Clipboard, watched directories, browser tabs and history, and adapter sources (`ctxt ingest`) are read on the operator's machine. Their results are sent to dpkms over HTTP.
- **Dedup state stays with the client.** Directory-watch file records move out of the dpkms database (`watch_file_records`) into a state file that ctxt owns. It sits under `$XDG_STATE_HOME/ctxt/watch/` and is keyed per resolved instance. It follows the flock-plus-atomic-rename pattern of `internal/ambient/position`.
  - The paths are private to the laptop and mean nothing on the dpkms host.
  - An API for them would couple dpkms to a client concern.
  - dpkms's content-hash dedup after the pipeline stays the safety net.
- **`ctxt watch status` reports the local watcher only:** whether the process is running, the clipboard setting, and per-directory counts and last error.
- `ctxt watch` stops writing watch rows into the dpkms database.

---

## Rationale

- **One path is the only way to make `--instance` mean the same thing for every command.** It is also the only way to make "it worked" mean the content reached the instance the operator chose.
- **The fallback's premise, that a local daemon will pick the work up later, is false in the canonical topology.** For offline work, the answer is a local dpkms instance, which ADR-074's hub topology already describes. A ctxt that sometimes behaves as a second storage engine is not the answer.
- **Scopes on routes, with roles as bundles,** satisfy both ADR-023's capability model and the roles the static provider already issues, with no change to token configuration.
- **Moving knowledge-level decisions server-side would split the brain across two configurations.** Sending the resolved values keeps a single owner.

### Alternatives considered

| Alternative | Verdict | Reason |
|---|---|---|
| Keep the direct-store path for local instances, and use HTTP only for remote ones | Rejected | Two implementations of every command, and `--instance` still means different things. It is also the mode switch the owner ruled out. |
| Keep the local fallback, but make it loud | Rejected | The write still lands in a store the remote daemon never reads. |
| ctxt over gRPC | Rejected | The gRPC surface covers only analyze, jobs, search, object reads and entities (`api/proto`). REST already serves most commands, and ctxt already uses it. |
| A third `writer` role | Deferred | Capture-only tokens are expressible as scopes. Letting a token list scopes directly is a follow-up, not a new role. |
| Server-side directory-watch state (an API) | Rejected | It exposes client-private paths to the server and couples dedup to server storage for no gain. |
| Move the whole embedding and registry lifecycle to the `dpkms` CLI | Rejected | It forces an operator shell on the dpkms host for routine knowledge decisions. The provider probe has to run server-side anyway, and ADR-071 made `ctxt embeddings` the operator surface. |

---

## Consequences

### Positive

- Every command works against a remote instance with a token, and `--instance` selects the same target for all of them.
- Nothing in ctxt creates, migrates or fills a database. Read-only commands become read-only.
- An auth failure or an unreachable instance produces a correct, scriptable exit code. There are no false "queued" messages.
- dpkms gets real authorization, and a reader token can no longer change the instance.
- The in-process test fixture and the remote black-box suite exercise the same API that operators use.

### Negative

- **With no reachable dpkms, ctxt does nothing useful.** Capturing offline needs a local dpkms instance (ADR-074) or a later client-side buffer (ADR-066's buffer).
- **About forty endpoints have to be added or widened** before the direct paths can be removed. The plan lists them.
- **Commands that iterate** (bulk delete by filter, compose) make more round trips than an in-process loop did.
- **ADR-075 amendment.**
  - The CLI stops being a hook host. Every covered mutation now runs its before-hooks and writes its outbox rows in the daemon.
  - `exec` hooks come from the daemon host's config, not the laptop's.
  - `ctxt events deliver` loses its "no daemon" purpose.
  - ADR-075's first increment must host its `domain.Service` wrappers in the daemon only.
- **The ADR-070 upgrade banner** reads a shadow file next to the local pidfiles (`cmd/ctxt/cmd/root.go:126-130`). Against a remote instance it has to come from the API response instead. The plan covers this.
- **`storage.*` keys in a ctxt-only config file become inert.** `ctxt config validate` reports them.

## Implementation Notes

- **Order.** The plan lands the client package, the endpoint resolver and route authorization first. It then removes the local fallbacks, moves commands resource by resource, and finally deletes `newService`, `resolveStoragePath`, `dbPathForInstance`, `write_gate.go` and the `idxbridge` fallbacks.
- **Tests.**
  - ctxt command tests run against an in-process dpkms (`httptest` around the real router on a temporary SQLite).
  - Every test binary goes through `internal/testguard`, and none reaches `127.0.0.1:8080` or `:8081`.
  - External providers use recorded `xrr` cassettes.
- **Operator impact.** Every task is ADR-070 `Operator-Impact: none`. Tasks that change what an operator sees add a `none` row to the release notes.

## References

- [ADR-023 – Authentication and Authorization Model](ADR-023-authentication-authorization-model.md)
- [ADR-056 – Unified Enqueue API](ADR-056-unified-enqueue-api.md)
- [ADR-066 – Ambient Capture Substrate](ADR-066-ambient-capture-substrate.md)
- [ADR-070 – Pipeline and Index Versioning](ADR-070-pipeline-and-index-versioning.md)
- [ADR-071 – Embedding Index Versioning](ADR-071-embedding-index-versioning.md)
- [ADR-074 – Same-Owner Device Sync](ADR-074-same-owner-device-sync.md)
- [ADR-075 – External Hook Surface](ADR-075-external-hook-surface.md)
- [`docs/dpkms-or-ctxt.md`](../dpkms-or-ctxt.md)
