---
status: paper
adr: ADR-066, ADR-067
task: T-0503
---

# US-0215: Foreground Window / App Source

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/researchers-osint.md)

---

## User Goal

As a knowledge worker, I want `ctxd` to know which application has foreground focus and emit a low-volume event whenever it changes, so that work sessions ([ADR-067](../../decisions/ADR-067-session-workunit.md)) can be cut accurately and queries like "what apps did I use in the last 2 hours?" return useful answers.

---

## Context

The session cutter ([ADR-067](../../decisions/ADR-067-session-workunit.md)) needs the foreground-window signal to decide when to cut sessions (soft cut on single-app focus, frequent-switching exception, etc.). This source provides that signal. It also stands alone as a useful ambient source: knowing the time-distribution across apps over a day produces useful work-pattern data.

This is **macOS-only in v1**. macOS Accessibility API (`AXFocusedWindowChanged`, `AXApplicationActivated`) gives a clean signal. Linux X11/Wayland and Windows variants are tracked separately as future work — they require different mechanisms (X11 _NET_ACTIVE_WINDOW property, Windows `WinEventHook`, Wayland's lack of a stable cross-compositor API).

**Critical scope distinction from OpenChronicle:** this source emits **window-focus events only** — bundle ID, window title, app name, focus timestamp. It does NOT capture the AX tree, visible text, focused-element values, or keystrokes. ctxt captures *signals*, not *recordings* (per ADR-066 §Rationale 5).

---

## Acceptance Criteria

- [ ] Source registered as `foreground` in the ambient runner (macOS only; build-tag `darwin`)
- [ ] On macOS, hooks AX events: `AXFocusedWindowChanged`, `AXApplicationActivated`
- [ ] Emits RawEvent per focus change with: bundle ID, app name, window title, timestamp
- [ ] Debounce: collapse rapid focus-change bursts (e.g. window-cycler) to one event per 1 second
- [ ] **Does NOT** capture: AX tree contents, focused-element values, visible text, keystrokes
- [ ] Routes to a lightweight pipeline (or skips pipeline entirely; the event flows to the cutter directly with a stripped-down KO type if persisted)
- [ ] Configurable: which app metadata fields to capture (some users may want bundle-id only, no window titles)

```yaml
ambient:
  sources:
    foreground:
      enabled: true
      debounce_seconds: 1
      capture_window_title: true     # set false to capture only bundle_id
      exclude_bundles:
        - com.1password.macos
        - com.lastpass.LastPass
```

- [ ] kit/policy CEL veto on `ctxt.ambient.event.captured` for `source=foreground` can drop sensitive bundles before any further processing
- [ ] **First-run permission flow:** macOS Accessibility permission request + clear error + setup pointer if denied
- [ ] Bus events emit per ADR-066 taxonomy + ADR-067 session events (`session.opened`, `session.event_joined`, `session.closed`)
- [ ] Linux / Windows / non-darwin builds: source returns `ErrNotSupported`; daemon starts cleanly without it
- [ ] Linux X11/Wayland support tracked as separate future story
- [ ] Windows support tracked as separate future story

---

## Implementation Notes

### Architecture

```
internal/ambient/foreground/
├── foreground.go             — interface + dispatch
├── foreground_darwin.go      — AX event subscriber (cgo + Swift bridge)
├── foreground_unsupported.go — non-darwin: returns ErrNotSupported
└── resources/                 — Swift bridge code, compiled at build time
```

Mirrors the OS-split pattern of [ADR-069 meeting capture](../../decisions/ADR-069-meeting-capture-source.md). The Swift helper subscribes to the AX events and emits a JSON line per focus change to stdout; Go reads stdin and emits RawEvents.

### Privacy posture (deliberate scope reduction)

OpenChronicle's full AX-tree capture is rejected for ctxt — it's the wrong product. This source takes the minimum signal needed for session-cutting and useful work-pattern data:

```
Captured:    bundle_id, app_name, window_title (optional), timestamp
NOT captured: AX tree, focused-element values, visible text, keystrokes, screenshot
```

If a user wants AX-tree-style capture, they can install OpenChronicle directly; ctxt does not embed that scope.

### Cutter interaction

The cutter ([ADR-067](../../decisions/ADR-067-session-workunit.md)) consumes foreground events to track app-switching. Each foreground event triggers the cutter's `OnEvent(occurredAt, bundleID)` per ADR-067's pseudo-Go interface. The cutter then decides whether to:

- Open a new session (no active session)
- Update last_event_time + recent_switches (existing session)
- Trigger a cut (idle / soft / timeout per the 3-rule cutter)

### CLI

```bash
ctxt capture --ambient sources                          # shows foreground source state
ctxt capture --ambient tail --source foreground         # follow focus events live (useful for cutter tuning)
```

---

## E2E Checklist

- [ ] On macOS, `ctxt capture --ambient` prompts for AX permission on first run
- [ ] Grant permission; verify foreground source becomes active
- [ ] Switch between 3 apps rapidly; verify debounce collapses bursts
- [ ] Verify NO AX-tree contents in any RawEvent payload (only bundle_id + window_title + app_name + timestamp)
- [ ] Add 1Password to exclude_bundles; bring it to focus; verify no event emitted
- [ ] Verify session cutter receives the events: `ctxt session list --since today` shows sessions with `app_mix` derived from foreground events
- [ ] On Linux / Windows, daemon starts cleanly with foreground source disabled (`source=foreground: not supported on linux`)
- [ ] kit/policy CEL veto on bundle_id pattern works
- [ ] Bus events fire (ambient + session)
- [ ] Disable AX permission mid-session; verify graceful error + clear message

---

## Related Stories

- [US-0211](US-0211-passive-clipboard-watcher.md) — Sibling ambient source
- [US-0213](US-0213-file-watch-source.md) — Sibling
- [US-0214](US-0214-browser-history-source.md) — Sibling
- [US-0216](US-0216-work-sessions.md) — Sessions consume this source's events for cutting; this is the dependency story
- [US-0217](US-0217-meeting-capture-desktop.md) — Meeting auto-detect (Phase 5) consumes foreground bundle-id events

---

## Sprint

**Skeleton 9.5 — Ambient Capture + Sessions + Agent-Native MCP**
Implements ADR-066 Phase 3 (foreground-window source) — see `tlc track show ambient-capture`, task **T-0503**.

---

## E2E Tests

- planned: `test/integration/us0215_foreground_test.go::TestForeground_AXPermissionFlow`
- planned: `test/integration/us0215_foreground_test.go::TestForeground_DebounceBursts`
- planned: `test/integration/us0215_foreground_test.go::TestForeground_NoAXTreeCaptured`
- planned: `test/integration/us0215_foreground_test.go::TestForeground_BundleExcludeList`
- planned: `test/integration/us0215_foreground_test.go::TestForeground_FeedsCutter`
- planned: `test/integration/us0215_foreground_test.go::TestForeground_NotSupportedOnNonDarwin`
