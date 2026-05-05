---
status: paper
adr: ADR-066
task: T-0504
---

# US-0212: Screenshot-on-Demand Source

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Researchers & OSINT Analysts](../../personas/researchers-osint.md)

> **Re-grounded in ADR-066** (2026-05-05). The original draft proposed **continuous periodic screen capture (every 30s)**. ADR-066 §Rationale 5 explicitly rejects continuous screen-memory capture: ctxt captures *signals*, not *recordings*. This story is re-scoped to **screenshot-on-demand**: explicit hotkey or scheduled trigger only, never continuous. Implementation task: **T-0504** (Source: screenshot-on-demand → OCR).
>
> If the user wants live screen-memory recording (the OpenChronicle model), that's a separate product surface and a separate ADR — not v1 scope.

---

## User Goal

As a knowledge worker, I want a hotkey (or schedule) that captures my screen, runs the result through OCR, and stores the structured output in my knowledge graph so that information I view but never explicitly save can be made findable on demand.

For meeting capture (recording video calls with audio + video → transcript + frame OCR), see US-0217 (which depends on a separate substrate per [ADR-069](../../decisions/ADR-069-meeting-capture-source.md)).

---

## Context

A significant portion of valuable information is consumed visually but never captured: terminal output that scrolls away, Figma designs reviewed briefly, dashboards glanced at during standup, paper book pages photographed, whiteboard contents at the end of a meeting. The clipboard watcher (US-0211) only captures what the user explicitly copies. The screenshot source captures what the user **explicitly screenshots** for ingestion.

The mechanism is straightforward: explicit trigger → screenshot → `image.ocr` pipeline → structured KnowledgeObject. The OCR output flows through the same entity extraction, mention resolution, and tagging as any other ingested content. Sessions ([ADR-067](../../decisions/ADR-067-session-workunit.md)) group the screenshot with surrounding work.

**Privacy by design:** explicit-trigger only. No continuous capture, no periodic polling without an explicit user-set schedule, no auto-detection. The first time the user invokes the source on each OS, a permission prompt appears. kit/policy CEL rules can veto sensitive contexts (bundle-id deny-list) before the OS permission prompt fires.

---

## Acceptance Criteria

- [ ] Source registered as `screenshot` in the ambient runner (per ADR-066 §Phase 3)
- [ ] **Explicit hotkey trigger:** configurable hotkey (default ⇧⌘S / Ctrl+Shift+S) captures the active screen / window and routes to `image.ocr`
- [ ] **CLI trigger:** `ctxt capture screenshot [--window-title TITLE] [--label "..."]`
- [ ] **Optional schedule trigger** (opt-in, off by default): `ambient.sources.screenshot.schedule.enabled = true` with a configurable interval (must be ≥ 60s; no sub-minute schedules) — e.g. for whiteboard kiosks or status-display capture
- [ ] First-use OS permission prompt; absence produces a clear error + setup pointer (per OS)
- [ ] kit/policy CEL veto on `ctxt.ambient.event.captured` for `source=screenshot` fires **before** OS permission prompt
- [ ] Capture target options:
    - Active window (default)
    - Full screen
    - User-selected region (via OS-native picker)
    - Specific window-title substring (CLI / config)
- [ ] Capture writes a temporary file under `$XDG_STATE_HOME/ctxt/ambient/media/screenshots/`; OCR pipeline result becomes the durable KnowledgeObject; raw image evicted per `screenshot.media_retention_hours` (default 24h)
- [ ] Per-app deny-list: `ambient.sources.screenshot.exclude_bundles` (default includes 1Password, Keychain Access, system Keychain, password manager bundles)
- [ ] Bus events per ADR-066 taxonomy: `ctxt.ambient.event.captured`, `…filtered`, `…enqueued`; plus `ctxt.ambient.screenshot.requested` / `…permission_*` for OS prompt flow
- [ ] Resulting KnowledgeObject carries `ambient_source=screenshot`, `session_id`, fingerprint (perceptual hash of the image)
- [ ] OCR text length minimum (`min_ocr_length = 80`) silently skips empty captures
- [ ] Works on macOS 13+; Windows 10+; Linux (Wayland portal first, X11 fallback)

---

## Implementation Notes

### Architecture

```
internal/ambient/screenshot/screenshot.go
  implements ambient.AmbientSource

  Trigger paths:
    1. Hotkey listener (per OS):
         macOS: NSEvent global monitor
         Windows: RegisterHotKey
         Linux: portal-mediated (Wayland) or XGrabKey (X11)
    2. CLI: `ctxt capture screenshot ...` → gRPC to ctxd
    3. Schedule (opt-in): time.Ticker with min 60s interval

  Capture path (per OS):
    macOS: ScreenCaptureKit (single-frame mode) — same dependency as ADR-069 meeting capture
    Windows: Graphics Capture API (single-frame)
    Linux: xdg-desktop-portal Screenshot API (Wayland) / XGetImage (X11)

  Output:
    -> write tmp file under media/screenshots/
    -> emit RawEvent{Source: "screenshot", Kind: "image", Payload: filepath, SuggestedPipeline: "image.ocr"}
    -> runner: redact (none for image) -> CEL filter -> dedup (perceptual hash) -> session-tag -> buffer -> enqueue
```

