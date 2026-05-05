---
status: paper
adr: ADR-069
task: T-0517
---

# US-0221: Meeting Auto-Detect Prompt (Opt-In)

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker who often forgets to start meeting recording, I want an opt-in feature that detects when a known meeting app comes to the foreground (Zoom, Teams, Meet, FaceTime, Discord, Slack Huddles) and **prompts** me to start recording — without ever starting the recording silently.

---

## Context

Tools like Otter and Granola default to **always-on auto-record**. ctxt rejects that posture per [ADR-069 §Rationale 1](../../decisions/ADR-069-meeting-capture-source.md): consent-law landscape varies by jurisdiction; always-on is a privacy hazard for the participants who didn't consent.

The middle ground: detect known meeting bundle-ids via the existing foreground-window source ([US-0215](US-0215-foreground-window-source.md)), emit a **prompt event** (OS notification + hotkey), and require explicit user confirmation before recording starts. Default 10-second confirmation window; auto-dismiss if no action.

Per-app remember-my-choice via kit/policy CEL rule lets users opt into "always start when Zoom is focused" without making it the global default. Other apps still prompt.

---

## Acceptance Criteria

### Detection

- [ ] Meeting source subscribes to `ctxt.ambient.foreground.changed` (or equivalent foreground source event)
- [ ] When foreground bundle-id matches the auto-detect list, emit `ctxt.ambient.meeting.auto_detected`
- [ ] Default detection list (configurable):
    - `us.zoom.xos` (Zoom)
    - `com.microsoft.teams2` (Microsoft Teams)
    - `com.tinyspeck.slackmacgap` (Slack — for Huddles)
    - `com.electron.discord` (Discord)
    - Browser tab on `meet.google.com` (via browser-history source heuristic)
    - Browser tab on `*.zoom.us/j/*`
    - macOS FaceTime: `com.apple.FaceTime`
- [ ] User can extend list via config

### Prompt

- [ ] OS-native notification with title "Record this meeting?" and label inferred from window title
- [ ] Hotkey to confirm (configurable; default ⌃M / Ctrl+M for confirm; ESC to dismiss)
- [ ] Configurable timeout: `ambient.meeting.auto_detect.prompt_timeout` (default 10s)
- [ ] Auto-dismiss if no action; emit `ctxt.ambient.meeting.prompt_dismissed`
- [ ] Confirmation triggers the same CLI path as `ctxt capture meeting start --label "..."` (label inferred from window title)
- [ ] Audio-only vs full mode: configurable per detected bundle (default: full for Zoom/Meet, audio-only for Slack Huddles)

### Per-app remember-my-choice (CEL-based)

- [ ] User can persist a choice for a specific bundle:
    - "Always start when Zoom is focused" — auto-confirms without prompt
    - "Never auto-prompt for Discord" — silently skips detection
- [ ] Choices stored as kit/policy CEL rules:

```yaml
# policy.d/auto-record-zoom.cel
match: ctxt.ambient.meeting.auto_detected
where: event.bundle_id == "us.zoom.xos"
action: invoke
target: ctxt.ambient.meeting.start
params:
  mode: full
  label_from: window_title
```

- [ ] CLI: `ctxt capture meeting auto-detect remember --bundle <id> --action [auto-start|skip|prompt]` writes the rule
- [ ] CLI: `ctxt capture meeting auto-detect rules` lists current per-app rules
- [ ] CLI: `ctxt capture meeting auto-detect forget --bundle <id>` removes a rule

### Config

```yaml
ambient:
  meeting:
    auto_detect:
      enabled: false                  # OFF by default; explicit opt-in
      prompt_timeout: 10s
      bundles:
        - id: us.zoom.xos
          mode: full
        - id: com.microsoft.teams2
          mode: full
        - id: com.tinyspeck.slackmacgap
          mode: audio_only
      browser_url_patterns:
        - "https://meet.google.com/*"
        - "https://*.zoom.us/j/*"
```

### Bus events

- [ ] `ctxt.ambient.meeting.auto_detected` (foreground bundle matched)
- [ ] `ctxt.ambient.meeting.prompt_dismissed` (user dismissed or auto-timeout)
- [ ] `ctxt.ambient.meeting.requested` (user confirmed; same path as explicit start)
- [ ] All downstream meeting events from [US-0217](US-0217-meeting-capture-desktop.md) follow

### Privacy posture

