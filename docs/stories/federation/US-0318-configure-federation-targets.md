# US-0318: Configure Federation Targets

**System Types:** dpkms (self-hosted)
**Personas:** [Platform Integrators](../../personas/platform-integrators.md)

---

## User Goal

As a platform integrator, I want to configure federation targets in `config.yaml` so that
objects from this instance are pushed to local SQLite merged DBs or remote HTTP endpoints
(Phase 2), with per-target sync modes and intervals.

---

## Context

Federation is a push-only DAG. Each instance declares zero or more downstream targets.
Targets may be local SQLite files (same host) or remote HTTP dpkms instances (Phase 2).
Zero federation config means isolated instance — no push, no sync. Cycle detection at
startup prevents infinite push loops. Config drives `FederationConfig` struct; no
runtime UI needed in Phase 1.

---

## Acceptance Criteria

- [ ] Valid federation config block loads without error on startup
- [ ] Cycle in federation DAG detected at startup and rejected with descriptive error
- [ ] Zero `federations` entries = isolated instance; no background push goroutines started
- [ ] Local SQLite target: `url: /path/to/merged.sqlite` accepted and validated
- [ ] Remote HTTP target: `url: https://team.internal:8080` accepted (Phase 2; placeholder ok)
- [ ] `sync_mode: async` starts background goroutine per target
- [ ] `sync_mode: inline` pushes synchronously at capture time (Phase 2)
- [ ] `interval` field (duration string, e.g. `5m`) parsed for async targets
- [ ] Missing `interval` on async target returns validation error
- [ ] `interval` on inline target ignored (or validation warning emitted)
- [ ] Each target has unique `name` field; duplicate names → startup error
- [ ] Config struct serializes/deserializes cleanly via YAML round-trip

---

## Implementation Notes

### config.yaml block

```yaml
federations:
  - name: merged-local
    url: /data/merged.sqlite
    sync_mode: async
    interval: 5m
  - name: team-server          # Phase 2
    url: https://team.internal:8080
    sync_mode: async
    interval: 10m
```

### FederationConfig struct (pseudocode)

```
FederationTarget:
  Name     string
  URL      string        // sqlite path or http(s) URL
  SyncMode string        // "async" | "inline"
  Interval time.Duration // async only

FederationConfig:
  Targets []FederationTarget
```

### Cycle detection (pseudocode)

```
BuildDAG(targets) → directed graph: self → each target.Name
DFS from self; if any target.URL resolves to self → cycle error
// Phase 1: self-reference check sufficient (single hop)
// Phase 2: query remote /api/v1/federation/topology for full graph
```

### Startup validation order

```
1. Parse YAML → FederationConfig
2. Validate each target: name non-empty, url parseable, sync_mode in {async,inline}
3. Async target: interval > 0 required
4. Duplicate name check
5. Cycle detection
6. If any error → log + exit(1)
7. Zero targets → log "federation disabled (isolated)" + continue
```

---

## E2E Test Checklist

- [ ] Config with valid local SQLite target loads; server starts without error
- [ ] Config with duplicate target names → startup error, server exits non-zero
- [ ] Config with cycle (A→B→A) → startup error with cycle path in message
- [ ] Empty `federations: []` → server starts; no push goroutines in pprof
- [ ] Async target missing `interval` → startup validation error
- [ ] YAML round-trip: marshal FederationConfig → unmarshal → fields match
- [ ] `sync_mode: async` + valid interval → background goroutine confirmed running
  (check via `/debug/pprof` goroutine count or log line)

---

## Related Stories

- [US-0319](./US-0319-async-push-local-merged.md) — async push to local merged DB
- [US-0320](./US-0320-inline-push-on-capture.md) — inline sync mode (Phase 2)
- [US-0321](./US-0321-http-federation-push.md) — remote HTTP target push (Phase 2)
- [US-0322](./US-0322-federation-topology-api.md) — topology API for cycle detection (Phase 2)
- [US-0323](./US-0323-federation-status-monitoring.md) — federation push status monitoring

---

## E2E Tests

- `test/e2e/federation/cycle_detection_test.go::TestCycleDetection_SelfLoop_Rejected`
- `test/e2e/federation/cycle_detection_test.go::TestCycleDetection_DuplicateTargetNames_Rejected`
- `test/e2e/federation/cycle_detection_test.go::TestCycleDetection_EmptyTargetName_Rejected`
- `test/e2e/federation/cycle_detection_test.go::TestCycleDetection_ValidNoLoop_Accepted`
- `test/e2e/federation/cycle_detection_test.go::TestCycleDetection_MultiHopABA_Phase2Deferred`
- planned: `test/e2e/federation/configure_targets_test.go::TestConfigure_AddTarget`
- planned: `test/e2e/federation/configure_targets_test.go::TestConfigure_ListTargets`
- planned: `test/e2e/federation/configure_targets_test.go::TestConfigure_RemoveTarget`
- planned: `test/e2e/federation/configure_targets_test.go::TestConfigure_PauseResume`
