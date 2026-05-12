# Lateral pipeline incident response

Operational runbook for the lateral discovery substrate. Every alert
in `docs/operations/dashboards/lateral-alerts.yaml` links to a
section here.

## TL;DR — emergency levers

The daemon ships with two operator levers: editing the config YAML +
sending SIGHUP, or restarting the daemon. There is no dedicated
operator CLI yet (`ctxt lateral status` and friends are deferred — see
`docs/lateral/launch-summary.md` § Deferred). Use these:

| Symptom | Lever |
|---------|-------|
| Strategy hammering an upstream API | YAML `strategies.<id>.enabled: false` + `pkill -HUP ctxt` |
| LLM provider stuck | `recipe.served` should clear when provider recovers; check the metric on the dashboard |
| Identity-key collision | YAML disable the strategy + restart daemon (see Identity-key collision section) |
| Reaper stalled | restart the daemon process |

Inspection happens via Prometheus (the lateral dashboard +
`docs/operations/dashboards/lateral.json` panels) and process logs.
The daemon's bus events surface as Prometheus metrics; query the
`ctxt_lateral_*` series rather than reading the bus directly.

## Scan failed rate

**Alert**: `LateralScanFailedRate` (>5% scan.failed/scan.completed for 10m)

### Diagnosis

Use the dashboard's "scan.failed by mechanism" panel — it groups
failures by the `qualifiers.mechanism` axis (`breaker_open`,
`rate_limit_floor`, `fetch_error`, `parse_error`, etc.).

If you need raw events, `journalctl -u ctxt-lateral` carries the bus
emissions when the daemon is run as a systemd service:

```bash
journalctl -u ctxt-lateral --since="10m ago" \
  | grep "ctxt.lateral.scan.failed"
```

The `mechanism` qualifier tells you the failure mode:
- `breaker_open` — upstream API breaker tripped; transient.
- `rate_limit_floor` — github rate-limit floor below threshold; pause.
- `fetch_error` — network error in the strategy's Fetcher; check
  upstream availability.
- `parse_error` — strategy code failed to parse a real response.
  Likely a schema change.

### Recovery

- **Transient breaker_open / rate_limit_floor**: no action needed.
  Confirm scan.failed clears within 30m. If not, escalate.
- **fetch_error sustained**: check the upstream provider's status
  page. If they're up, look at egress firewall + DNS resolution.
- **parse_error sustained**: this is a code regression. Roll back
  to the previous container or disable the strategy via YAML +
  SIGHUP until a fix ships.

## LLM outage stuck on

**Alert**: `LateralRecipeServedSustained` (recipe.served firing 30m+)

### Diagnosis

JIT's outage detector flips into recipe-fallback mode when the LLM
breaker trips. Sustained recipe mode means the breaker isn't recovering.

Check the dashboard's "recipe.served by circumstance" panel. The
`circumstance: llm_outage` series is the relevant signal — if it's
sustained, the breaker is open.

For raw breaker state, the daemon doesn't expose it via API today.
Read the journal to see the most recent breaker state transition:

```bash
journalctl -u ctxt-lateral --since="1h ago" \
  | grep -E "breaker.*(open|closed|half)" | tail -20
```

### Recovery

- **Provider down**: check provider's status page. recipe.served will
  clear automatically when the provider comes back + breaker
  half-opens.
