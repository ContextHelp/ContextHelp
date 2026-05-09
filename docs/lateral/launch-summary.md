# Lateral discovery — launch summary

The lateral discovery substrate shipped across five phases (P1–P5).
This doc is the top-level operator-facing summary: what landed, what
gates each piece, how to roll back per strategy, and the 30d / 90d
review checkpoints.

Cross-references:
- Per-strategy specs: [`strategies/`](strategies/) (one file per
  platform).
- Identity-key conventions: [`strategies/identity-keys.md`](strategies/identity-keys.md).
- Incident response: [`../operations/runbooks/lateral.md`](../operations/runbooks/lateral.md).
- Dashboard: [`../operations/dashboards/lateral.json`](../operations/dashboards/lateral.json).
- Alert rules: [`../operations/dashboards/lateral-alerts.yaml`](../operations/dashboards/lateral-alerts.yaml).

## What shipped per phase

### P1 — Substrate (T-0301 …)

The core scoring / lifecycle / candidate state machine. `internal/
lateral/{scoring,lifecycle,promote,reject,materialize,jobs}/`.
Bus-event catalog at `internal/lateral/events/catalog.go` — 26 topics
mirroring `schemas/lateral_events.json`.

### P2 — Identity + dispatch (T-0307 …)

Strategy interface (`internal/lateral/strategy.go`), Registry
dispatch (`internal/lateral/registry.go`), identity-key resolver +
candidate dedup. The substrate became strategy-aware here.

### P3 — Tier-A platform strategies (T-0308 …)

GitHub family: parent + gist + security-advisory. JIT (catch-all
LLM-driven). Adapters: `internal/lateral/adapters/{llm,fetcher,
breaker,bus,githubapi,pageshape}`.

### P4 — Roster (T-0316 …)

Platform roster: google + 4 children, x, linkedin, arxiv, wikipedia,
medium + 2 children, substack + 3 children, beehiiv + 2 children,
youtube. Custom-domain detection via Hints (canonical_host,
generator).

### P5 — Eval, rollout, observability (T-0324 …, this track)

- **Eval** (`internal/lateral/eval/`): replay harness, labelled
  fixture set, precision/recall/negative-pass metrics, conversion
  rate aggregator.
- **Rollout** (`internal/lateral/rollout/`): per-strategy enable
  gate (SIGHUP-reloadable), percentage traffic shaping, kill-switch
  for operator emergencies.
- **Observe** (`internal/lateral/observe/`): per-strategy quality
  score (precision + conversion).
- **Operations**: Grafana dashboard, Prometheus alert rules,
  incident runbook, lateral-eval CI gating.

## Configuration map

The full layered config lives in `lateral.<key>` of the project's
YAML (system → user → project → extras). Daemon-side keys:

```yaml
# Substrate (always present).
scoring:
  weights: {session_topic: 0.5, capture_window: 0.3, interest_registry: 0.2}
lifecycle: {cold_cycle_days: 30, soft_delete_days: 30, p3_reference_threshold: 3}
jobs: {engine_kind: memory}

# Strategy gates (this track).
strategies:
  jit:
    enabled: false           # opt-in; needs LLM adapter
  github:
    enable_parent: true
    enable_gist: true
    enable_security_advisory: true
  google:        {enabled: true, sample_percent: 100}
  google_search: {enabled: true, sample_percent: 100}
  google_scholar: {enabled: true, sample_percent: 100}
  google_trends: {enabled: true, sample_percent: 100}
  google_news:   {enabled: true, sample_percent: 100}
  x:             {enabled: true}
  linkedin:      {enabled: true}
  arxiv:         {enabled: true}
  wikipedia:     {enabled: true}
  medium:                {enabled: true}
  medium_publication:    {enabled: true}
  medium_profile:        {enabled: true}
  substack:              {enabled: true}
  substack_publication:  {enabled: true}
  substack_post:         {enabled: true}
  substack_notes:        {enabled: true}
  beehiiv:               {enabled: true}
  beehiiv_publication:   {enabled: true}
  beehiiv_post:          {enabled: true}
  youtube:               {enabled: true}
```

## Roll back per strategy

Two escalating levers — the operator CLI surface (`ctxt lateral kill`,
`ctxt lateral restore`) is on the roadmap but NOT shipped today.
Until it lands, both levers go through YAML + SIGHUP:

1. **YAML enabled flag + SIGHUP** (hard cutover, ~1s):
   ```yaml
   strategies:
     <strategy>:
       enabled: false
   ```
   ```bash
   pkill -HUP ctxt   # daemon re-reads config + applies gate
   ```
   The strategy stops dispatching on the next captured event.

