# `ctxt lateral` — Lateral Discovery Daemon

The `ctxt lateral` subcommand runs the lateral discovery substrate as a
long-running daemon. It subscribes to capture-pipeline events,
dispatches strategies through the registry, and emits candidate URLs
downstream for the resolver / cap-gate / materialization slice to
consume.

> **Status (track `lateral-daemon-wiring-20260509`)**: T-0312..T-0322
> ship the wiring shape end-to-end. Production deployments must wire
> their own LLM, GitHub, and platform clients per the [Wiring](#wiring)
> section. The default `ctxt lateral start` invocation registers zero
> strategies and no-ops on the poller.

## Commands

### `ctxt lateral start`

Run the daemon in the foreground.

```
ctxt lateral start [--lateral-config PATH] [--worker-id NAME] [--poll-interval DURATION]
```

Behaviour:

- Loads layered config (system → user → project → `--lateral-config`).
- Builds the strategy registry. Two gating mechanisms apply:
  - `cfg.JIT.Enabled` defaults `false` (opt-in) — JIT requires an LLM
    proposer dep that operators wire, so it's off until both the gate
    flips and the proposer is supplied.
  - `cfg.GitHub.Enable*` and roster's per-platform `*bool` gates default
    on (zero-value pointer = enabled per the `roster.Gates` contract).
    A fresh invocation with no operator-supplied API clients still
    registers the URL-shape strategies (`x`, `linkedin`, plus `arxiv`,
    `wikipedia`, `youtube`, `medium/substack/beehiiv` parents); the
    Google family + GitHub family register but degrade to no-op when
    their clients aren't wired (per each strategy's nil-client
    contract).
  - To opt OUT of a default-on strategy, set
    `strategies.<name>.enabled: false` in your config.
- Subscribes to `ctxt.ingest.object.captured` and
  `ctxt.ingest.object.persisted` on the in-process kit bus.
- Drives the cold-cycle poller at `--poll-interval` (default 5s).
- Stops cleanly on `SIGINT` / `SIGTERM`.

> **Note**: SIGHUP-driven config reload is plumbed at the substrate
> layer (`internal/lateral/config/reload.go`) and works for the
> mutable scoring / lifecycle slice; daemon-level `strategies.*` reload
> requires a process restart.

### `ctxt lateral status`

Reserved for the operational-reads slice (P5). Currently returns
`daemon.ErrNotWired`.

### `ctxt lateral config show`

Render the merged config block as JSON for operator triage.

```
ctxt lateral config show [--lateral-config PATH]
```

Useful for debugging gate decisions ("why isn't strategy X firing?").

## Configuration

The daemon reads a single root-keyed YAML at the conventional layer
locations (`/etc/ctxt/lateral.yaml`, `~/.config/ctxt/lateral.yaml`,
`./.ctxt/lateral.yaml`, plus `--lateral-config <path>` as a top-priority
extra). Layers merge via kit/core/config; later layers replace earlier
ones for scalars.

### Schema

```yaml
# Substrate config — owned by internal/lateral/config.
scoring:
  weights:
    session_topic: 0.5
    capture_window: 0.3
    interest_registry: 0.2
  threshold:
    sibling_repo: 0.45
    pinned_repo: 0.40
    starred_repo: 0.55
  cap_k:
    sibling_repo: 5
    pinned_repo: 3
    starred_repo: 3

lifecycle:
  cold_cycle_days: 30
  soft_delete_days: 30
  p3_reference_threshold: 3

jobs:
  engine_kind: memory   # "memory" only in v1; sqlite is a kit-side follow-on
  sqlite_path: ""

# Daemon-only — per-strategy gates.
strategies:
  jit:
    enabled: false        # opt-in catch-all
  github:
    enable_parent: true
    enable_gist: true
    enable_security_advisory: true
  google:
    enabled: true
  google_search:
    enabled: true
  google_scholar:
    enabled: true
  google_trends:
    enabled: true
  google_news:
    enabled: true
  x:
    enabled: true
  linkedin:
    enabled: true
  arxiv:
    enabled: true
  wikipedia:
    enabled: true
  medium:
    enabled: true
  medium_publication:
    enabled: true
  medium_profile:
    enabled: true
  substack:
    enabled: true
  substack_publication:
    enabled: true
  substack_post:
    enabled: true
  substack_notes:
    enabled: true
  beehiiv:
    enabled: true
  beehiiv_publication:
    enabled: true
  beehiiv_post:
    enabled: true
  youtube:
    enabled: true
```

### Gate semantics

- **Absent**: every strategy in `roster` defaults to enabled per the
  v1 spec posture. Setting `enabled: true` is a no-op on top of that
  default.
- **`enabled: false`**: strategy is not registered. Substrate dispatch
  treats it as if it didn't exist.
- **`{}` (empty entry)**: equivalent to absent — preserves the prior
  layer's value.
- **JIT** is special: it defaults to *disabled* (opt-in) because
  enabling it implies operator commitment to LLM cost. Set
  `strategies.jit.enabled: true` explicitly.
