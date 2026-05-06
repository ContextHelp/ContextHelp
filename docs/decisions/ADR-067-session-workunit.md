# ADR-067 – Session / WorkUnit as a First-Class Type

> **Status:** Accepted
> **Date:** 2026-05-05
> **Author:** jadb
> **Applies to:** ctxt CLI, ctxd local daemon, dpkms storage
> **Supersedes:** None
> **References:** ADR-053 (KnowledgeObject as pipeline draft), ADR-063 (graph-canonical KnowledgeObject), ADR-066 (ambient capture substrate), prior art: OpenChronicle session manager (`~/.p/sandbox/OpenChronicle/src/openchronicle/session/manager.py`, `docs/session.md`)

---

## Context

ADR-066 introduces ambient capture: a stream of small RawEvents (clipboard changes, foreground-app switches, file drops, browser visits) flowing through `ctxd` into KnowledgeObjects on dpkms. Today's data model is **per-item**: each KnowledgeObject is independent. There is no notion of "this clipboard event, that browser visit, and the file I just saved are all part of the same 30-minute work session on Project X."

This is a real gap for the queries ambient capture is meant to enable:

- "What did I work on yesterday afternoon?"
- "Pull up everything from the session where I was investigating the auth-token bug."
- "Compose a summary of my last three sessions on the migration project."

ctxt has rich per-item enrichment (mentions, tags, embeddings, graph edges), but no temporal/contextual grouping. Ambient capture amplifies this gap because it produces **many small items** instead of a few hand-curated ones — without grouping, the user drowns in dust.

OpenChronicle solved this with a **session boundary state machine** (`session/manager.py`, `docs/session.md`). It treats a session as a bounded chunk of focused work, cut by three rules (idle gap / single-app focus / hard timeout) with a frequent-switching exception. OC's evidence is convincing:

- v1 of OC wrote per-capture entries; v2 moved to session-level after two failure modes (long sessions under-reported due to dedup; event files conflated days with no temporal pivot). The post-mortem in `docs/session.md` lines 93–100 is explicit: "session-level writes make long work correct by construction."
- Their cutter has been tuned: 5-min idle gap, 3-min soft cut with a frequent-switching exception (≥2 distinct apps in last 2 min suppresses), 2-hour hard timeout.
- They use the session as the **unit of compression**: the S2 reducer fires per-session, and a per-stage bookmark (`flush_end`, `classified_end`) on the session row prevents double-processing during incremental flushes.

**Constraints unique to ctxt** (vs. OC):

