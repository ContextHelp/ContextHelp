# Lateral pipeline incident response

Operational runbook for the lateral discovery substrate. Every alert
in `docs/operations/dashboards/lateral-alerts.yaml` links to a
section here.

## TL;DR — emergency levers

| Symptom | Lever |
|---------|-------|
| Strategy hammering an upstream API | `ctxt lateral kill <strategy-id>` (or YAML `enabled: false` + SIGHUP) |
| LLM provider stuck | `recipe.served` should clear when provider recovers; check breaker state |
| Identity-key collision | `ctxt lateral candidates rollback --object-id=<id>` |
| Reaper stalled | restart daemon (preserves bus state) |

## Scan failed rate

**Alert**: `LateralScanFailedRate` (>5% scan.failed/scan.completed for 10m)

### Diagnosis

```bash
# Group failures by strategy + mechanism.
ctxt lateral events --topic=ctxt.lateral.scan.failed --window=10m \
  | jq '.qualifiers | {strategy_id, mechanism, error}' \
  | sort | uniq -c | sort -rn
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
  to the previous container or kill-switch the strategy until a fix
  ships.

## LLM outage stuck on

**Alert**: `LateralRecipeServedSustained` (recipe.served firing 30m+)

### Diagnosis

JIT's outage detector flips into recipe-fallback mode when the LLM
breaker trips. Sustained recipe mode means the breaker isn't recovering.

```bash
# Check breaker state.
ctxt lateral status --json | jq '.jit.breaker'

# If state == "open" + last_failure is recent: LLM provider is still
# down. If state == "open" + last_failure is hours old, the breaker
# probably needs a manual reset.
```

### Recovery

- **Provider down**: check provider's status page. recipe.served will
  clear automatically when the provider comes back + breaker
  half-opens.
- **Breaker stuck open**: confirm the LLM is actually reachable
  (curl the provider's API directly with an auth header). If yes,
  send SIGUSR1 to the daemon to force-reset the breaker:
  ```bash
  pkill -USR1 ctxt
  ```
- **Provider partial outage**: switch to the alternate provider via
  `lateral.jit.proposer.provider` config + SIGHUP reload.

## Per-strategy zero emission

**Alert**: `LateralStrategyZeroEmission` (strategy emits 0 candidates
for 1h despite traffic)

### Diagnosis

```bash
# How many events did the strategy claim this hour?
ctxt lateral events --topic=ctxt.lateral.scan.completed --window=1h \
  | jq 'select(.qualifiers.strategy_id == "<id>") | .qualifiers'
```

If `dispatched > 0` and `candidates_emitted == 0`:
- The strategy's `Probe` is running but returning empty results.
  Likely an upstream schema change.
- Or the sample-percent flag was set to 0 by mistake.

```bash
# Verify the gate + sampler config.
ctxt lateral config show | jq '.SamplePercent."<id>"'
```

### Recovery

- **Sample-percent regression**: edit YAML to remove the explicit 0
  (or set to 100) + SIGHUP.
- **Schema regression**: roll back the daemon binary, file an
  upstream-change incident.

## Identity-key collision

The resolver merges two unrelated entities under the same identity
key. Symptom: a candidate list contains URLs from clearly different
sources.

### Diagnosis

```bash
# Find the offending identity key.
ctxt lateral candidates inspect <object-id> --json \
  | jq '.candidates[] | {url, identity_key}' \
  | sort -u
```

If two URLs you'd expect to be distinct share the same `identity_key`,
the strategy emitted a non-canonical key. Cross-reference
`docs/lateral/strategies/identity-keys.md` for the per-platform format.

### Recovery

- Roll back the affected candidate cluster:
  ```bash
  ctxt lateral candidates rollback --object-id=<id> --strategy=<id>
  ```
  This re-runs the strategy with `--force-resolve` so identity keys
  are re-derived.
- If the strategy is consistently emitting bad keys, kill-switch it
  and file a bug.

## Per-strategy crash loop

**Alert**: `LateralStrategyCrashLoop` (single strategy >0.5
failures/sec sustained 5m)

### Diagnosis

```bash
ctxt lateral events --topic=ctxt.lateral.scan.failed --window=5m \
  | jq 'select(.qualifiers.strategy_id == "<id>")'
```

Look at the `error` qualifier. Crash loops usually mean either:
- Panic recovered by the kit/runtime/bus subscriber wrapper (will
  show as `error: panic recovered: ...`).
- Same upstream error type repeating (e.g. all `parse_error`).

### Recovery

Use the kill-switch — operator emergency lever:

```bash
# Immediate, in-process. No SIGHUP round-trip.
ctxt lateral kill <strategy-id>

# Verify the strategy is now skipped.
ctxt lateral events --topic=ctxt.lateral.scan.completed --window=1m \
  | jq 'select(.qualifiers.strategy_id == "<id>")' | wc -l
# Should be 0.
```

After the upstream / code issue is resolved:

```bash
ctxt lateral restore <strategy-id>
```

Or edit YAML + SIGHUP for durable restoration.

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
ctxt lateral events --topic=ctxt.lateral.sanity_check.violated --window=1h \
  | jq '{subject, detail}'
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
