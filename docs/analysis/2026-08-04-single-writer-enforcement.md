# Single-Writer Enforcement — Findings and Design

> **Date:** 2026-08-04
> **Applies to:** dPKMS, ctxt
> **Companion:** [2026-08-04-storage-architecture-pov.md](2026-08-04-storage-architecture-pov.md)

## Summary

The storage POV flagged dpkms-as-sole-writer as "the most fragile contract in the system — enforced by convention and a reachability probe." Tracing the code shows the reality is one step worse: **the probe is not in the write path at all**. `idxbridge` exists but is wired into nothing; every `ctxt` CLI command opens the SQLite file directly, runs migrations, and writes — daemon running or not. Two writers on one file is not a fallback failure mode today; it is the steady state. What keeps it working is SQLite's own WAL cross-process locking plus a 5-second busy timeout, not any contract the codebase enforces.

The recommendation is an advisory `flock` on a sidecar lockfile, held for the daemon's lifetime and taken non-blocking per-command by the CLI's direct path, with lock-free reads and an instructive error when the lock is held. The building blocks (a working flock helper, pidfile liveness, an acknowledged flock TODO) already exist in-tree.

## 1. The fallback decision, as actually implemented

**`idxbridge` is dormant.** `internal/idxbridge/idxbridge.go` implements the probe-then-fallback design faithfully: `Probe` hits `GET /health` with a 500 ms timeout (idxbridge.go:34-35, 115-129), caches the result for 30 s (idxbridge.go:37, 101-112), and `SearchObjects` routes to `GET /api/v1/search` when live, delegating to the local `Fallback` otherwise (idxbridge.go:141-156). But a repo-wide search finds no importer outside the package and its test — no command in `cmd/ctxt` or `cmd/dpkms` constructs an `IdxBridge`. The bridge also only covers `SearchObjects`; writes were never in its scope.

**The actual CLI path is unconditionally direct.** Every `ctxt` command obtains its service via `newService()` in `cmd/ctxt/cmd/helpers.go:46-97`, which:

1. Resolves the DB path (`helpers.go:55`, `resolveStoragePath` at 101-111);
2. Opens the SQLite file directly via `storageutil.NewDriver` (`helpers.go:63`, dispatching at `internal/storageutil/factory.go:12-21` to `sqlite.New`);
3. Calls `driver.Init(ctx)` (`helpers.go:68`) — which is `Migrate` (`internal/storage/sqlite/driver.go:97-99`). **Even read-only commands execute the migration ladder**, i.e. potential DDL writes, on every invocation.

There is no daemon probe anywhere in this path. Write commands (`ctxt analyze` → `service.Analyze` → `Queue.Enqueue` at `internal/service/service.go:254`) insert job rows directly into the shared file; the daemon's worker pool picks them up. The system's normal ingest flow with a running daemon is *already* a two-writer configuration.

**Instance routing makes the two-writer state explicit.** `dbPathForInstance` (`helpers.go:132-147`) scans live pidfiles for a *running* daemon and returns its `DBPath` so the CLI can open that same file directly. The error message when no instance matches ("no running dpkms instance named…") confirms the design intent: direct CLI access to a live daemon's database is a feature, not an accident.

**Mode of the direct open.** `sqlite.New` (`internal/storage/sqlite/driver.go:49-58`) opens read-write (default `mattn/go-sqlite3` mode) with DSN pragmas:

```
_journal_mode=WAL &_synchronous=NORMAL &_foreign_keys=ON &_cache_size=-64000 &_busy_timeout=5000
```

(driver.go:54). No `mode=ro` path exists; there is no read-only open anywhere in the CLI.

## 2. Locking inventory

| Mechanism | Status | Evidence |
|---|---|---|
| WAL journal mode | On, every connection (DSN pragma) | `internal/storage/sqlite/driver.go:54` |
| `busy_timeout` | 5000 ms | `internal/storage/sqlite/driver.go:54` |
| `locking_mode=EXCLUSIVE` | Not used | — |
| flock / lockfile before direct open | **None** | `sqlite.New` and `newService` take no lock |
| pidfile check before direct open | **None** — pidfiles are used for discovery/routing only, never as a write gate | `cmd/ctxt/cmd/helpers.go:132-147` |
| Daemon-side exclusivity | **None on the DB.** `pidfile.CheckNameConflict` guards instance *names*, not DB paths (`internal/pidfile/pidfile.go:108-118`); two daemons with different names/ports can serve the same file, each running a worker pool | `cmd/dpkms/cmd/serve.go:392` |

