# Workflow: Work Sessions

## Goal

Group ambient captures into bounded work units called **sessions** (a.k.a. WorkUnits) so you can query "what was I doing yesterday afternoon?" and get coherent answers — pre-meeting browser visits, the meeting itself, post-meeting file edits, and clipboard captures all under one ID.

## Scope

- Listing and inspecting sessions
- Composing per-session summaries
- Tuning the cutter (idle / soft-cut / timeout) for your workflow
- Cross-source queries (sessions span clipboard + browser + foreground + meetings)
- Manual session control (advanced)

## Primary stories

- `US-0216` (work sessions)

## Prerequisites

1. `ctxd` running with at least one ambient source active (see [ambient-capture.md](ambient-capture.md)).
2. Foreground-window source enabled (the cutter needs the foreground signal to detect app switches and idle gaps reliably).
3. dpkms reachable. Sessions persist server-side (with soft-FK so client-side network-loss replay works).

## How sessions are cut

Three rules, all running client-side in `ctxd`:

| Rule | Default | Description |
|---|---|---|
| **Idle (hard cut)** | `gap_minutes = 5` | No capture-worthy event for 5 min → session ends at last_event_time |
| **Soft cut** | `soft_cut_minutes = 3` | Single app held focus for 3 min AND user is NOT frequent-switching → session ends. Frequent-switching exception: ≥2 distinct apps in last 2 min → keep session active. |
| **Hard timeout** | `max_session_hours = 2` | Session > 2h regardless of activity → end |

Plus: `ctxd` shutdown force-ends, and a daily safety net at local 23:55 closes anything still open.

> See [`../../diagrams/ambient/067-cutter-flowchart.mmd`](../../diagrams/ambient/067-cutter-flowchart.mmd) for the full decision flow.

## Procedure

### Step 1: List recent sessions

```bash
ctxt session list
ctxt session list --since "yesterday"
ctxt session list --since "2026-04-28" --until "2026-04-30"
ctxt session list --profile work
```

Sample output:

```
ID                       Started        Ended          Reason     Events  App mix
sess_a1b2c3d4e5f6        14:32 today    15:38 today    soft_cut   34      zoom 67%, chrome 23%, slack 10%
sess_9z8y7x6w5v4u        09:15 today    11:42 today    timeout    127     vscode 45%, chrome 32%, terminal 23%
sess_3m2n1o0p9q8r        16:30 yesterday 17:45 yesterday idle      52      figma 78%, chrome 22%
```

### Step 2: Show a single session

```bash
ctxt session show sess_a1b2c3d4e5f6
```

Renders:
- Session metadata (start/end time, duration, end reason, app mix, source mix, profile)
- Ordered timeline of every KnowledgeObject captured during the session
- Inline references to meeting transcripts, file edits, browser visits, clipboard captures
- Mentions extracted across all items in the session

### Step 3: Tail the active session live

```bash
ctxt session tail
```

Streams every event joining the active session as it's captured. Useful during work for "what's ctxt actually picking up right now?"

### Step 4: Compose by session

```bash
ctxt compose --session sess_a1b2c3d4e5f6
ctxt compose --session sess_a1b2c3d4e5f6 --template meeting-recap
ctxt compose --session sess_a1b2c3d4e5f6 --template followup-actions
```

Or compose by time-range (auto-resolves to overlapping sessions):

```bash
ctxt compose --since "2 hours ago" --template work-summary
ctxt compose --since "yesterday afternoon" --until "yesterday evening"
```

Template registry is the same one used by other compose commands; sessions just give you a clean scope.

### Step 5: Tune the cutter

Edit `~/.config/contexthelp/config.yaml`:

```yaml
ambient:
  session:
    gap_minutes: 5              # idle cut threshold
    soft_cut_minutes: 3         # single-app-focus cut threshold
    max_session_hours: 2        # hard timeout
    recent_switch_window_minutes: 2  # frequent-switching exception window
    tick_seconds: 30            # how often the cutter checks for cuts when no events arrive
```

Restart `ctxd` to apply. Common tunings:

