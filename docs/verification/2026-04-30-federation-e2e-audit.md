# Federation E2E Coverage Audit — 2026-04-30

**Task:** T-0088 (tools-showcase-scenarios)
**Author:** $USER
**Stories:** US-0318, US-0319, US-0320, US-0321, US-0322, US-0323
**ADR:** ADR-064 (Status: Proposed)

---

## Summary

Federation Phase-1 unit-tested in isolation; **zero cross-instance e2e
coverage**. `LocalPusher` + `RemotePusher` exercised via in-process
SQLite + `httptest.Server` only. No test boots two `dpkms serve`
binaries, configures `federations:` block, and observes objects flowing
source→target. Async background worker, watermark advance, cycle
detection, lifecycle (`dpkms ps`/`stop`/`reboot`), backup-restore-rebuild,
and DAG chaining are unimplemented in the binary path — `LocalPusher` is
not instantiated from `cmd/dpkms` or `serve.go`. Stories US-0318/0321/
0322/0323 also gate on ADR-064 ratification (currently Proposed); every
acceptance criterion ≥ 1 hop beyond pure pusher unit calls is a gap.

---

## Per-Story Inventory

### US-0318 — Configure Federation Targets

| # | Acceptance Criterion | Test | Cross-Instance? |
|---|---|---|---|
| 1 | Valid config loads | partial: `internal/config/validate.go::validateFederations` (unit) | N |
| 2 | Cycle detected at startup | none — validate.go comment defers full topology check | N |
| 3 | Zero entries = isolated | none | N |
| 4 | Local SQLite target accepted | none (no validate test for sqlite path shape) | N |
| 5 | Remote HTTP target accepted | none | N |
| 6 | `sync_mode: async` starts goroutine | none — no worker wired in `serve.go` | N |
| 7 | `sync_mode: inline` synchronous | none — no FederationPush step registered | N |
| 8 | `interval` parsed | none | N |
| 9 | Missing interval → error | none (validate.go has the check; no test) | N |
| 10 | Inline + interval ignored | none | N |
| 11 | Duplicate name → error | none (validate.go has check; no test) | N |
| 12 | YAML round-trip | none | N |

### US-0319 — Async Federation Push to Local Merged DB

| # | Acceptance Criterion | Test | Cross-Instance? |
|---|---|---|---|
| 1 | Objects appear within interval | none — no worker | N |
| 2 | Dedup by content-hash | `internal/federation/local_test.go::TestLocalPusher_Idempotent` | N (in-process) |
| 3 | Watermark advances on success | none — table created (mig 025) but no Get/Update code | N |
| 4 | Crash mid-push: watermark not advanced | none | N |
| 5 | Edges travel with objects | `local_test.go::TestLocalPusher_EdgesTravelWithObjects` | N (in-process) |
| 6 | Mentions travel with objects | none — `LocalPusher.Push` does not iterate mentions | N |
| 7 | Zero objects → no-op | none | N |
| 8 | Push errors logged + continue | none — no worker | N |
| 9 | Goroutine exits on shutdown | none — no worker | N |

### US-0320 — Inline Federation Push During Ingest

| # | Acceptance Criterion | Test | Cross-Instance? |
|---|---|---|---|
| 1 | Inline push fires during job | none — no `steps.FederationPush` | N |
| 2 | Push failure → job fails | none | N |
| 3 | Failed job retried | none | N |
| 4 | Push success → object at target immediately | none | N |
| 5 | `sync_mode: inline` activates path | none | N |
| 6 | Multiple inline targets all-or-nothing | none | N |
| 7 | Partial fail = job fail | none | N |
| 8 | Step appears in job trace | none | N |
| 9 | `sync_mode: async` skips inline path | none | N |

### US-0321 — Multi-Instance dpkms ps + Lifecycle

| # | Acceptance Criterion | Test | Cross-Instance? |
|---|---|---|---|
| 1 | `dpkms ps` shows live instances | none — `cmd/dpkms/cmd/ps.go` exists, no test | N |
| 2 | Stale pidfile cleanup on `ps` | none | N |
| 3 | `dpkms stop --port` SIGTERM | none — `cmd/dpkms/cmd/shutdown.go`, no test | N |
| 4 | Stop on absent port → error | none | N |
| 5 | `dpkms reboot --port` | none — `cmd/dpkms/cmd/reboot.go`, no test | N |
| 6 | Reboot preserves config + DB | none | N |
| 7 | Each instance own DB + config | none — one-instance e2e in `serve_integration_test.go` only | N |
| 8 | Pidfile written on serve | partial: `internal/pidfile/pidfile.go` exists; no e2e | N |
| 9 | Pidfile removed on shutdown | none | N |
| 10 | Stale pidfile cleared on serve | none | N |

### US-0322 — Backup and Federation Rebuild

| # | Acceptance Criterion | Test | Cross-Instance? |
|---|---|---|---|
| 1 | `dpkms backup` produces tarball with db + config | partial: `cmd/dpkms/cmd/backup_test.go` (unit) | N |
| 2 | Archive `config.yaml` retains `federations:` | none | N |
| 3 | `dpkms restore` unpacks both files | none — `restore.go`, no test | N |
| 4 | Serve after restore → goroutines start | none — no worker exists | N |
| 5 | Downstream DB repopulated | none | N |
| 6 | No special federation backup cmd | trivially true (no command exists) | N |
| 7 | Housekeeping per-instance | partial: `housekeeping_test.go` (unit; no fed scope) | N |