- [ ] **NEVER** starts recording without explicit confirmation OR explicit per-app rule
- [ ] Auto-detect is **opt-in** (`enabled: false` by default)
- [ ] Veto rules can suppress auto-detect entirely for sensitive contexts (e.g. while in `profile=personal`)
- [ ] Bundles in `meeting.exclude_bundles` are never detected (deny takes precedence over auto-detect)

---

## Implementation Notes

### Architecture

```
internal/ambient/meeting/
└── auto_detect.go       — subscribes to foreground events; emits auto_detected; manages prompt UI
```

Reuses the foreground source's bundle-id signal; no new OS hooks.

### Prompt UI per OS

- **macOS**: `UNUserNotificationCenter` (User Notifications framework) for the prompt; global hotkey via `NSEvent` global monitor
- **Windows**: `Toast Notifications` API; global hotkey via `RegisterHotKey`
- **Linux**: notify-send / portal-mediated notification; hotkey via portal or compositor-specific binding

### Browser tab detection

The foreground source emits `bundle_id="com.google.Chrome"` when Chrome is focused. To detect a Google Meet tab, we additionally consult the browser-history source's most recent visit — if it matches `meet.google.com` and was within 30s, treat as Meet meeting active.

This composition reuses [US-0214](US-0214-browser-history-source.md)'s output rather than building a separate "browser tab" source.

### CLI integration

```bash
# Enable
ctxt capture meeting auto-detect enable

# Per-app rules
ctxt capture meeting auto-detect remember --bundle us.zoom.xos --action auto-start --mode full
ctxt capture meeting auto-detect remember --bundle com.electron.discord --action skip
ctxt capture meeting auto-detect rules
ctxt capture meeting auto-detect forget --bundle us.zoom.xos
```

---

## E2E Checklist

- [ ] Enable auto-detect in config; restart ctxd
- [ ] Bring Zoom to foreground; verify prompt notification appears within 1s
- [ ] Press confirm hotkey; verify recording starts via [US-0217](US-0217-meeting-capture-desktop.md)'s path with label from window title
- [ ] Bring Zoom to foreground again; let prompt timeout (10s); verify `ctxt.ambient.meeting.prompt_dismissed` fires; no recording
- [ ] Add `auto-start` rule for Zoom; bring Zoom to foreground; verify recording starts WITHOUT prompt (CEL rule auto-confirms)
- [ ] Add `skip` rule for Discord; bring Discord to foreground; verify NO prompt, NO recording
- [ ] Open Google Meet in Chrome; verify prompt appears (browser-history source composition works)
- [ ] Disable auto-detect; bring Zoom to foreground; verify NO prompt
- [ ] In `profile=personal`, configure veto on auto-detect; bring Zoom to foreground; verify suppressed
- [ ] Bundle in `meeting.exclude_bundles`; verify auto-detect ignores it (deny takes precedence)
- [ ] All bus events fire as documented

---

## Related Stories

- [US-0215](US-0215-foreground-window-source.md) — Foreground source provides the bundle-id signal
- [US-0214](US-0214-browser-history-source.md) — Browser-history source provides URL-based detection
- [US-0217](US-0217-meeting-capture-desktop.md) — Meeting capture (this story is the auto-start trigger)
- [US-0218](US-0218-meeting-redact-export.md) — Redact + export (consequence: auto-detected recordings can be redacted post-hoc)

---

## Sprint

**Skeleton 9.5 — Ambient Capture + Sessions + Agent-Native MCP**
Implements ADR-069 Phase 5 (auto-detect prompt) — see `tlc track show ambient-capture`, task **T-0517**.

---

## E2E Tests

- planned: `test/integration/us0221_auto_detect_test.go::TestAutoDetect_PromptOnZoom`
- planned: `test/integration/us0221_auto_detect_test.go::TestAutoDetect_PromptTimeout`
- planned: `test/integration/us0221_auto_detect_test.go::TestAutoDetect_HotkeyConfirm`
- planned: `test/integration/us0221_auto_detect_test.go::TestAutoDetect_RememberAutoStart`
- planned: `test/integration/us0221_auto_detect_test.go::TestAutoDetect_RememberSkip`
- planned: `test/integration/us0221_auto_detect_test.go::TestAutoDetect_BrowserURLDetection`
- planned: `test/integration/us0221_auto_detect_test.go::TestAutoDetect_DisabledByDefault`
- planned: `test/integration/us0221_auto_detect_test.go::TestAutoDetect_VetoSuppressionInProfile`
- planned: `test/integration/us0221_auto_detect_test.go::TestAutoDetect_ExcludeBundlesTakesPrecedence`
