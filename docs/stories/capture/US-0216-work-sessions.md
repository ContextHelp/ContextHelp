---
status: paper
adr: ADR-067
task: T-0505
---

# US-0216: Work Sessions (Temporal Grouping of Captures)

**System Types:** ctxt, dpkms
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/researchers-osint.md), [AI Agents](../../personas/agents-llms-tools.md)

---

## User Goal

As a knowledge worker, I want my ambient captures (clipboard, browser visits, foreground apps, screenshots, meetings, file drops) automatically grouped into bounded **work sessions** — so that "what was I doing yesterday afternoon?" returns one cohesive view instead of a noisy stream of independent events.

---

## Context

Without sessions, ambient capture produces a flood of small KnowledgeObjects with no temporal structure. The user drowns in dust. Sessions group those captures into human-scale work units (typical 30 min – 2 hours) using a 3-rule cutter ported from OpenChronicle.

Sessions are **client-side concepts** — the cutter runs in `ctxd` because the foreground-window signal it depends on is local. dpkms persists sessions as opaque metadata (per [ADR-067](../../decisions/ADR-067-session-workunit.md)), with soft-FK semantics so network-loss replay handles arrival ordering correctly.

In v1, sessions are **grouping-only**: no LLM-driven per-session summary. The schema reserves columns (`flush_end`, `classified_end`) for a Phase 6+ reducer (paired with the deferred timeline-aggregator pattern from ADR-066 §Rationale 6).

---

## Acceptance Criteria

### Type + storage

- [ ] `Session` and `AppShare` types added to `pkg/pluginapi/` (stable plugin API)
- [ ] `SessionID string` field added to `KnowledgeObject` (optional; soft-FK)
- [ ] `sessions` table created via migration (SQLite + Postgres parity)
- [ ] `objects.session_id` column added; soft-FK index
- [ ] Session ID format: `sess_<12-hex>` (matches OpenChronicle convention)

### Cutter (client-side, in ctxd)

- [ ] **Hard cut (idle):** `gap_minutes` (default 5) of no events → end at last_event_time, reason=`idle`
- [ ] **Soft cut (single-app):** one app held focus for `soft_cut_minutes` (default 3) AND not frequent-switching (≥2 distinct apps in last 2 min) → end, reason=`soft_cut`
- [ ] **Hard timeout:** session > `max_session_hours` (default 2) → end, reason=`timeout`
- [ ] **Force end:** ctxd shutdown → end, reason=`shutdown`
- [ ] **Daily safety net:** at local 23:55, force-end any open session, reason=`daily_safety_net`
- [ ] Cut tick (`tick_seconds = 30`) fires the cutter even when no events arrive
- [ ] Cutter algorithm matches OC's `session/manager.py` line-by-line (lock discipline, recent-switches deque, frequent-switching predicate)

### Wire shape

- [ ] `PUT /api/v1/sessions/{id}` (idempotent) on session open AND close
- [ ] `POST /api/v1/analyze` accepts new optional fields: `session_id`, `ambient_source`, `fingerprint` (per ADR-066/067)
- [ ] dpkms's existing post-pipeline ContentHash dedup preserved as safety net

### Bus events

- [ ] `ctxt.ambient.session.opened` on cutter start
- [ ] `ctxt.ambient.session.closed` with `end_reason` payload
- [ ] `ctxt.ambient.session.event_joined` per RawEvent tagged with active SessionID
- [ ] `ctxt.ambient.session.cut_evaluated` per cutter tick (useful for tuning)

### CLI

- [ ] `ctxt session list [--since X --until Y --profile P]` returns session metadata
- [ ] `ctxt session show <id>` renders session timeline + every KO captured during it
- [ ] `ctxt session tail` follows the active session live (events joining as they happen)
- [ ] `ctxt compose --session <id>` scopes compose templates to one session
- [ ] `ctxt compose --since "2 hours ago"` auto-resolves to overlapping sessions
- [ ] `ctxt analyze --session-id <id>` (advanced) tags a manual ingest with a specific session

### Replay safety (soft-FK)

- [ ] RawEvents tagged with `session_id` whose row hasn't arrived yet are accepted by dpkms; objects link retroactively when session row arrives
- [ ] `PUT /api/v1/sessions/{id}` is idempotent across replay
- [ ] Network-loss replay scenario: 100 events + open + close all arrive in order → all 100 KOs link correctly

### Tuning surface

- [ ] All cutter constants configurable via `ambient.session.*` in config
- [ ] Tuning table from [ADR-067 §Implementation Notes](../../decisions/ADR-067-session-workunit.md) documented in user-facing manual

---

## Implementation Notes

### Cutter (Go port of OC)

Algorithm matches `session/manager.py` line-by-line. See [ADR-067 §Implementation Notes](../../decisions/ADR-067-session-workunit.md) for the full pseudo-Go interface.

