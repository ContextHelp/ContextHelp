# US-0212: Screen Monitor

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Researchers & OSINT Analysts](../../personas/researchers-osint.md)

---

## User Goal

As a knowledge worker, I want ctxt to periodically capture my screen and extract any
meaningful content via OCR so that information I view but never explicitly save is still
findable in my knowledge base.

---

## Context

A significant portion of valuable information is consumed visually but never captured:
terminal output that scrolls away, Figma designs reviewed briefly, Slack threads scanned
and closed, dashboards glanced at during standup. The clipboard watcher (US-0211) only
captures what the user explicitly copies. The screen monitor captures what the user merely
looks at.

The mechanism is straightforward: periodic screenshot → `image.ocr` pipeline → structured
knowledge object. The OCR output is then subject to the same entity extraction, mention
resolution, and tagging as any other ingested content.

Noise is the primary risk. Most screen content is ephemeral: mouse pointers, chrome UI,
transient notifications, duplicate frames. The watcher must deduplicate aggressively
(perceptual hash + text hash), apply a minimum meaningful-content threshold, and allow
the user to configure focus regions and exclusion patterns (e.g., skip browser chrome,
skip system menubar).

Privacy is critical. Screen capture is the most sensitive watcher type. It must be
opt-in, clearly documented, and must never run without explicit user consent. On macOS,
Screen Recording permission must be requested and its absence must fail gracefully.

---

## Acceptance Criteria

- [ ] Screen monitor is disabled by default; requires explicit config opt-in
- [ ] `ctxt watcher enable screen` requires user confirmation (interactive prompt)
- [ ] Monitor captures screenshots at configurable interval (default: 30s)
- [ ] Each screenshot is processed through the `image.ocr` pipeline
- [ ] Perceptual hash deduplication skips frames with < 5% visual change from last capture
- [ ] Text hash deduplication skips frames whose OCR output matches last ingested text
- [ ] Minimum OCR text length threshold (default: 120 chars) filters empty/noise frames
- [ ] Jobs annotated with `origin=watcher/screen`
- [ ] Config supports focus regions (crop to active window or defined rect) and interval:

```yaml
watchers:
  screen:
    enabled: false
    interval: 30s
    min_ocr_length: 120
    focus: active_window   # active_window | full | {x,y,w,h}
    exclude_apps:
      - 1Password
      - Keychain Access
```

- [ ] `exclude_apps` list prevents capture when specified apps are frontmost (macOS)
- [ ] On macOS, Screen Recording permission absence produces a clear error + setup guide
- [ ] Raw screenshots are never persisted to disk — only OCR text and metadata
- [ ] `ctxt watcher status` shows screen monitor state, last trigger, and jobs enqueued
- [ ] Works on macOS; Linux support documented as best-effort (X11/Wayland variance noted)

---

## Implementation Notes

### Architecture

```
ScreenMonitor (ticks every 30s)
  -> capture screenshot (active window or full screen)
  -> phash(frame) diff > threshold?
    -> submit to image.ocr pipeline
      -> ocr_text length >= min_ocr_length?
        -> hash(ocr_text) != lastTextHash?
          -> enqueue job
          -> store lastTextHash
      -> discard otherwise
```

Implements the `Watcher` interface (Sk8 Task 2.1). Screenshot capture via OS APIs:
- macOS: `screencapture` CLI or `CGWindowListCreateImage` via cgo
- Linux: `scrot` / `import` (ImageMagick) as subprocess fallback

### Perceptual Hash

Use dHash (difference hash, 8x8 grid). Threshold: Hamming distance > 10 of 64 bits (~15%)
triggers re-evaluation. This tolerates minor cursor movement and clock changes.

### Active Window Focus (macOS)

```
frontmost app = NSWorkspace.sharedWorkspace.frontmostApplication
if frontmost in exclude_apps -> skip
capture CGWindowID of frontmost window -> crop screenshot to window bounds
```

### Privacy Guarantees

- Screenshots held in memory only; never written to `data/` or temp files
- OCR text stored as knowledge object content (same as any other ingested text)
- `CTXT_NO_SCREEN=1` env var disables unconditionally
- Log lines emit: `[screen-watcher] frame captured, ocr_length=347, enqueued=true`
  — never raw text

### CLI

```bash
# Enable (prompts for confirmation)
ctxt watcher enable screen
# -> Screen monitoring captures your display periodically. Continue? [y/N]

# Start
ctxt watcher start

# Status
ctxt watcher status
# ->
# Watcher     Status    Interval  Last Trigger              Jobs Enqueued
# clipboard   running   2s        2026-03-25T14:32:01Z      47
# screen      running   30s       2026-03-25T14:31:58Z      12
```

---

## E2E Checklist

- [ ] Enable screen watcher; verify confirmation prompt appears
- [ ] Start watcher; open a text-heavy window; wait 30s
- [ ] Verify job appears in `ctxt jobs` with `origin=watcher/screen`
- [ ] Verify resulting object has OCR text content
- [ ] Cover screen / switch to empty desktop; wait 30s; verify no job (noise filter)
- [ ] Add app to `exclude_apps`; bring it to foreground; verify no capture
- [ ] Stop watcher; verify no further jobs
- [ ] Verify no screenshot files in `data/` or `/tmp/ctxt*`
- [ ] On macOS: revoke Screen Recording permission; verify graceful error message

---

## Related Stories

- [US-0003](../ingestion/US-0003-image-ocr-and-analysis.md) — Image OCR pipeline
  (this story drives it passively)
- [US-0211](US-0211-passive-clipboard-watcher.md) — Passive clipboard watcher (sibling)
- [US-0208](US-0208-temporal-watch.md) — Temporal watch

---

## Sprint

**Skeleton 8 — Trust & Automation**
New Task 2.5 (Screen Monitor Watcher). Depends on Task 2.1 (Watcher Interface) and
the `image.ocr` pipeline from Skeleton 4.

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Solo Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/solo-developer.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

> Not yet implemented.