2. **Sample-percent ramp-down + SIGHUP** (graceful, no abrupt cutover):
   ```yaml
   strategies:
     <strategy>:
       sample_percent: 10   # 10% of traffic; bump down further as needed
   ```
   ```bash
   pkill -HUP ctxt
   ```
   Useful when the strategy isn't broken but is producing
   higher-than-expected load. Setting `sample_percent: 0` is a soft
   kill-switch (denies all traffic without changing the enabled gate).

## 30-day / 90-day review checkpoints

### 30d post-launch

Measure:

- **Conversion rate per strategy** (`ctxt_lateral_conversion_rate`):
  what fraction of emitted candidates promoted to Promoted state?
  Strategies < 5% conversion need investigation — they're claiming
  events but the substrate isn't taking them seriously.
- **Precision per strategy** (PR-time `lateral-eval` workflow): has
  any strategy drifted off the labelled fixture set? Catch
  drift early.
- **scan.failed rate by mechanism**: chronic rate_limit_floor on
  github = under-budget; chronic parse_error = upstream schema drift.

Tune:

- Strategies with low precision but high traffic: tighten Applies()
  filter or bump sample_percent down.
- Strategies with high precision but low conversion: investigate
  why operators aren't promoting candidates (often: candidate URL
  preview is unhelpful → tune the preview map).

### 90d post-launch

Measure:

- **Quality score distribution** (`ctxt_lateral_quality_score`):
  sort strategies. Anything in the bottom quartile with
  conversion < 5% and precision < 0.7 is a candidate for retirement
  (or a major rewrite).
- **Reaper cycle duration**: should stay flat. Linear growth = the
  candidate table is unbounded → check soft-delete is firing.
- **Identity-key collision rate**: should be ~0. Non-zero = a
  strategy's identity key is non-canonical; cross-reference the
  identity-keys.md spec.

Tune:

- Retire strategies that don't earn their keep — they cost dispatch
  budget for no candidate value.
- Add new strategies via the staged rollout flow (1% → 10% → 50% →
  100%, see runbook).

## Known limitations

- **Eval fixtures are synthetic**: per the brief's "shape regression,
  not real-world accuracy" framing. Real-world precision is measured
  via the conversion rate, not the labelled set.
- **xrr cassettes deferred**: P3 + P4 still use in-memory stub
  fetchers. Migration plan at [`xrr-cassette-migration.md`](xrr-cassette-migration.md).
- **No per-strategy ML retraining**: out of scope for the
  engineering track. Tuning is config-driven (sample_percent,
  enabled flag).
- **Kill-switch is operational, not durable**: a SIGHUP reload that
  re-asserts the YAML's enabled=true clobbers the kill state. The
  runbook documents this; production playbooks should always edit
  YAML when a kill should stick.

## Deferred work

- **Real production telemetry exporter** (Prometheus deployment,
  Grafana hosting). The schemas + rules are in place; deployment is
  ops work.
- **xrr cassette migration** (T-0334 deferred — xrr binary not
  yet available).
- **Live-LLM eval harness**: today's eval skips JIT (no LLM
  adapter at eval time). When deterministic-LLM testing infra
  lands, JIT joins the labelled-fixture flow.
- **Status subcommand** (`ctxt lateral status` returns ErrNotWired):
  read-the-bus implementation is a follow-on operational slice.
- **Operator CLI surface** (`ctxt lateral kill <id>` /
  `ctxt lateral restore <id>` / `ctxt lateral events ...` /
  `ctxt lateral candidates inspect <id>`): not yet wired. Soft
  kill-switching today goes through YAML edits + SIGHUP. The
  rollout/observe primitives (StrategyGate, KillSwitch) are in place;
  the CLI wrapper around them is the missing piece.
- **SIGUSR1 breaker reset**: not wired. Operators restart the daemon
  to re-arm a stuck breaker.

## Cross-references

- `internal/lateral/daemon/wiring.go` — registry construction.
- `internal/lateral/daemon/lifecycle.go` — bus subscription + dispatch.
- `internal/lateral/daemon/reload.go` — SIGHUP-driven gate / sampler reload.
- `internal/lateral/eval/replay.go` — offline replay harness.
- `internal/lateral/observe/quality.go` — composite quality score.
- `cmd/ctxt/cmd/lateral.go` — daemon entry point.
- `cmd/ctxt/cmd/lateral_eval.go` — eval CLI surface.