### US-0323 — DAG Federation Chain Push

| # | Acceptance Criterion | Test | Cross-Instance? |
|---|---|---|---|
| 1 | Object flows source→merged→team→archive | none | N |
| 2 | Each hop dedups by content-hash | indirect: `local_test.go::TestLocalPusher_Idempotent` (1 hop, in-proc) | N |
| 3 | Watermarks advance independently per node | none | N |
| 4 | Cycle (A→B→A) detected at startup | none | N |
| 5 | Intermediate nodes push downstream | none | N |
| 6 | Terminal node makes no outbound push | none | N |
| 7 | Identical binary, only config differs | trivially true (binary has no fed wiring) | N |
| 8 | LocalPusher for paths, RemotePusher for URLs | none — selection logic absent | N |

---

## Coverage Matrix Rollup

| Story  | Criteria | Any Test | Cross-Instance | Gap |
|--------|---------:|---------:|---------------:|----:|
| US-0318 |       12 |        0 |              0 |  12 |
| US-0319 |        9 |        2 |              0 |   9 |
| US-0320 |        9 |        0 |              0 |   9 |
| US-0321 |       10 |        0 |              0 |  10 |
| US-0322 |        7 |        2 |              0 |   7 |
| US-0323 |        8 |        1 |              0 |   8 |
| **Tot** |   **55** |    **5** |          **0** | **55** |

"Any Test" counts in-process unit tests (`LocalPusher` calls,
`handlers_federation_test.go` httptest, validate.go unit). None spin
two `dpkms` binaries. **Cross-instance e2e: 0 / 55.**

---

## ADR-064 Status Note

ADR-064 is **Proposed**, not Accepted. All six stories already presume
the model is decided (refer to it as authoritative; embed config
shapes from §Decision). Phase-1 code (`internal/federation/{local,
remote,pusher}.go`, migration 025, validate hook, HTTP receive
handler) has landed under the Proposed assumption.

**Recommendation:** ratify ADR-064 to Accepted before any of the gap
e2e tests below ship. If the model changes (e.g., pull added to
v1, fan-in, a different conflict policy), the criteria themselves
shift and the new tests would need rewrites. Ratification is
prerequisite for stable test surface.

---

## Gap List (Prioritized)

P1 = blocks scenario 1 client-bubble dashboard. P2 = phase-1 gaps
without scenario-1 dependence.

1. **P1** — Two-binary async push e2e (US-0319, AC #1+#3): boot two
   `dpkms serve` instances on free ports with source→target federation
   block; capture in source; assert object appears in target SQLite
   within interval; assert watermark row exists with `last_synced_at >
   epoch`. Blocks scenario 1 because client-bubble dashboard reads from
   merged DB; no merged DB without working async push.
2. **P1** — Cycle detection e2e (US-0318 AC #2, US-0323 AC #4):
   construct config with self-reference (URL points back); `dpkms
   serve` exits non-zero; stderr contains `cycle`. Blocks scenario 1
   because deploying multiple ctxt instances per profile risks cycles.
3. **P2** — Idempotency under concurrent push (US-0319 AC #2,
   US-0323 AC #2): two source instances pushing same content-hash to
   one target merged DB; assert single row at target. Validates dedup
   under realistic write concurrency.
4. **P2** — Lifecycle e2e (US-0321 AC #1+#3+#5): `dpkms serve` ×2 on
   different ports; `dpkms ps` lists both; `dpkms stop --port`; `ps`
   shows one; `dpkms reboot`; PID changes. Validates ops surface for
   multi-profile deployments.
5. **P2** — Backup-restore-rebuild e2e (US-0322 AC #1+#4+#5): backup
   source instance; delete merged DB; restore source on a different
   data dir; serve; assert merged DB repopulated to original count
   within one interval. Validates DR workflow stated in ADR.

US-0320 (inline) gaps deferred — Phase-1 inline mode requires a
pipeline step that does not yet exist; e2e premature until step
lands. US-0323 full DAG chain (3 hops) deferred behind P1#1 — needs
async push working first.

---

## Recommendations

- Ratify ADR-064 (Proposed → Accepted) before merging gap e2e tests.
- Wire `internal/federation` into `cmd/dpkms/cmd/serve.go`: spawn
  one goroutine per `cfg.Federations[i]` with `SyncMode == "async"`;
  read/write `federation_watermarks` via storage layer (currently
  table exists, no Go API for it).
- Add `internal/storage/sqlite/federation_watermarks.go` with
  `GetWatermark(name)` / `UpdateWatermark(name, t)` to unblock
  watermark-related criteria.
- Stand up an `internal/federation/e2etest` helper that boots two
  servers via `httptest.Server` + isolated SQLite paths so gap tests
  share setup.
- File the 5 gap tasks against `tools-showcase-scenarios` (P1 first
  two, P2 next three); link each back to T-0088.

---

## References

- ADR-064 — `docs/decisions/ADR-064-federation.md`
- Stories — `docs/stories/federation/US-0318..US-0323-*.md`
- Code inventory — `internal/federation/{pusher,local,remote,pull}.go`
- Receive handler — `internal/server/http/handlers_federation.go`
- Migration — `internal/storage/sqlite/migrations/025_federation_watermarks.sql`
- Validate — `internal/config/validate.go::validateFederations`
- Lifecycle binaries — `cmd/dpkms/cmd/{ps,shutdown,reboot,backup,restore}.go`