| Symptom | Knob to adjust |
|---|---|
| Sessions cut too eagerly during real focused work across multiple apps | `soft_cut_minutes`: 3 → 5 |
| Sessions don't cut soon enough after lunch / breaks | `gap_minutes`: 5 → 3 |
| Deep-work session got chopped at 2h | `max_session_hours`: 2 → 4 |
| Cutter feels laggy | `tick_seconds`: 30 → 10 (cost is negligible) |

### Step 6: Manual session control (advanced)

Force-end the active session (rare; daemon shutdown handles this automatically):

```bash
ctxt session end --reason "manual"
```

Manually tag a one-shot capture with a session_id (advanced; for replaying historical events):

```bash
ctxt analyze "..." --session-id sess_a1b2c3d4e5f6
```

## Common patterns

### "Recap my day"

```bash
ctxt session list --since today
# Pick the interesting ones, then:
ctxt compose --session <id> --template daily-recap
```

### "Pull together the entire investigation of bug #4521"

```bash
# If the work spanned several sessions:
ctxt session list --since "2 weeks ago" | grep -i "4521\|auth"
# Compose across multiple sessions:
ctxt compose --session sess_a,sess_b,sess_c --template investigation-summary
```

### "What apps am I spending session time in?"

```bash
ctxt session list --since "this week" --output json | \
  jq '[.[] | .app_mix | to_entries[] | {app: .key, sec: .value}] | group_by(.app) | map({app: .[0].app, total: ([.[].sec] | add)}) | sort_by(.total) | reverse'
```

### "Why is my session not cutting?"

```bash
ctxt capture --ambient tail --topic ctxt.ambient.session.cut_evaluated
```

Shows every cutter evaluation with the reason it kept the session active (frequent-switching detected, idle threshold not met, etc.).

## Outputs to validate

- Session has correct start/end times
- `end_reason` is one of: idle, soft_cut, timeout, shutdown, daily_safety_net
- `app_mix` adds to ~100%
- KnowledgeObjects in the session all have matching `session_id`
- `event_count` matches the count returned by `objects WHERE session_id=?`

## Common failure modes

### "Sessions never start"

- Check that the foreground source is active (`ctxt capture --ambient sources`)
- macOS: foreground requires AX permission (System Settings → Privacy & Security → Accessibility)
- Linux: foreground source is currently macOS-only (X11/Wayland tracked separately)

### "Session ID is empty on captured KnowledgeObjects"

- Pre-Phase-4 KnowledgeObjects (created before sessions migration) keep `session_id == ""` forever (no backfill in v1)
- KnowledgeObjects created via one-shot `ctxt analyze` with no active `ctxd` session also have empty session_id (no foreground signal → no cutter)

### "Session was cut at 5min idle but I was just watching a video"

- Watching a passive video produces no capture-worthy events; the cutter sees idle
- Either: lower expectations (the cutter is event-driven), or have a non-passive source running (browser-history will fire as you scroll)

### "Storage shows the session row missing but objects reference it"

- This is the soft-FK working as designed. Objects can arrive before the session row during network-loss replay. `dpkms session show <id>` shows nothing until the session row arrives; once it does, the objects are linked retroactively.

## Reference: session lifecycle

> See [`../../diagrams/ambient/067-lifecycle.mmd`](../../diagrams/ambient/067-lifecycle.mmd) for the state machine.

In v1, sessions stop at the `active → ended` transition. There is no per-session reducer (LLM summary). The schema reserves `flush_end` and `classified_end` columns for a future Phase 6+ reducer (paired with the deferred OpenChronicle timeline-aggregator pattern from ADR-066 §Rationale 6).

## Related references

- [`ambient-capture.md`](ambient-capture.md) — parent daemon
- [`meeting-capture.md`](meeting-capture.md) — meetings cluster naturally with their prep/follow-up
- [`mcp-agents.md`](mcp-agents.md) — `current_session()` and `session()` MCP tools for agent queries
- [`../../decisions/ADR-067-session-workunit.md`](../../decisions/ADR-067-session-workunit.md)
- [`../../diagrams/ambient/067-lifecycle.mmd`](../../diagrams/ambient/067-lifecycle.mmd)
- [`../../diagrams/ambient/067-cutter-flowchart.mmd`](../../diagrams/ambient/067-cutter-flowchart.mmd)
- [`../../diagrams/ambient/067-replay-sequence.mmd`](../../diagrams/ambient/067-replay-sequence.mmd)