### Daemon wiring

```go
cutter := session.NewCutter(cfg.Session)
cutter.OnStart = sessionStore.Open
cutter.OnEnd   = func(id string, s, e time.Time, r string) {
    sessionStore.Close(id, e, r)
    enqueueClient.PutSession(ctx, makeSessionPayload(...))
}
ambientRunner.OnRawEvent = func(ev RawEvent) {
    cutter.OnEvent(ev.OccurredAt, ev.BundleID())
    ev.SessionID = cutter.ActiveID()
    enqueueClient.PostEvent(ctx, ev)
}
```

### Storage shape

```sql
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    started_at TEXT NOT NULL,
    ended_at TEXT,
    end_reason TEXT,
    app_mix TEXT,           -- JSON
    event_count INTEGER NOT NULL DEFAULT 0,
    source_mix TEXT,        -- JSON
    profile_id TEXT,
    metadata TEXT,
    flush_end TEXT,         -- reserved Phase 6+
    classified_end TEXT,    -- reserved Phase 6+
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_sessions_started_at ON sessions(started_at);
ALTER TABLE objects ADD COLUMN session_id TEXT;
CREATE INDEX idx_objects_session ON objects(session_id) WHERE session_id IS NOT NULL;
```

### CLI rendering

`ctxt session show` produces a Markdown-style timeline:

```
Session sess_a1b2c3d4e5f6
  Started: 2026-05-05 14:32:18  (sess_a1b2c3d4e5f6)
  Ended:   2026-05-05 15:38:42  (66 min, soft_cut)
  Profile: work
  App mix: zoom 67%, chrome 23%, slack 10%
  Sources: clipboard, browser, foreground, meeting

Items (34):
  14:32  foreground   Switched to Zoom
  14:33  meeting      Recording started: "Q3 planning"
  14:33  clipboard    https://docs.example/q3-roadmap.md (url.generic)
  ...
  15:38  meeting      Stopped — transcript_ready (KO obj_xyz)
```

---

## E2E Checklist

- [ ] Run ctxd for 30 min with foreground + clipboard sources active
- [ ] Idle for 6 min; verify session cuts with reason=idle
- [ ] Stay focused on one app for 4 min while frequent-switching=false; verify soft_cut
- [ ] Frequent-switch (≥2 apps in 2 min) for 5 min on one app; verify session NOT cut (frequent-switching exception)
- [ ] Run continuously for 2h+; verify timeout cut
- [ ] Kill ctxd cleanly; verify force_end fires; session row in dpkms shows reason=shutdown
- [ ] At 23:55 local, verify daily safety net fires
- [ ] Take dpkms offline; capture 50 events; bring dpkms back; verify all 50 KOs have correct session_id (soft-FK works in both arrival orders)
- [ ] `ctxt session list --since today` shows expected sessions with correct end_reason
- [ ] `ctxt compose --session <id>` returns content from only that session's KOs
- [ ] Plugin reads `KnowledgeObject.SessionID`; backwards-compat (empty string for pre-Phase-4 KOs) works
- [ ] All bus events from ADR-067 §Decision fire

---

## Related Stories

- [US-0211](US-0211-passive-clipboard-watcher.md), [US-0212](US-0212-screen-monitor.md), [US-0213](US-0213-file-watch-source.md), [US-0214](US-0214-browser-history-source.md), [US-0215](US-0215-foreground-window-source.md) — Sources whose RawEvents get session_id-tagged
- [US-0217](US-0217-meeting-capture-desktop.md) — Meetings naturally bundle with prep/follow-up captures into one session
- [US-0219](US-0219-mcp-agent-integration.md) — MCP `sessions()` and `session()` tools query this
- [US-0220](US-0220-local-mcp-during-network-loss.md) — Local MCP `current_session()` surfaces the live cutter state

---

## Sprint

**Skeleton 9.5 — Ambient Capture + Sessions + Agent-Native MCP**
Implements ADR-067 — see `tlc track show ambient-capture`, task **T-0505**.

---

## E2E Tests

- planned: `test/integration/us0216_sessions_test.go::TestCutter_IdleHardCut`
- planned: `test/integration/us0216_sessions_test.go::TestCutter_SoftCutWithFrequentSwitchingException`
- planned: `test/integration/us0216_sessions_test.go::TestCutter_HardTimeout`
- planned: `test/integration/us0216_sessions_test.go::TestCutter_ForceEndOnShutdown`
- planned: `test/integration/us0216_sessions_test.go::TestCutter_DailySafetyNet`
- planned: `test/integration/us0216_sessions_test.go::TestSession_SoftFKReplay`
- planned: `test/integration/us0216_sessions_test.go::TestSession_PutIdempotent`
- planned: `test/integration/us0216_sessions_test.go::TestComposeBySession`
- planned: `test/integration/us0216_sessions_test.go::TestSessionListAndShow`