The screenshot source is a thin shim that implements `AmbientSource` (per ADR-066 §Decision item 2). Captures land on disk first; the substrate enqueues against `image.ocr` which already exists in `internal/pipeline/builtins/image_ocr.go`.

### Deduplication

Handled by the substrate. Perceptual hash (dHash, 8x8 grid; Hamming distance > 10 of 64 bits triggers as new) is the fingerprint for `screenshot` events. Identical-looking captures within the dedup window collapse.

### Privacy

- **Explicit-trigger only** by default. Schedule mode is opt-in with a minimum 60s interval to prevent abuse.
- Bundle-id deny-list runs **before** screenshot capture; matching apps cause source to no-op without OS permission prompt
- kit/policy CEL veto runs at `ctxt.ambient.event.captured` and is the structural enforcement layer
- Raw image files evicted after `screenshot.media_retention_hours` (default 24h); only OCR text persists durably
- macOS recording-indicator is shown by the OS during capture (orange/purple dot in menubar) — not suppressible

### Auto-detect (NOT in v1)

The original draft proposed continuous capture with perceptual-hash filtering. Per ADR-066 §Rationale 5, this is rejected. If a future story needs always-on screen memory, it gets its own ADR and substrate (matches OpenChronicle's model, separate scope).

### CLI

```bash
# Hotkey: ⇧⌘S (configurable)
# OR explicit:
ctxt capture screenshot                                  # active window
ctxt capture screenshot --full-screen
ctxt capture screenshot --window-title "Figma"
ctxt capture screenshot --region                          # OS-native region picker
ctxt capture screenshot --label "Q3 dashboard snapshot"

# Verify
ctxt capture --ambient tail --source screenshot
ctxt list --ambient-source screenshot --since today
```

---

## E2E Checklist

- [ ] Press hotkey ⇧⌘S; verify capture event in `ctxt capture --ambient tail --source screenshot`
- [ ] Verify object created via `image.ocr`, with OCR text content, `ambient_source=screenshot`, `session_id` populated
- [ ] Trigger via CLI; verify same path
- [ ] Add `com.1password.*` to exclude_bundles; bring 1Password to foreground; press hotkey; verify no capture (CEL veto OR bundle deny)
- [ ] On first run, verify OS permission prompt; deny → graceful error + setup pointer
- [ ] Take 5 screenshots of the same screen in quick succession; verify dedup keeps only one (perceptual hash)
- [ ] Capture an empty desktop; verify silently skipped (OCR < min_ocr_length)
- [ ] Verify raw image file deleted after media_retention_hours; OCR'd KnowledgeObject persists
- [ ] Bus events fire for the documented topics

---

## Related Stories

- [US-0003](../ingestion/US-0003-image-ocr-and-analysis.md) — Image OCR pipeline (this source drives it)
- [US-0211](US-0211-passive-clipboard-watcher.md) — Passive clipboard watcher (sibling ambient source)
- [US-0213](US-0213-file-watch-source.md) — File-watch source (sibling)
- [US-0215](US-0215-foreground-window-source.md) — Foreground-window source (sibling)
- [US-0216](US-0216-work-sessions.md) — Work sessions (groups screenshot captures)
- [US-0217](US-0217-meeting-capture-desktop.md) — Meeting capture (different substrate, ADR-069)

---

## Sprint

**Skeleton 9.5 — Ambient Capture + Sessions + Agent-Native MCP**
Implements ADR-066 Phase 3e (screenshot-on-demand source) — see `tlc track show ambient-capture`, task **T-0504**.

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers & OSINT Analysts](../../personas/researchers-osint.md)

---

## E2E Tests

- planned: `test/integration/us0212_screenshot_test.go::TestScreenshot_HotkeyTrigger`
- planned: `test/integration/us0212_screenshot_test.go::TestScreenshot_CLITrigger`
- planned: `test/integration/us0212_screenshot_test.go::TestScreenshot_RoutesToImageOCR`
- planned: `test/integration/us0212_screenshot_test.go::TestScreenshot_BundleDenyList`
- planned: `test/integration/us0212_screenshot_test.go::TestScreenshot_CELVeto`
- planned: `test/integration/us0212_screenshot_test.go::TestScreenshot_PerceptualHashDedup`
- planned: `test/integration/us0212_screenshot_test.go::TestScreenshot_PermissionFlow`
- planned: `test/integration/us0212_screenshot_test.go::TestScreenshot_MediaRetentionEviction`