- **GitHub family** uses three flags rather than a single `enabled`
  because operators frequently want to disable advisories without
  disabling sibling-repo discovery.

## Wiring

The daemon's `cmd/ctxt/cmd/lateral.go` ships with **stub adapters**: it
constructs an in-memory bus + publisher and uses the package-shipped
strategy `Register` helpers. Production deployments need to provide:

| Strategy | Required dep | Source |
|----------|--------------|--------|
| jit | LLM Completer | `internal/lateral/adapters/llm.NewProposer` wrapping `kit/ai/llm.Completer` |
| jit | Fetcher | `internal/lateral/adapters/fetcher.NewHTTP` (default) or `NewIBR` (cookie-aware) |
| jit | Page-type classifier | `internal/lateral/adapters/pageshape.New` (heuristic + LLM stack) |
| github | APIClient | `internal/lateral/adapters/githubapi.NewREST` (REST-only v1; GraphQL deferred) |
| github | Breaker | `internal/lateral/adapters/breaker.New` wrapping `kit/core/breaker` |
| github | Fetcher | same as jit |

Operators thread these through `daemon.Build(cfg, deps)` and pass the
resulting registry to `daemon.NewLifecycle`. See
`internal/lateral/daemon/smoke_test.go` for the end-to-end shape.

## Operational signals

The daemon publishes events on the in-process kit bus. Operators wiring
the network adapter (kit/runtime/bus/network.go) can consume them
out-of-process.

| Topic | Severity | Source | When |
|-------|----------|--------|------|
| `ctxt.lateral.scan.failed` | error | `lateral.daemon` | Strategy probe returned an error |
| `ctxt.lateral.subpath.failed` | error | `lateral.jit` (or platform) | Per-path fetch failed |
| `ctxt.lateral.recipe.served` | info | `lateral.jit` | Cached proposal served (LLM outage) |
| `ctxt.lateral.reaper_cycle.completed` | info | `lateral.reaper_cycle` | One cold-cycle scan finished |
| `ctxt.lateral.scan.completed` | info | `lateral.daemon` | Per-event dispatch finished (carries `candidates_emitted` count) |
| `kit.config.snapshot.reload_failed` | error | `kit.config` | SIGHUP reload vetoed (immutable field changed) |

## Troubleshooting

### "lateral: github strategies disabled — APIClient wiring is a v1 follow-on"

The default `lateral start` body fail-softs github family registration
because `daemon.Build` panics if `cfg.GitHub.EnableParent=true` but
`Deps.GitHubAPI=nil`. Provide an APIClient (see [Wiring](#wiring)) and
remove the fail-soft branch in your fork.

### "JIT enabled but proposer nil" (panic at boot)

`daemon.Build` panics loud when JIT is enabled without an LLM Proposer.
Either wire `internal/lateral/adapters/llm.NewProposer` or set
`strategies.jit.enabled: false`.

### LLM outage shows up as `recipe.served` events

Expected: when the breaker for the LLM is open, `jit.Pipeline` serves
the cached proposal with `circumstance=llm_outage`. This is a
degraded-success signal, not a failure. Tune via `kit/core/breaker`
opts when constructing the breaker.

### GitHub rate-limit throttle

`github.NewGuardedAPI` wraps the APIClient with breaker + dynamic
floor. When the floor engages, fetches return `github.ErrFloorThrottled`
and emit `subpath.failed`. Operators tune via
`github.Config.FloorWindow` / `FloorBounds`.

### Pre-existing `cmd/ctxt/cmd` test panic

The `cmd/ctxt/cmd` test package fails to load on the parent branch
(kit/cli double-registers `--config` in init). Wiring logic lives in
`internal/lateral/daemon` so the wiring slice stays test-covered until
the kit/cli issue is fixed.

## Architecture references

- [`internal/lateral/strategy.go`](../../internal/lateral/strategy.go) — `LateralStrategy` interface, `CapturedEvent`, `Candidate`.
- [`internal/lateral/registry.go`](../../internal/lateral/registry.go) — Family-aware dispatch (Platform > Shape > JIT).
- [`internal/lateral/strategies/jit/wiring.go`](../../internal/lateral/strategies/jit/wiring.go) — JIT register helper.
- [`internal/lateral/strategies/github/wiring.go`](../../internal/lateral/strategies/github/wiring.go) — GitHub family register helper.
- [`internal/lateral/strategies/roster/roster.go`](../../internal/lateral/strategies/roster/roster.go) — P4 platform roster register helper.
- [`internal/lateral/daemon/`](../../internal/lateral/daemon/) — Daemon wiring (this track's home).
- [`internal/lateral/adapters/`](../../internal/lateral/adapters/) — Adapter packages (T-0314..T-0319).

## Track lineage

This document captures the `lateral-daemon-wiring-20260509` track
(T-0312..T-0323). Prior tracks established the substrate
(`lateral-substrate-*`), strategy implementations, scoring, lifecycle,
and identity-key resolver. The daemon-wiring slice composes those
pieces behind a CLI surface.
