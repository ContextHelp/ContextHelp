---
status: paper
adr: ADR-069
task: T-0513, T-0514, T-0515, T-0516, T-0519
---

# US-0217: Meeting Capture (Desktop, Audio + Video)

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/researchers-osint.md)

---

## User Goal

As a knowledge worker, I want to record video calls (Zoom, Meet, Teams, FaceTime, Discord, Slack Huddles) on my desktop with one hotkey, capturing both system audio (the participants) and the window framebuffer (shared screens) — so that the transcript, decisions, tasks, and shared-screen content all become searchable in my knowledge graph without leaving the meeting tool.

---

## Context

A 60-minute meeting produces 60 minutes of dense, verbatim, decision-laden context. Today users either lose it, hand-type notes during the meeting (poor signal), or use a third-party tool (Otter, Granola, Cleft, Zoom AI Companion) that doesn't compose with the rest of the user's knowledge graph.

ctxt already has the pipeline machinery: `audio.transcribe` (diarization, alignment, sectioning) and `video.full` (audio_extractor → audio_transcriber → frame_sampler → scene_detector → frame_ocr → timeline_assembler) live in `internal/pipeline/builtins/`. **The missing piece is just the capture source.** This story is that source.

Per [ADR-069](../../decisions/ADR-069-meeting-capture-source.md), three desktop OS tracks share a common Go `MeetingRecorder` interface, with OS-specific implementations behind build tags:

- macOS 13+: ScreenCaptureKit (system audio + window framebuffer in one stream)
- Windows 10+: WASAPI loopback + Graphics Capture API
- Linux Wayland: xdg-desktop-portal + PipeWire (X11 fallback for non-portal compositors)

Mobile companions (iOS / Android) are deferred to Phase 6/7 — covered in [US-0222](US-0222-mobile-meeting-companion.md).

**Privacy is structural, not incidental:** explicit-trigger only in v1, mandatory recording indicator, kit/policy CEL veto on `*.requested` before OS permission prompt, first-class redact-as-supersede ([US-0218](US-0218-meeting-redact-export.md)). Auto-detect prompt ([US-0221](US-0221-meeting-auto-detect-prompt.md)) is opt-in and never starts recording without explicit confirmation.

---

## Acceptance Criteria

### Substrate

- [ ] Meeting source registered as `meeting` in the ambient runner
- [ ] `MeetingRecorder` Go interface in `internal/ambient/meeting/recorder.go` (Start / Stop / Pause / Resume / Status)
- [ ] OS-specific impls behind build tags: `recorder_darwin.go`, `recorder_windows.go`, `recorder_linux.go`, `recorder_unsupported.go` (returns ErrNotSupported)
- [ ] Same MeetingRecorder semantics across all platforms; test suite enforces parity

### macOS (Phase 3a + 3b)

- [ ] **3a (audio-only):** ScreenCaptureKit audio + AVCaptureSession mic mix → `.m4a` → `audio.transcribe`
- [ ] **3b (full):** ScreenCaptureKit audio + window framebuffer → `.mov` (H.264 + AAC) → `video.full`
- [ ] Swift bridge (`resources/ScreenCaptureKitBridge.swift`) compiled at build time
- [ ] First-run permission prompt for Screen Recording + Microphone

### Windows (Phase 3c)

- [ ] WASAPI loopback (system audio) + Graphics Capture API (video) + Media Foundation encoder → `.mp4`
- [ ] C++ bridge (`resources/WindowsCaptureBridge.cpp`) compiled at build time
- [ ] First-run permission flyout

### Linux (Phase 3d)

- [ ] xdg-desktop-portal ScreenCast (Wayland) + PipeWire monitor (audio) + gstreamer → `.webm` (or `.mp4` if H.264-licensed build)
- [ ] X11 fallback via XComposite + XDamage (second-class)
- [ ] portal feature-test on first run; report concrete gaps for non-supportive compositors

### Trigger paths

- [ ] CLI: `ctxt capture meeting start --label STR [--audio-only] [--no-video] [--window-title TITLE] [--duration MAX]`
- [ ] Hotkey (configurable; default ⇧⌘M / Ctrl+Shift+M)
- [ ] Stop: `ctxt capture meeting stop` OR same hotkey OR auto-stop when meeting bundle exits foreground
- [ ] **NO** silent auto-record. Auto-detect prompt is a separate feature ([US-0221](US-0221-meeting-auto-detect-prompt.md))

### Recording indicator

- [ ] Mandatory: red pulsing menubar/tray icon while recording
- [ ] OS-native indicator preserved (macOS orange/purple dot; Windows Graphics Capture overlay)
- [ ] `ctxt.ambient.meeting.indicator_displayed` bus event fires within 500ms of `…started`
- [ ] kit/policy CEL watchdog rule asserts indicator is shown; if not, recording auto-pauses

### Pipeline routing

- [ ] Audio-only mode → POST `/api/v1/analyze` with `pipeline=audio.transcribe`
- [ ] Full mode → `pipeline=video.full`
- [ ] Active SessionID attached
- [ ] Bus event `ctxt.ambient.meeting.enqueued` carries pipeline name + KO ID once available

### Storage / retention (T-0519)

- [ ] Media files under `$XDG_STATE_HOME/ctxt/ambient/media/<session_id>/<uuid>.{mov,mp4,webm,m4a}`
- [ ] Default retention: `meeting.media_retention_hours = 48` (file deleted after; transcript persists forever)
- [ ] Local cap: `meeting.local_max_gb = 20` with LRU eviction
- [ ] Optional S3 archive: `meeting.s3_archive = true`
- [ ] `ctxt.ambient.meeting.media_evicted` bus event on local delete; `…media_archived` on S3 upload

### Failure modes