In-tree precedents that the enforcement design can reuse:

- **flock helper.** `internal/cursor/lock.go:12-28` implements exclusive advisory `syscall.Flock` on a sidecar file with correct release semantics (per-fd, no unlink to avoid racing waiters).
- **pidfile liveness.** `internal/pidfile/pidfile.go:58-91` prunes stale pidfiles via `kill -0`.
- **Acknowledged gap.** `cmd/ctxt/cmd/upgrade_dpkms.go:483-489` states outright: "Cross-process locking (e.g. flock on the shadow path) is a follow-up — for now the worker's Manager.Start guard is single-process."
- **A migration comment documents a live symptom.** `internal/storage/sqlite/migrations.go:500-504` notes migration idempotency guards exist because "a second handle reads pre-WAL-checkpoint state."

## 3. Concrete two-writer failure scenarios

WAL makes concurrent cross-process access *crash-safe at the page level*; none of the following corrupt the B-tree. They corrupt behavior.

1. **Concurrent migration race.** Daemon starts (`serve.go:148` → `driver.Init`) while a CLI command is mid-`Migrate`, or vice versa. Both run the same DDL ladder; the idempotency shims (`migrations.go:500-510`) were added reactively per-migration, so any migration without a guard trips "duplicate column" or partial-DDL states. This gets worse every time a migration is added by someone unaware the ladder runs multi-process.

2. **Mixed-build binaries on one file.** The CGO bifurcation the POV flagged: `internal/storage/sqlite/sqlite3_fts5.go` warns that without `-tags fts5` the package "still compiles but migrations will fail at runtime." A CLI binary built without the tag (or a `modernc.org/sqlite`-linked tool — see `internal/federation/local.go:154`) opening a daemon's live DB fails mid-migration or mid-FTS-write with the daemon holding correct state — the CLI's error looks like data corruption to the user.

3. **`SQLITE_BUSY` past the 5 s ceiling.** A bulk CLI ingest (`ctxt import batch`) holds long write transactions while the daemon's workers write job status. Either side that waits >5 s gets `database is locked` at an arbitrary statement — a partial, non-retryable failure surfacing far from its cause.

4. **Duplicate worker pools.** Because `CheckNameConflict` keys on name not `DBPath`, a second `dpkms serve` pointed at the same file starts a second pool (`serve.go:358`) and immediately runs crash recovery (`serve.go` step 10d, `queue.RecoverStale`) — resetting the first daemon's legitimately-running jobs to `pending`, causing double execution. The stale-job sweeper (`internal/jobs/worker.go:480`) then perpetuates mutual job theft.

5. **Housekeeping against a live daemon.** `dpkms housekeeping` opens the DB directly (`housekeeping.go:31`) and runs `PRAGMA wal_checkpoint(TRUNCATE)` and `VACUUM` (`housekeeping.go:142, 306-313`). `VACUUM` requires exclusive access; against a live daemon it either fails with BUSY or stalls the daemon's writers. Nothing warns the operator.

6. **Probe-cache windows (once idxbridge is wired).** The design caches *negative* probe results for 30 s too (idxbridge.go:105-111 stamps `probedAt` unconditionally): a CLI that probed just before daemon startup keeps writing directly for up to 30 s after the daemon is up. Conversely a daemon that is listening but still inside `driver.Init` answers `/health` only after storage init — a slow migration makes the probe a false negative exactly when direct access is most dangerous (scenario 1).

## 4. Enforcement design

### Options considered

**(a) SQLite `locking_mode=EXCLUSIVE`.** Rejected. It is per-connection; `database/sql` pools multiple connections, so the daemon would deadlock against itself unless capped to one connection. Contention surfaces as `SQLITE_BUSY` at arbitrary statements — the worst possible UX — and it cannot distinguish "daemon holds it" from "another CLI holds it," so no instructive error is possible.

**(b) Pidfile + liveness check before direct open.** Rejected as primary. It is check-then-act (TOCTOU): the daemon can start between the CLI's scan and its open. It also fails open — a daemon that crashes without removing its pidfile is handled by the `kill -0` prune, but a daemon whose pidfile write failed (`serve.go:413` treats that as a *warning*, non-fatal) is invisible to the check while very much writing. Useful only as a diagnostic layer.