- **Breaker stuck open**: confirm the LLM is actually reachable
  (curl the provider's API directly with an auth header). If yes,
  restart the daemon — the breaker is in-process, restart re-arms it:
  ```bash
  sudo systemctl restart ctxt-lateral
  ```
  Force-reset via signal is not currently wired (deferred).
- **Provider partial outage**: switch to the alternate provider via
  `lateral.jit.proposer.provider` config + `pkill -HUP ctxt`.

## Per-strategy zero emission

**Alert**: `LateralStrategyZeroEmission` (strategy emits 0 candidates
for 1h despite traffic)

### Diagnosis

Use the dashboard's per-strategy "candidates_emitted" panel. If the
strategy's series is flat at 0 while neighbors emit normally, either:
- The strategy's `Probe` is running but returning empty results.
  Likely an upstream schema change.
- Or `sample_percent` is set to 0 in the operator's YAML.

Verify the active config:

```bash
# The daemon doesn't expose this via API; read the YAML the running
# process loaded. The path comes from the systemd unit's
# Environment=CTXT_CONFIG=... or the daemon's --config flag.
cat /etc/ctxt/lateral.yaml | yq '.strategies."<id>".sample_percent'
```

### Recovery

- **Sample-percent set to 0**: edit YAML to remove the explicit 0
  (missing key = 100% / full traffic) or set to 100 + SIGHUP.
- **Schema regression**: roll back the daemon binary, file an
  upstream-change incident.

## Identity-key collision

The resolver merges two unrelated entities under the same identity
key. Symptom: a candidate list contains URLs from clearly different
sources.

### Diagnosis

There is no `ctxt lateral candidates inspect` subcommand yet. Inspect
the lateral_candidate table directly:

```bash
sqlite3 /var/lib/ctxt/lateral.sqlite \
  "SELECT identity_key, url FROM lateral_candidate WHERE object_id='<id>'"
```

If two URLs you'd expect to be distinct share the same `identity_key`,
the strategy emitted a non-canonical key. Cross-reference
`docs/lateral/strategies/identity-keys.md` for the per-platform format.

### Recovery

The resolver doesn't ship a rollback subcommand. Mitigation steps:

1. Disable the offending strategy via YAML + SIGHUP so the bad keys
   stop landing.
2. Manually remove the polluted rows from `lateral_candidate` and
   `lateral_edges` (back up first):
   ```bash
   sqlite3 /var/lib/ctxt/lateral.sqlite \
     "DELETE FROM lateral_candidate WHERE strategy='<id>' AND identity_key='<bad-key>'"
   ```
3. Re-enable the strategy after the keys are fixed in code.

If the strategy is consistently emitting bad keys, file a bug; the
fix is a code change in the strategy package.

## Per-strategy crash loop

**Alert**: `LateralStrategyCrashLoop` (single strategy >0.5
failures/sec sustained 5m)

### Diagnosis

Read the journal for the strategy's failures:

```bash
journalctl -u ctxt-lateral --since="5m ago" \
  | grep -E "scan.failed.*strategy_id.*<id>"
```

Look at the `error` qualifier. Crash loops usually mean either:
- Panic recovered by the kit/runtime/bus subscriber wrapper (will
  show as `error: panic recovered: ...`).
- Same upstream error type repeating (e.g. all `parse_error`).

### Recovery

Use the YAML+SIGHUP soft kill-switch:

```bash
# 1. Edit the YAML (path varies per deploy):
yq -i '.strategies."<id>".enabled = false' /etc/ctxt/lateral.yaml

# 2. SIGHUP for runtime reload.
pkill -HUP ctxt

# 3. Verify the strategy is skipped — its candidates_emitted series
# in the dashboard should drop to 0 within ~1 minute.
```

After the upstream / code issue is resolved, flip `enabled` back to
true and SIGHUP again. Note: `sample_percent: 0` in the YAML achieves
the same effect with finer-grained control (see Sampler doc).

## Reaper cycle stalled

**Alert**: `LateralReaperCycleStalled` (no completion for 10m+)

### Diagnosis

The cold-cycle reaper is the daemon-side poller that ages
Probationary candidates into Expired. If it stalls, candidates with
expired TTLs leak in the lateral_candidate table.

```bash
# Check the daemon process is alive.
pgrep -f "ctxt lateral start"

# Check the job/poller worker logs for panic / error.
journalctl -u ctxt-lateral --since="20m ago" | grep -i reaper
```

### Recovery

Reaper state is in-memory; restart is safe:

```bash
sudo systemctl restart ctxt-lateral
```

If the stall recurs immediately, it's a daemon-side bug. Capture a
goroutine dump (`pkill -QUIT ctxt`) and file an incident.

## Sanity-check violations

**Alert**: `LateralSanityCheckViolated` (any sanity_check.violated
event)

These are structural assertions. Treat every fire as a real bug.

### Diagnosis

```bash
journalctl -u ctxt-lateral --since="1h ago" \
  | grep "ctxt.lateral.sanity_check.violated"
```

The `detail` map contains the assertion-specific dump. Common
violations:
- `state_machine_invalid_transition` — candidate moved
  Promoted → Probationary; impossible per the spec.
- `score_out_of_range` — scoring layer produced > 1.0 or < 0.0.

### Recovery

Sanity violations don't auto-heal. File an incident, capture the
detail dump, and roll back the daemon to the previous version while
the bug is fixed.

## Production rollout — staged

When introducing a new strategy or a high-risk change, ramp via the
sample-percent knob:

| Stage | Sample percent | Verify |
|-------|---------------|--------|
| 1     | 1             | scan.completed counts; alert thresholds quiet |
| 2     | 10            | conversion rate ≈ pre-rollout neighbors |
| 3     | 50            | precision/recall vs labelled fixtures stable |
| 4     | 100           | full rollout |

YAML:

```yaml
strategies:
  <new-strategy>:
    enabled: true
    sample_percent: 1   # bump per stage; SIGHUP between stages
```

Each stage runs minimum 24h before bumping. If any alert fires,
revert to the previous stage's percent.

## Operator CLI — what's NOT shipped

The following commands are referenced informally but DO NOT exist in
the daemon today:

- `ctxt lateral status` — returns `daemon.ErrNotWired`
- `ctxt lateral events --topic=...` — no bus reader subcommand
- `ctxt lateral kill <id>` / `ctxt lateral restore <id>` — soft
  kill-switch is YAML+SIGHUP only
- `ctxt lateral candidates inspect/rollback` — no candidate-table CRUD
  surface
- SIGUSR1 breaker reset — restart-only

Operators inspect via Prometheus + journalctl + sqlite. Mutating
operations go through YAML edits + SIGHUP or daemon restart. CLI
parity is on the roadmap (see `docs/lateral/launch-summary.md`
§ Deferred).