- [ ] Device unplug mid-recording (headphones, USB mic): emit `…device_changed` + recover or stop gracefully
- [ ] Codec error mid-recording: emit `…capture_failed`, leave partial file for retry
- [ ] Permission revoked mid-recording: emit `…capture_failed`, stop cleanly
- [ ] Display sleep / lid close: configurable behavior (continue / pause)

### Bus event coverage

- [ ] All 22+ topics from [ADR-069 §5](../../decisions/ADR-069-meeting-capture-source.md) fire as documented

### Cross-OS parity

- [ ] Same MeetingRecorder semantics across darwin/windows/linux
- [ ] Same bus event taxonomy (mobile companions [US-0222] emit identical topics over HTTP/WS)
- [ ] Test suite enforces parity (per-OS recorder tests with synthetic audio/video sources)

---

## Implementation Notes

### Architecture

> See [ADR-069 §Implementation Notes](../../decisions/ADR-069-meeting-capture-source.md) for the full file structure.
>
> Diagrams: [`069-recording-state.mmd`](../../diagrams/ambient/069-recording-state.mmd), [`069-multi-platform.mmd`](../../diagrams/ambient/069-multi-platform.mmd), [`069-recording-sequence.mmd`](../../diagrams/ambient/069-recording-sequence.mmd).

### Phasing

Per the ambient-capture track:

1. T-0513 — macOS audio-only (Phase 3a): smallest E2E to prove substrate
2. T-0514 — macOS full audio + video (Phase 3b)
3. T-0515 — Windows recorder (Phase 3c)
4. T-0516 — Linux recorder (Phase 3d)
5. T-0519 — media retention tier (parallel)

### Why ScreenCaptureKit over BlackHole/Loopback.app

Per [ADR-069 §Decision](../../decisions/ADR-069-meeting-capture-source.md): macOS 13+'s ScreenCaptureKit is public, vendor-blessed, captures system audio + window framebuffer in one stream, no kernel-level driver install. BlackHole/Loopback.app are documented as fallback for macOS 12 holdouts only.

---

## E2E Checklist

- [ ] **macOS audio-only**: `ctxt capture meeting start --label "test" --audio-only`; play 30s synthetic speech; stop; verify KO via `audio.transcribe` with diarization, transcript, mentions extracted
- [ ] **macOS full**: same flow with --no `--audio-only`; verify KO via `video.full` with frame OCR present (slides shared during the synthetic test)
- [ ] **Windows full**: same flow on Win10+; verify `.mp4` produced
- [ ] **Linux full**: same flow on a Wayland compositor; verify `.webm` produced
- [ ] **Hotkey trigger**: bind hotkey; press; verify recording starts without CLI invocation
- [ ] **Recording indicator**: verify menubar pulses red while recording; absence triggers CEL watchdog auto-pause
- [ ] **CEL veto**: rule for bundle-id `com.bank.*`; bring it to focus; press start hotkey; verify recording rejected with `…policy_vetoed` event
- [ ] **Permission denied**: deny first-run OS prompt; verify graceful error + setup pointer
- [ ] **Device unplug mid-recording**: unplug headphones; verify `…device_changed` event; recovery or graceful stop
- [ ] **Pipeline runs**: verify `…transcript_ready` fires with KO ID once `audio.transcribe` / `video.full` completes
- [ ] **SessionID attached**: KO has `session_id` matching the active cutter session
- [ ] **Retention**: verify media file deleted at `media_retention_hours`; KO persists
- [ ] **S3 archive**: enable; verify file uploaded; verify local file evicted while S3 copy retained

---

## Related Stories

- [US-0218](US-0218-meeting-redact-export.md) — Redact + export commands (depends on this)
- [US-0221](US-0221-meeting-auto-detect-prompt.md) — Auto-detect prompt (Phase 5; depends on this)
- [US-0222](US-0222-mobile-meeting-companion.md) — Mobile companion apps (Phase 6/7; same bus taxonomy)
- [US-0216](US-0216-work-sessions.md) — Sessions group meeting captures with prep/follow-up
- [US-0004](../ingestion/US-0004-audio-transcription-and-indexing.md) — Audio transcription pipeline (this story drives it)
- [US-0005](../ingestion/US-0005-video-processing-with-scenes.md) — Video processing pipeline (this story drives it)

---

## Sprint

**Skeleton 9.5 — Ambient Capture + Sessions + Agent-Native MCP**
Implements ADR-069 desktop tracks (Phase 3a/3b/3c/3d) — see `tlc track show ambient-capture`, tasks **T-0513, T-0514, T-0515, T-0516, T-0519**.

---

## E2E Tests

- planned: `test/integration/us0217_meeting_macos_test.go::TestMeeting_AudioOnly_macOS`
- planned: `test/integration/us0217_meeting_macos_test.go::TestMeeting_Full_macOS`
- planned: `test/integration/us0217_meeting_windows_test.go::TestMeeting_Full_Windows`
- planned: `test/integration/us0217_meeting_linux_test.go::TestMeeting_Full_LinuxWayland`
- planned: `test/integration/us0217_meeting_linux_test.go::TestMeeting_Full_LinuxX11Fallback`
- planned: `test/integration/us0217_meeting_test.go::TestMeeting_HotkeyTrigger`
- planned: `test/integration/us0217_meeting_test.go::TestMeeting_RecordingIndicatorMandatory`
- planned: `test/integration/us0217_meeting_test.go::TestMeeting_CELVetoBeforePermission`
- planned: `test/integration/us0217_meeting_test.go::TestMeeting_DeviceChangeMidRecording`
- planned: `test/integration/us0217_meeting_test.go::TestMeeting_RetentionEviction`
- planned: `test/integration/us0217_meeting_test.go::TestMeeting_S3Archive`