**(c) Advisory flock on a sidecar lockfile.** **Recommended.** Kernel-owned, so a crashed holder releases automatically — no stale-lock recovery logic. Atomic acquire, no TOCTOU. Non-blocking probe (`LOCK_NB`) gives an immediate, attributable "who holds it" answer. Same-host only — which is exactly the deployment model, and the constraint is honest: NFS-mounted DB paths are already unsupported by SQLite WAL itself. Precedent in-tree at `internal/cursor/lock.go`.

### Recommended mechanism

A `dblock` helper (natural home: alongside `internal/cursor/lock.go`'s pattern, or a small `internal/dblock` package) managing `<dbpath>.lock`:

- **Acquire semantics:** `flock(LOCK_EX)`; after acquiring, truncate-and-write a small JSON body `{pid, role: "daemon"|"cli", instance, started_at}` — purely diagnostic (the flock is the authority; the body is for error messages). Never unlink on release (per the cursor helper's race note, `internal/cursor/lock.go:25-27`).
- **Daemon:** acquire with a short bounded wait (retry `LOCK_NB` for ~10 s) in `serve.go` **before** `driver.Init` at serve.go:144 — closing scenario 1 (migration race) and scenario 4 (`CheckNameConflict` gains a real DB-level guard: second daemon on the same file fails fast with the first daemon's identity). Hold for process lifetime; kernel releases on any exit.
- **CLI direct path:** in `newService()` before `storageutil.NewDriver` (`helpers.go:63`), take the lock `LOCK_NB`:
  - **Acquired** → proceed exactly as today; release in the existing `cleanup` closure (`helpers.go:89`). CLI commands are short, so daemon startup landing inside one waits at most seconds within its bounded retry.
  - **Held** → read the lock body and branch:
    - Holder is a daemon → the correct future behavior is "route via daemon API" (which requires finally wiring `idxbridge` and extending it beyond `SearchObjects`); until that lands, fail with an instructive error (below). For read-only commands (`find`, `show`, `list`), open with `mode=ro` in the DSN and skip `Init` — WAL readers are safe alongside a single writer, so reads should *never* be blocked by the lock.
    - Holder is another CLI → brief bounded retry (~2 s), then the same error.
- **Housekeeping:** `dpkms housekeeping` must acquire the lock the same way (its `VACUUM`/`wal_checkpoint(TRUNCATE)` are the strongest reason any process needs true exclusivity), closing scenario 5.
- **Handoff:** there is no explicit handoff protocol and none is needed — ownership transfers implicitly through the kernel. Daemon stops → lock releases → next CLI command acquires. CLI finishes → daemon's bounded startup retry acquires. The only tunable is the daemon's retry budget, which should exceed the longest plausible CLI command that isn't a bulk import (bulk imports while starting the daemon are precisely the case that *should* serialize visibly).

### UX when the lock is held

```
ctxt: database is in use by dpkms (pid 41230, instance "main", since 09:14)
  → this command will be routed via the daemon API in a future release
  → for now: stop the daemon (`dpkms shutdown`) or use a read command
```

Principles: name the holder (from the lock body), state the resolution, exit non-zero with a distinct exit code so scripts can detect contention, and never silently degrade a write into a no-op. Read commands proceed in `mode=ro` without any message.

### Sequencing

1. Land the flock gate (daemon + CLI + housekeeping) — this is the backstop and is small: one helper, three call sites.
2. Split `newService` reads from writes so read commands stop running `Migrate` and open `mode=ro` — this alone removes the most common CLI write (`schema_migrations` DDL probing) from the contention window.
3. Wire `idxbridge` for real and extend it to the write surface, at which point "lock held by daemon" becomes "route to daemon" instead of an error. Fix the negative-probe cache (scenario 6) as part of the wiring: cache daemon-up for 30 s, but daemon-down for ~1 s.

Step 1 turns the single-writer claim from convention into mechanism; steps 2–3 turn the resulting friction back into UX. The POV's verdict called for "hard single-writer enforcement" as work that defends a property the architecture already claims — the flock gate is that work, and nothing about it blocks or prejudges the eventual daemon-API routing.