- ctxt is **not screen-memory**. We don't capture AX trees or per-keystroke events; ambient sources emit *signals* (clipboard, focus, file). The cutter logic still applies; the data being grouped is different and smaller.
- **dpkms is often remote** (per ADR-066's saved constraint). Sessions must be cut **client-side in `ctxd`** — the foreground-window signal lives on the user's machine, and a remote dpkms can't observe it. dpkms receives `SessionID` as opaque metadata.
- **ctxt has no S2 reducer in v1.** OC's session is also the LLM-batching unit; ctxt's session in v1 is a *grouping* unit only. Per-session compression/summarization is deferred (the timeline-aggregator caveat from ADR-066 §Rationale point 6 applies here too — when we do add a reducer, sessions are the right unit, but v1 ships without one).
- **`KnowledgeObject` is in `pkg/pluginapi/`** (stable plugin API). Adding `SessionID` is a deliberate API expansion that plugins must be able to ignore safely.

The brainstorm + plan call this out as Phase 4 work; this ADR locks the type, schema, cutter algorithm, and dpkms-side semantics before code lands.

**The question:** What shape does the Session/WorkUnit type take, where does the cutter run, how does dpkms persist and query by session, and what's the migration story for KnowledgeObjects created before the type existed?

---

## Decision

**Adopt `Session` (a.k.a. `WorkUnit`) as a first-class type with three rules:**

1. **Session is a bounded chunk of focused work, cut client-side by `ctxd`.** Cutting requires the foreground-window signal (and other local signals); it cannot move to dpkms. Sessions are emitted to dpkms as opaque metadata via `session_id` on each enqueue.

2. **Cutter algorithm is OpenChronicle's, ported verbatim** (with constants tunable in `[ambient.session]` config):
    - **Hard cut (idle):** no capture-worthy event for `gap_minutes` (default 5).
    - **Soft cut (single-app focus):** one app holds focus for `soft_cut_minutes` (default 3) AND the user is NOT frequent-switching (≥2 distinct apps in the last `recent_switch_window_minutes` = 2 minutes).
    - **Hard timeout:** session exceeds `max_session_hours` (default 2) regardless of activity.
    - **Force end** on `ctxd` shutdown; **daily safety net** at local 23:55 closes any session still open.
    - Cut tick (`session.tick_seconds`, default 30) fires the cutter even when no events arrive (idle/timeout cuts).

3. **Session is grouping-only in v1.** The Session row carries identity, timestamps, app-mix metadata, and per-stage bookmarks (reserved for future reducers/classifiers per OC's pattern). It does NOT trigger LLM summarization in v1 — that's a Phase 6+ concern paired with the deferred timeline-aggregator (ADR-066 §Rationale 6).

### Type shape

```go
// pkg/pluginapi/pluginapi.go — new type
type Session struct {
    ID            string         `json:"id"`              // "sess_<12-hex>" (matches OC convention)
    StartedAt     time.Time      `json:"started_at"`
    EndedAt       *time.Time     `json:"ended_at,omitempty"` // nil while active
    EndReason     string         `json:"end_reason,omitempty"` // "idle" | "soft_cut" | "timeout" | "shutdown" | "daily_safety_net"
    AppMix        []AppShare     `json:"app_mix,omitempty"`    // {bundle_id, share_pct} sorted desc
    EventCount    int            `json:"event_count"`          // total RawEvents that joined this session
    SourceMix     []string       `json:"source_mix,omitempty"` // distinct ambient-source names (clipboard, foreground, ...)
    ProfileID     string         `json:"profile_id,omitempty"` // matches KnowledgeObject.ProfileID semantics
    Metadata      map[string]any `json:"metadata,omitempty"`   // open-ended per-deployment fields

    // Per-stage bookmarks — reserved for future reducers/classifiers (Phase 6+).
    // In v1 these are zero-valued; populated when a per-session reducer ships.
    FlushEnd      *time.Time     `json:"flush_end,omitempty"`
    ClassifiedEnd *time.Time     `json:"classified_end,omitempty"`

    CreatedAt     time.Time      `json:"created_at"`
    UpdatedAt     time.Time      `json:"updated_at"`
}

type AppShare struct {
    BundleID string  `json:"bundle_id"`
    ShareSec float64 `json:"share_sec"` // foreground time in seconds
}

// KnowledgeObject — extension
type KnowledgeObject struct {
    // ... existing fields unchanged ...
    SessionID string `json:"session_id,omitempty"` // optional; populated for ambient-captured items
}
```

### Storage shape (SQLite + Postgres parity)

```sql
CREATE TABLE sessions (
    id              TEXT PRIMARY KEY,
    started_at      TEXT NOT NULL,
    ended_at        TEXT,
    end_reason      TEXT,
    app_mix         TEXT,           -- JSON array of {bundle_id, share_sec}
    event_count     INTEGER NOT NULL DEFAULT 0,
    source_mix      TEXT,           -- JSON array of strings
    profile_id      TEXT,
    metadata        TEXT,           -- JSON
    flush_end       TEXT,
    classified_end  TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX idx_sessions_started_at ON sessions(started_at);
CREATE INDEX idx_sessions_profile_started ON sessions(profile_id, started_at);

ALTER TABLE objects ADD COLUMN session_id TEXT;
CREATE INDEX idx_objects_session ON objects(session_id) WHERE session_id IS NOT NULL;
```

`session_id` on `objects` is a **soft FK** (no enforced foreign key) — sessions on dpkms can lag the events that reference them by milliseconds in network-loss + replay scenarios; soft FK avoids an enqueue ordering constraint.

### Wire shape (the existing enqueue HTTP path per ADR-056)

`ctxd` POSTs to `/api/v1/analyze` with two new optional fields:

```json
{
  "content": "...",
  "type": "url",
  "ambient_source": "clipboard",
  "fingerprint": "sha256:...",
  "session_id": "sess_a1b2c3d4e5f6"
}
```

Sessions are POSTed to `/api/v1/sessions` (new endpoint) on `started_at` and again on `ended_at`. Session creation is idempotent on `session.id` (PUT semantics) so replay after network loss is safe.

### CLI surface

```
ctxt session list [--since X --until Y --profile P]
ctxt session show <id>
ctxt session tail        # follow the active session live (ctxd-side)
ctxt compose --session <id>
ctxt compose --since "2 hours ago"   # auto-resolves to overlapping sessions
ctxt analyze --session-id <id>       # manual override (advanced)
```

### Bus events (extending ADR-066's taxonomy)

| Topic | Emitted when |
|-------|--------------|
| `ctxt.ambient.session.opened` | `_start_locked` fired; new sess_xxx assigned |
| `ctxt.ambient.session.closed` | `_end_locked` fired; carries end_reason payload |
| `ctxt.ambient.session.event_joined` | A RawEvent was tagged with the active SessionID |
| `ctxt.ambient.session.cut_evaluated` | `check_cuts()` ran (every tick_seconds); useful for tuning |

---

## Rationale

### Chosen: client-side cutter, dpkms-side persistence, opaque SessionID on enqueue

- **Cutter must be local because the foreground-window signal is local.** A remote dpkms cannot see what app you're focused on. ctxd is the only place the cutter algorithm has the data it needs. dpkms receives the SessionID as opaque metadata; it does not re-cut or second-guess.
- **OC's three rules are well-tuned and battle-tested.** Their post-mortem (`docs/session.md` lines 93–100) explicitly documents the v1→v2 transition; reinventing the wheel risks rediscovering the same failure modes. Port verbatim with the same defaults; tune later if ctxt's signal mix differs in practice.
- **Frequent-switching exception is the load-bearing nuance.** Without it, IDE+terminal+browser workflows get chopped every 3 minutes. With it, a real focus break (lunch, meeting) still cuts cleanly. Don't drop this when porting.
- **Soft-FK on `objects.session_id` matches the actual constraint.** With network-loss replay, RawEvents tagged with `sess_X` may arrive at dpkms before the session-opened endpoint call lands. A hard FK would force ordering; soft FK lets storage absorb both orders and reconciles on Session arrival.
- **Per-stage bookmarks are reserved-but-unused in v1.** When ADR-066's deferred timeline aggregator + a per-session reducer eventually ship, the schema is already correct and we don't migrate twice. Cost in v1: two nullable columns. Trivial.
- **Session as opaque-id-on-enqueue keeps the API minimal.** No new pipeline-step contract; existing pipelines ignore `SessionID` if they want; queries just gain a new filter.
- **`Session` lives in `pkg/pluginapi/` next to `KnowledgeObject`.** Plugins that want session-aware behavior (e.g. a "recent sessions" digest pipeline) can read it; plugins that don't care can ignore it. Follows the existing extension contract.

### Rejected alternatives

1. **Cut sessions on dpkms (server-side).** Rejected: dpkms has no foreground-window signal in the remote-deployment topology that ADR-066 targets. Any cutter logic on dpkms would be working from a strictly poorer signal (just timestamps + source identifiers) — degraded, not different.

2. **Make session a tag instead of a type.** Rejected: tags are open-ended labels. Sessions have explicit start/end timestamps, end-reason, app-mix, and per-stage bookmarks — all queryable as structured fields. Forcing this through the tag system would lose the structure and double the query cost (joins across the tag table for every session-window query).

3. **Hierarchical sessions (sub-sessions for app-focus blocks within a meta-session).** Rejected for v1. OC explored similar ideas and settled on a flat model. Hierarchy is tempting but introduces real questions (where does the boundary live? do sub-sessions cut differently?) that v1 doesn't need to answer. Revisit if real query patterns demand it.

4. **Use the existing `metadata.dates_mentioned` field instead of a new SessionID.** Rejected: that field captures dates the *content* mentions, not the temporal grouping the *capture* belongs to. Different semantics; conflating them would break existing queries.

5. **Synthesize sessions retroactively from KnowledgeObject timestamps.** Rejected: lossy. Without the foreground-window signal, idle/soft-cut detection collapses to "gap > N minutes" — works for hard cuts only. v1 captures the cut decisions at the moment they're made (when the data is richest) and stores them.

6. **One Session per ambient source.** Rejected: defeats the purpose. The user's "session" spans clipboard events, browser visits, file drops, and foreground-window switches all at once; that cross-source unification is the value.

---

## Consequences

### Positive

- **Temporal queries become trivial.** "Yesterday afternoon's work" → `WHERE started_at BETWEEN ...`. No more scanning per-item timestamps and bucketing client-side.
- **Compose by session is a one-line query.** `ctxt compose --session sess_X` becomes `WHERE session_id = ?`. Clean foundation for session-scoped summaries when the reducer eventually ships.
- **Ambient capture stops drowning the user in dust.** Many small items grouped under a session ID present as one work unit, not 200 noise items.
- **OC's tuned defaults give us a reasonable starting point** without months of tuning. Power users get the same knobs OC documents (gap_minutes, soft_cut_minutes, max_session_hours).
- **Schema extension is forward-compatible.** Per-stage bookmarks reserved-now, populated-later means no second migration when reducers land.
- **Soft-FK design absorbs network-loss replay** without enforcing strict ordering on `ctxd`'s replay logic. Operationally simpler.
- **Session lives in `pkg/pluginapi/`** so plugins can opt into session-awareness without forcing the rest of the ecosystem to care.

### Negative

- **`KnowledgeObject` gains a field in the stable plugin API.** Existing plugins must be safe to ignore `SessionID`; new plugins that want session-awareness must handle the empty-string case (objects from non-ambient sources, or pre-Phase-4 objects) gracefully.
- **Storage migration touches the canonical objects table.** ALTER TABLE is online for SQLite (with WAL) and Postgres but introduces deployment ordering: dpkms must be on the new schema before a `ctxd` daemon enqueues with `session_id` populated. Standard rolling-deploy concern; documented in the migration notes.
- **Cutter has tuning surface.** Three constants × profile-level overrides means real users will hit edge cases that need adjustment. Mitigated by adopting OC's defaults (already-tuned) and documenting the exact same tuning table OC ships in `docs/session.md`.
- **Sessions are a `ctxd`-only concept on the producer side.** Users running ambient-capture via service-manager (brew/launchctl/systemd) get sessions; users still using only one-shot `ctxt analyze` invocations get `session_id == ""`. Asymmetry is real but principled — without a foreground-window stream, there's no session to cut.
- **Cross-machine session continuity is not handled in v1.** A user with a laptop and a desktop both running `ctxd` produces two independent session streams. ADR-064 federation could eventually merge them; out of scope here.

### Neutral / Considerations

- **Session naming.** OC uses `sess_<12-hex>` (e.g. `sess_a1b2c3d4e5f6`). Adopt the same shape for cross-tooling familiarity. Not a typed Tau-prefix convention; sessions are plain UUIDs squeezed.
- **Profile scope.** Sessions inherit the active profile at start-time. If the user switches profiles mid-session, the session ends and a new one starts. Matches ctxt's existing per-profile data isolation.
- **Privacy posture per session.** kit/runtime/policy CEL rules can filter sessions (drop sessions where bundle-id deny-list dominated; redact sessions tagged `personal` from `work` profile queries). Same enforcement boundary as ADR-066 (client-side, before enqueue).
- **Idempotency on session creation.** PUT `/api/v1/sessions/{id}` accepts the same payload twice with no side effects. Required for replay-after-network-loss correctness.
- **Backfill from existing KnowledgeObjects?** Not in v1. Pre-Phase-4 objects keep `session_id == ""` forever; no synthetic sessions. If desired later, a one-time migration tool could bucket per-day or per-burst, but the lossy nature of retro-cutting argues against it.

---

## Implementation Notes

### New / modified files

| Path | Change |
|---|---|
| `pkg/pluginapi/pluginapi.go` | Add `Session` + `AppShare` types; add `SessionID string` to `KnowledgeObject` |
| `internal/ambient/session/cutter.go` | Three-rule cutter (port of OC `session/manager.py`) |
| `internal/ambient/session/cutter_test.go` | Idle/soft/timeout/frequent-switching/force-end test cases |
| `internal/ambient/session/store.go` | Local SQLite mirror (sessions table on the `ctxd` side for resume-after-restart) |
| `internal/storage/types.go` | Wire `Session` through the existing storage interface |
| `internal/storage/sqlite/migrations/0NN_sessions.sql` | sessions table + objects.session_id |
| `internal/storage/postgres/migrations/0NN_sessions.sql` | parity migration for Postgres backend |
| `internal/api/sessions.go` | New `/api/v1/sessions` endpoint (POST/PUT idempotent, GET list/show) |
| `internal/api/analyze.go` | Accept `session_id` + `ambient_source` + `fingerprint` on enqueue |
| `cmd/ctxt/cmd/session.go` | `ctxt session {list,show,tail}` |
| `cmd/ctxt/cmd/compose.go` | `--session`, `--since`, `--until` flags |

### Cutter reference (Go port of OC)

```go
// internal/ambient/session/cutter.go
package session

type Cutter struct {
    GapMinutes              int           // default 5
    SoftCutMinutes          int           // default 3
    MaxSessionHours         int           // default 2
    RecentSwitchWindow      time.Duration // default 2 * time.Minute
    Clock                   func() time.Time

    OnStart func(id string, startedAt time.Time)
    OnEnd   func(id string, startedAt, endedAt time.Time, reason string)

    mu               sync.Mutex
    activeID         string
    sessionStart     time.Time
    isActive         bool
    lastEventTime    time.Time
    lastBundleID     string
    appSwitchedAt    time.Time
    recentSwitches   []switchEntry // ring buffer, len <= 50
}

func (c *Cutter) OnEvent(occurredAt time.Time, bundleID string) { /* matches OC's on_event */ }
func (c *Cutter) CheckCuts()                                    { /* matches OC's check_cuts */ }
func (c *Cutter) ForceEnd(reason string) (id string, ok bool)   { /* matches OC's force_end */ }
```

Algorithm matches `session/manager.py` line-by-line: same lock discipline, same recent-switches deque, same frequent-switching predicate (≥2 distinct apps in window).

### Daemon wiring (in `ctxd`)

```go
cutter := session.NewCutter(cfg.Session)
cutter.OnStart = sessionStore.Open                       // persist active row locally
cutter.OnEnd   = func(id string, s, e time.Time, r string) {
    sessionStore.Close(id, e, r)
    enqueueClient.PutSession(ctx, makeSessionPayload(...))  // idempotent PUT to dpkms
}

ambientRunner.OnRawEvent = func(ev RawEvent) {
    cutter.OnEvent(ev.OccurredAt, ev.BundleID())          // cutter learns of activity
    ev.SessionID = cutter.ActiveID()                       // tag the event
    enqueueClient.PostEvent(ctx, ev)
}

// Periodic ticks
go cutter.RunTicker(ctx, cfg.Session.TickSeconds)         // check_cuts every 30s
go safetyNetCron(ctx, cutter, dailySafetyNetTime{Hour: 23, Min: 55})
```

Ticker + safety-net mirror OC's `run_check_cuts` and `run_daily_safety_net`.

### Replay semantics

If `ctxd` is buffering due to dpkms-down (per ADR-066's buffer):

- Session-opened events queue alongside RawEvents. On replay, the session PUT is idempotent (PUT semantics on session.id), so duplicate POSTs are no-ops.
- RawEvents tagged with `session_id` may arrive before or after the corresponding session row. Soft-FK on `objects.session_id` makes both orders OK.
- Session-closed events also queue; closing happens locally first (so the cutter advances), then replays to dpkms when network returns.

### Migration concerns

- **Existing KnowledgeObjects**: untouched. `session_id` is added as nullable/empty-default. No backfill.
- **dpkms must be on the new schema before a `ctxd` daemon enqueues with session_id populated.** Standard rolling-deploy: ship dpkms version N+1 with the schema change first, then ship `ctxt`/`ctxd` version N+1 that emits sessions. Old dpkms receiving a session_id field on a payload from new ctxt: silently ignores (forward-compat via JSON field omission).
- **Plugin API change**: announced in CHANGELOG; major-version bump on `pkg/pluginapi/` if we're versioning that package strictly. Otherwise documented as additive (backwards-compatible: new field, optional).

### Testing implications

- **Cutter tests** parallel OC's: idle gap → cut at last event time; soft cut + frequent-switching exception → no cut; timeout → cut at session_start + max_hours; force_end → cut at last event time; on_event after force_end → starts new session. Use a fake clock; no time.Sleep.
- **Storage tests**: insert session, insert KnowledgeObject with session_id, query both directions; soft-FK race (object before session) round-trips correctly.
- **Replay tests**: buffer 100 RawEvents + 1 session-opened + 1 session-closed across a dpkms-down window; on recovery, all 100 land with correct session_id, session row reflects the cutter's recorded end_reason.
- **Bus-event tests**: every cut path emits the expected `*.session.*` topic per the ADR-066 taxonomy extension above.
- **End-to-end smoke**: `ctxt capture --ambient --foreground` for 10+ minutes with simulated app switches via test harness; verify session boundary cut firing as expected; `ctxt session list` shows the resulting sessions; `ctxt compose --session <id>` returns only that session's items.

---

## Diagrams

Source-of-truth `.mmd` files live in [`../diagrams/ambient/`](../diagrams/ambient/). See [`../diagrams/README.md`](../diagrams/README.md) for authoring conventions.

### [Session lifecycle (state machine)](../diagrams/ambient/067-lifecycle.mmd)

In v1, sessions stop at the *active → ended* transition — there is no per-session reducer (deferred Phase 6+ per ADR-066 §Rationale 6). The `reduced` / `failed` / `retrying` states are reserved-but-unused.

### [Three-rule cutter (decision flow)](../diagrams/ambient/067-cutter-flowchart.mmd)

Runs every `tick_seconds` (default 30s) AND inline on every `OnEvent`. All times local. Lock-protected. Frequent-switching exception (≥2 distinct apps in last 2 min) suppresses the soft cut — required for IDE+terminal+browser workflows.

### [Replay sequence (network-loss + session boundary)](../diagrams/ambient/067-replay-sequence.mmd)

Demonstrates soft-FK behavior: events tagged with a session_id can arrive at dpkms before or after the corresponding session row, and storage absorbs both orders.

---

## References

- ADR-053 — KnowledgeObject as pipeline draft (target type the new SessionID extends)
- ADR-056 — unified enqueue API (the path session-id rides on)
- ADR-063 — graph-canonical KnowledgeObject (session is grouping, not a graph relationship — distinct concern)
- ADR-066 — ambient capture substrate (parent ADR; SessionID is populated by the cutter that runs in the substrate)
- ADR-068 *(planned)* — dpkms MCP read-surface (will expose session-scoped queries to agents)
- OpenChronicle prior art:
  - `~/.p/sandbox/OpenChronicle/src/openchronicle/session/manager.py` — three-rule cutter; ports verbatim to Go
  - `~/.p/sandbox/OpenChronicle/docs/session.md` — rationale, tuning table, post-mortem on per-capture vs. session-level writes
- OC v1→v2 post-mortem (`docs/session.md` lines 93–100): why per-capture writes failed and session-level fixed it
- tlc track: `ambient-capture` — `tlc track show ambient-capture` (T-0496 carries this ADR; T-0505 carries the implementation)
