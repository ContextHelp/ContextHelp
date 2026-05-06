# ADR-069 – Meeting Capture Source (Audio + Video, Multi-Platform)

> **Status:** Accepted
> **Date:** 2026-05-05
> **Author:** jadb
> **Applies to:** ctxt CLI, ctxd local daemon, mobile companions (iOS/Android), existing audio.transcribe / video.full / video.audio_only pipelines
> **Supersedes:** None
> **References:** ADR-066 (ambient capture substrate), ADR-067 (sessions), ADR-068 (MCP read-surface), ADR-053 (KnowledgeObject as pipeline draft), ADR-056 (unified enqueue API), prior art: Granola, Otter, Cleft, Zoom AI Companion (proprietary), OpenAI Whisper (transcription), Apple ScreenCaptureKit, Microsoft Graph Media Capture API

---

## Context

The ambient capture substrate (ADR-066) ships sources for clipboard, file-watch, browser-history, foreground-window, and on-demand screenshot. The next high-leverage source is **meeting capture** — recording the audio and video of video calls (Zoom, Meet, Teams, FaceTime, Discord, Slack Huddles, …) and feeding the result into ctxt's existing transcript + frame-OCR pipelines.

**Why this is high-value:**

- A 60-minute meeting produces 60 minutes of dense, verbatim, decision-laden context that the user will never re-watch but desperately wants searchable. Today this lives in a separate tool (Otter, Granola, Cleft, Zoom's own AI Companion) that doesn't compose with the rest of the user's knowledge graph.
- The pipeline machinery is **already built**: `audio.transcribe` (speaker diarization, timestamp alignment, sectioning, tagging, embeddings) and `video.full` (audio_extractor → audio_transcriber → frame_sampler → scene_detector → frame_ocr → timeline_assembler) live in `internal/pipeline/builtins/{audio_transcribe,video_full,video_audio_only}.go`. The hard work — running Whisper, splitting speakers, OCR'ing slides, aligning frames to transcript — is solved. **The missing piece is just the capture source: get the audio+video file onto disk, hand it to a pipeline, attach it to the active session.**
- Per ADR-067, sessions cluster ambient captures into work units. A meeting *is* a session (often the most important one of the day). Grouping pre-meeting prep (clipboard, browser visits) + the meeting itself (audio + video transcript) + post-meeting follow-ups (file edits, more clipboard) under one SessionID is exactly what the substrate enables.

**Why this is not the same problem as v1's screenshot/clipboard sources:**

- **Multi-platform** with a hard split. Mac/Linux/Windows are desktop OSes where `ctxd` (the Go daemon from ADR-066) runs natively and can hook OS audio/screen APIs. iOS and Android are mobile OSes where `ctxd` does not run; they need either a companion app (Swift/Kotlin) or a bridge (iOS Shortcut → ctxd-on-laptop). The architecture must be honest about this split — one codepath cannot cover both.
- **System audio is the hard problem, not microphone audio.** Capturing your own mic is trivial (every OS has a public mic API). Capturing the *participants on the call* requires either a virtual audio loopback driver (BlackHole, Loopback.app, VB-Cable) or an OS-blessed loopback API (macOS 13+'s ScreenCaptureKit audio, Windows WASAPI loopback, Linux PulseAudio monitor source). The user-facing setup story varies wildly by OS.
- **Video capture means the meeting window's framebuffer**, not the user's webcam. Same OS-API split: ScreenCaptureKit (macOS), DXGI Desktop Duplication / Graphics Capture API (Windows), pipewire / xdg-desktop-portal (Linux Wayland), X11 XComposite (Linux X11). Webcam capture is not what's wanted here.
- **Privacy is structural, not incidental.** Recording a meeting where other humans are speaking has consent implications (legal in some jurisdictions, illegal in others; one-party-consent vs. two-party-consent regimes vary by US state alone). The source must default to off, require explicit user trigger, and surface a clear "recording" indicator. No always-on auto-detect.
- **Storage cost is real.** A 60-minute 1080p video at moderate bitrate is ~500MB; 100 meetings is 50GB before any retention. The buffer (ADR-066) handles raw events well in the kilobyte-to-megabyte range; multi-hundred-megabyte video files want different lifecycle defaults.

**The question:** What's the right shape for a meeting capture source that handles desktop and mobile honestly, captures system audio + video at the user's explicit trigger, hands files to existing pipelines, and respects the consent/storage realities?

---

## Decision

**Adopt a Meeting Capture Source as a multi-platform ambient source under ADR-066, with three platform tracks and two trigger modes:**

### 1. Three platform tracks

| Track | Platforms | Implementation | Status |
|---|---|---|---|
| **Desktop** | macOS 13+, Windows 10+, Linux (Wayland-first, X11 fallback) | Native Go in `internal/ambient/meeting/` using cgo bindings to OS APIs; ships in `ctxd` | Phase 3 of ADR-066 + this ADR |
| **iOS companion** | iPhone, iPad (iOS 17+) | Swift app (`ctxt-ios`); ReplayKit for screen+audio; POSTs to user-configured dpkms via the existing `/api/v1/analyze` HTTP path | Phase 6 (deferred) |
| **Android companion** | Android 10+ | Kotlin app (`ctxt-android`); MediaProjection + AudioPlaybackCapture (Android 10+); same enqueue path | Phase 7 (deferred) |

iOS/Android companions are **out of v1** but the substrate (enqueue API, session tagging, profile resolution) supports them on day one. They post recordings to whichever dpkms is configured (local-network or remote) per ADR-066's enqueue model.

> **Repo structure: TBD at Phase 6/7 start.** Earlier drafts of this ADR locked-in "separate repos" for iOS/Android. That decision was premature — both monorepo and split-repo are defensible, with real trade-offs:
>
> - **Monorepo** (in this repo, under `mobile/ios/` + `mobile/android/`): one source of truth for the wire contract (RawEvent / RecordOptions / bus topics), atomic cross-language refactors, easier end-to-end tests. Trade-off: heterogeneous CI (Swift + Kotlin + Go in one pipeline).
> - **Separate repos** (`ctxt-ios`, `ctxt-android`): matches the rest of the ctxt org's repo layout, isolates Apple Developer / Play Console secrets, independent release cadence. Trade-off: three-way schema sync, integration-test ceremony.
>
> The decision is **revisited when Phase 6 actually starts**, not now. Either path requires identical Go-side work (none beyond what's already shipped); the substrate's enqueue API + bus event taxonomy are the binding contract. Whatever repo structure ships, the wire is unchanged.

### 2. Two trigger modes (desktop)

- **Explicit trigger (default, primary, only mode in v1):**
    - `ctxt capture meeting start [--label "Q3 planning"] [--audio-only] [--no-video]`
    - Hotkey binding (configurable; default ⇧⌘M / Ctrl+Shift+M).
    - Menubar/tray button (provided by `ctxd` in subsequent phase, or by a thin platform-native launcher per OS).
    - **Stop:** `ctxt capture meeting stop`, the same hotkey, or auto-stop when the call ends (detected via `foreground` source: bundle-id leaves `us.zoom.xos`/`com.microsoft.teams2`/`com.google.Chrome` with `meet.google.com`/etc.).
- **Auto-detect prompt (Phase 5, opt-in):**
    - `ctxd`'s `foreground` source observes a known meeting-app bundle-id come to focus.
    - Emits a *prompt event* (OS notification + hotkey to confirm) — does NOT start recording. Recording requires explicit user confirmation. Default 10-second confirmation window; auto-dismiss if user doesn't act.
    - Per-app remember-my-choice (per kit/policy CEL rule): "always start when Zoom is focused" or "never auto-prompt for Discord."

**No always-on, no silent auto-record.** Drop-in tools like Otter and Granola behave differently (always-on background recording with manual exclusion); we choose explicit-by-default to match ctxt's existing privacy posture and to side-step consent-law minefields.

### 3. Capture mechanism per OS

**macOS (13+):**
- **Audio + video together:** [`ScreenCaptureKit`](https://developer.apple.com/documentation/screencapturekit) (public API since macOS 12.3, system-audio capture since 13.0). Captures both system audio (the meeting participants) and the meeting window's framebuffer in one stream. Requires `NSScreenCaptureUsageDescription` permission (system prompt on first use).
- **Mic mixing:** `AVCaptureSession` for the user's microphone, mixed with the system-audio stream into a single 2-channel AAC/Opus track.
- **Output format:** `.mov` (H.264 video + AAC audio) for `video.full` pipeline, OR `.m4a` (audio only) for `audio.transcribe` if `--audio-only`.
- **No third-party dependencies in the default path.** BlackHole/Loopback.app are NOT required on macOS 13+; we document them as fallback for macOS 12 only (which we may choose not to support).

**Windows (10+):**
- **Audio:** WASAPI loopback (public API; no driver install). Captures system-mix output device. Mic via WASAPI capture.
- **Video:** Windows Graphics Capture API (Windows 10 1903+). Captures specific window or full screen.
- **Output format:** `.mp4` (H.264 + AAC) via Media Foundation.

**Linux (Wayland primary, X11 fallback):**
- **Audio:** PulseAudio monitor source (`pactl load-module module-loopback`) or PipeWire monitor link. Mic via standard ALSA/PipeWire input.
- **Video (Wayland):** [`xdg-desktop-portal` ScreenCast interface](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.ScreenCast.html) — works on GNOME, KDE, Sway, Hyprland (compositor support varies; we feature-test).
- **Video (X11):** XComposite + XDamage; lower quality, kept as fallback for X11 holdouts.
- **Output format:** `.webm` (VP9 + Opus) via `gstreamer` pipeline OR `.mp4` if the user has H.264-licensed builds.

**Recording always lands on local disk first,** under the buffer's media subdirectory (`$XDG_STATE_HOME/ctxt/ambient/media/<session_id>/<uuid>.{mov,mp4,webm,m4a}`). The buffer treats meeting recordings as a first-class type with separate retention defaults (see §4).

### 4. Buffer + retention for media

Meeting recordings are large; the ADR-066 buffer's defaults (raw 7d / normalized 30d / 2GB total cap) are wrong for them. New media tier:

- **Default retention:** raw media file kept until the pipeline successfully completes AND for `meeting.media_retention_hours` (default **48 hours**) afterwards. Then deleted from local disk. The transcript + frame-OCR results survive as KnowledgeObjects in dpkms forever (unchanged from existing pipelines).
- **Fallback to S3 buffer (per ADR-066's pluggable buffer):** users with the S3 backend configured can extend retention indefinitely; `meeting.s3_archive = true` keeps the raw media file in cold storage even after local eviction. Useful for legal/compliance retention, court-of-record meetings, etc.
- **Local cap:** `meeting.local_max_gb` (default **20GB** for media — separate from the substrate's 2GB raw-event cap). LRU eviction within the cap.
- **Pipeline routing:** on capture-stop, `ctxd` enqueues against `audio.transcribe` (audio-only mode) or `video.full` (full mode), with the file path as the input and the active SessionID attached. dpkms's existing pipeline machinery does the rest.

### 5. Bus events (extending ADR-066's taxonomy)

Every state transition in the meeting capture lifecycle emits a kit/runtime/bus event (4-segment past-tense topics: `ctxt.ambient.meeting.<object>.<action>`). Both desktop (`ctxd`) and mobile companion (Phase 6) implementations MUST emit the same taxonomy so observers don't care where the recording originated.

| Topic | Emitted when | Payload includes |
|-------|--------------|------------------|
| **Trigger / lifecycle** | | |
| `ctxt.ambient.meeting.requested` | User pressed hotkey / called `start` / mobile companion tapped record (before any recording starts) | `mode`, `label`, `source` (hotkey/cli/menubar/auto_detect/mobile_app), `target_window`, `requested_at` |
| `ctxt.ambient.meeting.policy_vetoed` | kit/policy CEL rule rejected the request | `rule_id`, `reason`, originating-request payload |
| `ctxt.ambient.meeting.permission_requested` | OS permission prompt shown (first-time flow) | OS name, permission scope (screen/audio/mic) |
| `ctxt.ambient.meeting.permission_granted` | User granted OS permissions | OS name, permissions granted |
| `ctxt.ambient.meeting.permission_denied` | User denied OS permissions | OS name, permission scope, deny reason if available |
| `ctxt.ambient.meeting.started` | Recording is actively running | `mode`, `session_id`, `started_at`, mic capture status, system-audio capture status, video capture status, output file path |
| `ctxt.ambient.meeting.indicator_displayed` | Recording-indicator UI element shown — observable so absence is alarming | indicator type (menubar/tray/os-native), placement |
| `ctxt.ambient.meeting.paused` | User pressed pause | `paused_at`, elapsed-so-far |
| `ctxt.ambient.meeting.resumed` | User pressed resume | `resumed_at`, gap-duration |
| `ctxt.ambient.meeting.stopped` | Recording stopped | `duration_sec`, `bytes_on_disk`, `end_reason` (user/auto_detect/max_duration/error/shutdown/permission_revoked) |
| **Auto-detect (Phase 5)** | | |
| `ctxt.ambient.meeting.auto_detected` | `foreground` source observed a meeting bundle-id; prompt about to fire | bundle_id, suggested label |
| `ctxt.ambient.meeting.prompt_dismissed` | User dismissed the auto-detect prompt without confirming | bundle_id, dismiss reason (timeout/explicit) |
| **Capture errors (mid-recording)** | | |
| `ctxt.ambient.meeting.capture_warned` | Recoverable warning during recording (e.g. dropped audio frames) | warning class, recovery action taken |
| `ctxt.ambient.meeting.capture_failed` | Unrecoverable error mid-recording | error class, partial-file path if any |
| `ctxt.ambient.meeting.device_changed` | Audio/video device disappeared/swapped mid-recording (lid close, headphones unplug) | device type, before/after, recovery taken |
| **Pipeline + KO** | | |
| `ctxt.ambient.meeting.enqueued` | File handed to pipeline (`audio.transcribe` or `video.full`) | pipeline name, file path, session_id, enqueue_id |
| `ctxt.ambient.meeting.transcript_progress` | Pipeline is reporting progress (long meetings) | percent_complete, partial_text_available |
| `ctxt.ambient.meeting.transcript_ready` | Transcript pipeline completed | KnowledgeObject ID, transcript length, speaker count |
| `ctxt.ambient.meeting.transcript_failed` | Pipeline errored | KnowledgeObject draft ID if any, error class |
| **Storage / retention** | | |
| `ctxt.ambient.meeting.media_archived` | Raw media uploaded to S3 archive (if `meeting.s3_archive=true`) | s3 key, bytes |
| `ctxt.ambient.meeting.media_evicted` | Local media file deleted per retention policy | file path, retention reason (TTL/cap/manual) |
| **Redaction** | | |
| `ctxt.ambient.meeting.redact_requested` | User invoked `ctxt capture meeting redact` | KnowledgeObject ID, segment range |
| `ctxt.ambient.meeting.redact_completed` | Redaction supersede chain written + media segment removed | new KnowledgeObject ID, segments removed, archive updated |
| **Export** | | |
| `ctxt.ambient.meeting.exported` | User invoked `ctxt capture meeting export` | KnowledgeObject ID, format (md/srt/vtt), output path |

**kit/runtime/policy CEL rules** can subscribe at any of these topics for fine-grained gating. Common patterns:

- **Veto** on `…requested`: "never record while bundle-id is in `[com.bank.*, com.legal.*]`" or "require non-empty label between 18:00 and 08:00." Veto fires `…policy_vetoed` and short-circuits before the OS permission prompt.
- **Audit** on `…started` / `…stopped`: ship to a compliance log; alert if a recording exceeds `max_session_hours`.
- **Alert** on `…capture_failed` / `…device_changed`: surface to the user immediately so they can re-record.
- **Required-companion** on `…indicator_displayed`: a watchdog rule asserts this fires within 500ms of `…started`; if not, pause recording.
- **Retention enforcement** on `…enqueued` for compliance-bound profiles: force `s3_archive=true`, override local retention to mandate 7-year retention.
- **Redaction trigger** on `…transcript_ready`: post-processing CEL rule that auto-redacts known-sensitive patterns (SSNs, credit-card numbers) before the KnowledgeObject becomes searchable.

Every event is also visible to MCP clients via the `ctxt.mcp.bus.tail` interface (per ADR-068's bus-tap), so AI agents can observe the recording lifecycle as it happens — useful for "did the meeting end? was the transcript indexed yet?" workflows.

### 6. CLI surface

```
ctxt capture meeting start [--label STR] [--audio-only] [--no-video] [--duration MAX]
ctxt capture meeting stop
ctxt capture meeting status
ctxt capture meeting list [--since X --until Y]      # past recordings
ctxt capture meeting show <id>                        # transcript + frame highlights
ctxt capture meeting redact <id> --segment HH:MM-HH:MM   # remove a segment from transcript + media
ctxt capture meeting export <id> [--format md|srt|vtt]   # render transcript out
```

The `redact` command is not optional UX — the user discovering after-the-fact that something sensitive landed in a transcript needs a fast, durable removal path. Redact rewrites the KnowledgeObject (supersede, not delete, per the ADR-066 supersede-not-delete pattern) and deletes the corresponding media segment from local disk + S3 archive if present.

### 7. Recording indicator

**Visible recording indicator is non-negotiable.** The substrate guarantees one of:
- Menubar/tray icon turns red and pulses while recording (default).
- OS-native recording indicator if the platform provides one (macOS shows an orange/purple dot in the menubar for screen capture by default; Windows shows a Graphics Capture overlay).
- Hotkey to instantly stop AND show the active recording's metadata (label, duration, target session).

`ctxt.ambient.meeting.indicator_displayed` event fires whenever the indicator is shown; missing this event during an active recording is an observable bug (and a CEL rule can alert on it).

---

## Rationale

### Chosen: explicit-trigger desktop source + deferred mobile companions + existing pipelines

- **Existing pipelines do all the AI work.** `audio.transcribe` and `video.full` already exist with diarization, OCR, scene detection, timeline assembly. The capture source is a thin shim that produces the right file shape; we don't need to reimplement Whisper, FFmpeg, or speaker clustering.
- **Explicit trigger sidesteps the consent-law minefield.** Two-party-consent jurisdictions (CA, FL, IL, MA, MD, MT, NV, NH, PA, WA in the US — and most of Europe under GDPR) require all parties to be aware. Always-on recording would force ctxt to ship lawyer-disclaimers at the wrong altitude. Explicit-by-default with an unmissable indicator pushes the consent burden to the user (where it belongs) and matches what most modern tools do (Zoom requires the host to start recording; Granola asks before starting).
- **Three desktop OS tracks share substrate, differ in capture API.** The Go interface (`MeetingRecorder`) is the same across OSes; the OS-specific implementations live behind build tags. ScreenCaptureKit / WASAPI+Graphics Capture / xdg-desktop-portal are public, modern, and don't require kernel-level drivers (BlackHole et al. become fallback, not requirement).
- **Mobile is its own architecture problem and deserves dedicated native apps.** ReplayKit (iOS) and MediaProjection (Android) are designed for in-app screen+audio capture. A Go daemon will not run on iOS at all and runs on Android only awkwardly. Ship `ctxt-ios` (Swift) and `ctxt-android` (Kotlin) as small native apps that record locally and POST to the configured dpkms via the existing enqueue HTTP path. Phase 6/7 work; not v1. Repo structure (monorepo vs split repos) is TBD at Phase 6/7 start — see §1 above.
- **Media files get their own retention tier because their size profile is different.** Bolting a 500MB file into a buffer designed for kilobyte events is a bug magnet. Separate cap, separate retention default, separate eviction story, separate optional S3 archive — all configurable, all defaulted to sensible values, all auditable via bus events.
- **Redact is a first-class command, not an afterthought.** Users will record a meeting where someone says something they later wish wasn't recorded. Without redact, ctxt becomes a liability. Redact-as-supersede composes with the existing data model (ADR-066's append-only-with-supersede pattern).

### Rejected alternatives

1. **Always-on auto-record (Otter/Granola model).** Rejected: consent-law exposure varies by jurisdiction and is the user's problem, not ours. Always-on is also disrespectful of the participants who didn't consent. Auto-detect *prompt* (with explicit confirm) is the closest we go.

2. **Native Go audio/video capture across all OSes (no cgo).** Rejected: pure-Go audio capture libs (malgo, oto) work for mic only; system-audio loopback requires OS APIs that need cgo on every OS. The cost is build complexity (cross-compile gets harder); the benefit is using the same APIs the platform vendor blesses. Worth it.

3. **Single mobile companion app for both iOS and Android.** Rejected: too different. ReplayKit is broadcast-extension-shaped; MediaProjection is foreground-service-shaped. Cross-platform mobile frameworks (Flutter, React Native) don't expose system-audio-capture cleanly. Two thin native apps will be smaller and more reliable than one cross-platform abstraction.

4. **Capture only audio (skip video).** Rejected: shared screens during meetings carry rich content (slides, code, diagrams) that the existing `video.full` pipeline OCR's into searchable text. Audio-only loses 30–60% of meeting information value. Audio-only is a *flag* (`--audio-only`), not the default.

5. **Stream live to dpkms during recording (no local file).** Rejected: network glitches during a 60-minute meeting destroy the whole recording. Local-file-first with deferred enqueue is robust; it's also how every other meeting recorder works for good reason.

6. **Use a third-party meeting bot (joins the call as a participant).** Rejected: requires sharing meeting invite links with our service, depends on each platform's bot API or accessibility hacks, breaks for meetings the user is hosting from a different account, and conflicts with corporate meeting-recording policies. Capture-on-the-user's-machine is dramatically simpler and works for any platform the user can join.

7. **Record webcam (user's own video).** Rejected: not the wanted signal. The interesting thing in a video call is the *other* participants and any shared screen, not your own face. Webcam capture would also amplify privacy concerns without adding value.

8. **Skip mobile entirely.** Rejected: the user explicitly named mobile as in-scope. iOS/Android meetings are a real and growing share of meetings (especially commute calls, on-the-go check-ins). Deferring to Phase 6 is honest about scope; cutting them is wrong.

9. **Roll our own transcript/diarization.** Rejected: `audio.transcribe` already exists with `audio_transcriber` + `speaker_diarizer` + `timestamp_aligner` steps. The pipeline is the right home for transcript work; the capture source's job ends at "produce a file."

---

## Consequences

### Positive

- **A 60-minute meeting becomes a searchable KnowledgeObject** with diarized transcript, OCR'd shared-screen frames, scene-aligned timeline, mentions extracted, embeddings indexed, and SessionID attached — all via existing pipelines. First-class member of the knowledge graph.
- **Meetings group naturally with prep/follow-up via ADR-067 sessions.** Pre-meeting clipboard captures + the meeting itself + post-meeting file edits compose into one queryable session in `ctxt session show`.
- **Three desktop OSes covered with public, vendor-blessed APIs.** No kernel drivers, no questionable workarounds. Setup is OS permission prompts only (the user grants screen+audio recording once per app).
- **iOS/Android architecture is named and bounded.** Companion apps can ship later without redesigning the substrate; the enqueue path absorbs them on day one.
- **Privacy posture is structural.** Explicit trigger, mandatory indicator, kit/policy veto on `*.requested`, redact-as-supersede. Each layer composes; none can be silently bypassed.
- **Existing pipelines absorb meeting media transparently.** No new pipeline to maintain; the enrichment quality bar moves with whatever Whisper/diarization improvements ship for everyone.
- **Bus event taxonomy enables observability and policy** at every transition (request → permission → start → indicator → stop → enqueue → eviction). Missing events surface bugs.

### Negative

- **cgo dependencies per desktop OS.** macOS needs ScreenCaptureKit Swift bridge; Windows needs Media Foundation / Graphics Capture C++ bridge; Linux needs gstreamer + xdg-desktop-portal D-Bus calls. Build complexity goes up; cross-compile from a single host gets harder. Mitigated by build-tagged platform files and CI runners per OS.
- **Permission prompts are platform-specific UX.** First-time recording on each OS shows a different dialog (System Settings → Privacy → Screen Recording on macOS; settings flyout on Windows; portal-mediated on Linux). We document each; we cannot suppress any.
- **Storage footprint is dramatically larger than other ambient sources.** A heavy meeting day (4×60min recordings) produces ~2GB. Default 20GB cap = ~10 days of heavy use before LRU eviction. Power users will need to tune `meeting.local_max_gb` upward or enable S3 archive.
- **Recording can fail mid-meeting.** OS-level pause (display sleep, lid close), audio device unplug, codec errors — all real. We must surface failures clearly via bus events and partial-recording recovery (transcribe whatever was captured).
- **Two-party-consent jurisdictions remain a user problem.** ctxt provides the mechanism; the user provides the consent. We document the legal landscape but don't enforce it (nor could we — we don't know who's on the call).
- **Mobile companion apps are real software products** with their own release cadence, app-store review, signing certs, and OS API drift. Phase 6 is large.
- **Auto-detect prompt (Phase 5) walks closer to the line.** Even prompting can feel intrusive if it fires for every Zoom launch. Per-app remember-choice via kit/policy is the relief valve.

### Neutral / Considerations

- **macOS 12 support.** ScreenCaptureKit's audio capture is 13.0+. macOS 12 users can install BlackHole/Loopback.app and use the older `AVCaptureSession` route as a fallback. Decide whether to ship that path or hold to 13+ minimum. Recommend: 13+ minimum, document BlackHole workaround for users who can't upgrade.
- **Linux compositor variance.** GNOME, KDE, Sway, Hyprland all ship xdg-desktop-portal but with different completeness. We feature-test on first run and report concrete gaps; X11 fallback exists for compositors without good portal support.
- **Live transcription (transcribe-while-recording) vs. post-hoc.** v1 ships post-hoc only — file lands, pipeline runs, transcript appears 1–5 minutes later depending on Whisper model. Live transcription (with running-text in a UI overlay) is a significant additional engineering effort; deferred.
- **Speaker identity beyond diarization.** Diarization produces "Speaker A / B / C" anonymously. Tying speakers to the user's contacts/entities (via voice fingerprinting or video face-recognition) is a separate, much more privacy-fraught problem. Out of scope for this ADR.
- **Calendar integration.** A meeting captured between 2:00 and 3:00 PM with a calendar event titled "Q3 planning with Acme" should auto-label. Hooks into the existing `[ambient.calendar]` source (when it ships per ADR-065 federation Phase 3). Stub the field for now.
- **Slack Huddles / Discord voice / FaceTime.** All work with this design (system-audio loopback + window framebuffer); they just aren't auto-detected by bundle-id heuristics in v1. Per-app onboarding ("teach ctxd this app is a meeting app") covers them.

---

## Implementation Notes

### New files

```
internal/ambient/meeting/
├── meeting.go              — MeetingSource (implements AmbientSource); state machine
├── meeting_test.go
├── recorder.go             — MeetingRecorder interface (Start/Stop/Pause/Resume; produces files)
├── recorder_darwin.go      — ScreenCaptureKit bridge (cgo + Swift)
├── recorder_windows.go     — Media Foundation / Graphics Capture bridge (cgo + C++)
├── recorder_linux.go       — gstreamer + xdg-desktop-portal (cgo + D-Bus)
├── recorder_unsupported.go — build-tag fallback (returns ErrNotSupported)
├── indicator/              — recording-indicator UI hooks (menubar/tray)
│   ├── indicator_darwin.go
│   ├── indicator_windows.go
│   ├── indicator_linux.go
├── retention.go            — media-tier retention (separate from event-tier)
├── redact.go               — segment-level transcript + media redaction
└── events.go               — bus topic builders (kit/bus 4-segment)

cmd/ctxt/cmd/
├── capture_meeting.go      — `ctxt capture meeting {start,stop,status,list,show,redact,export}`

resources/
├── ScreenCaptureKitBridge.swift   — compiled at build-time, vendored as .a or .dylib
├── WindowsCaptureBridge.cpp       — compiled at build-time, vendored as .lib or .dll
```

Mobile companions (Phase 6/7; repo structure TBD — monorepo `mobile/{ios,android}/` or split repos `ctxt-ios` / `ctxt-android`):
```
ctxt-ios/    — Swift, ReplayKit-based, ~1000 LoC
ctxt-android/ — Kotlin, MediaProjection + AudioPlaybackCapture, ~1500 LoC
```

### Modified files

- `internal/pipeline/builtins/audio_transcribe.go` — no changes; reuse as-is
- `internal/pipeline/builtins/video_full.go` — no changes; reuse as-is
- `internal/pipeline/builtins/video_audio_only.go` — no changes; reuse as-is
- `cmd/ctxd/main.go` — register MeetingSource alongside other ambient sources
- `cmd/ctxt/cmd/root.go` — register `capture meeting` subcommand under CAPTURE category

### MeetingRecorder interface (Go)

```go
package meeting

type Mode int
const (
    ModeFull       Mode = iota // audio + video
    ModeAudioOnly
)

type RecordOptions struct {
    Mode         Mode
    Label        string
    SessionID    string         // tagged onto KO via ADR-067
    OutputDir    string         // buffer media subdir
    MaxDuration  time.Duration  // safety cap; 0 = no cap
    SourceWindow *WindowTarget  // nil = full screen; non-nil = specific window
}

type WindowTarget struct {
    BundleID    string // macOS
    PID         int    // any
    WindowTitle string // substring match
}

type MeetingRecorder interface {
    Start(ctx context.Context, opts RecordOptions) (RecordHandle, error)
}

type RecordHandle interface {
    Pause(ctx context.Context) error
    Resume(ctx context.Context) error
    Stop(ctx context.Context) (RecordResult, error)
    Status() RecordStatus
}

type RecordResult struct {
    FilePath   string
    Duration   time.Duration
    BytesSize  int64
    Mode       Mode
    EndReason  string // "user" | "auto_detect" | "max_duration" | "error" | "shutdown"
}
```

OS-specific files implement `MeetingRecorder`; `meeting.go` consumes the interface.

### Phasing

- **Phase 3a (this ADR's Phase 1) — macOS recorder, audio-only mode.** ScreenCaptureKit audio + ScreenCaptureKit video recording. Hand to `audio.transcribe`. Ship behind feature flag.
- **Phase 3b — macOS full-mode (audio + video).** Hand to `video.full`. Frame OCR begins paying off.
- **Phase 3c — Windows recorder.** WASAPI loopback + Graphics Capture API. Same interface; build-tagged file.
- **Phase 3d — Linux recorder.** xdg-desktop-portal + PipeWire. X11 fallback explicitly second-class.
- **Phase 4 — auto-detect prompt.** `foreground` source observes meeting bundle-ids; emits prompt event; user confirms or dismisses.
- **Phase 5 — redact + export commands.** Segment-level transcript+media redaction; SRT/VTT export.
- **Phase 6 — iOS companion app.** ReplayKit broadcast extension. Separate repo. App store review.
- **Phase 7 — Android companion app.** MediaProjection + AudioPlaybackCapture. Separate repo. Play store review.

Phases 3a–3d and 4–5 land in the `ambient-capture` track. Phases 6–7 each get their own track and ADRs (mobile-specific concerns are non-trivial: app-store policy, signing, OAuth-dpkms-from-mobile, etc.).

### Migration concerns

- **None for existing users.** Meeting capture is opt-in via `ctxt capture meeting start`. Default install behavior unchanged.
- **No schema migrations.** KnowledgeObjects produced by the meeting source go through existing pipelines into existing tables; SessionID attaches via ADR-067's already-planned schema change.
- **Plugin API.** Meeting capture lives in `internal/ambient/`, not `pkg/pluginapi/`. Plugins do not register meeting recorders in v1.

### Testing implications

- **Per-OS recorder tests with synthetic audio/video sources.** macOS: `AVCaptureSession` with a dummy file source; Windows: synthetic Media Foundation source; Linux: gstreamer test pipeline.
- **State-machine tests for MeetingSource:** start → indicator → pause → resume → stop → enqueue → media-evict; abnormal exits (process kill mid-recording, OS sleep mid-recording, codec error mid-recording) all leave a recoverable partial.
- **Retention tests:** media file present at T+0, deleted at T+`media_retention_hours` (fake clock); KnowledgeObject persists.
- **Redact tests:** redact segment HH:MM-HH:MM, verify supersede chain in dpkms, verify media file no longer contains the segment, verify transcript no longer contains the segment, verify export honors redaction.
- **Bus-event tests:** every transition emits the documented topic.
- **End-to-end smoke (macOS first-class):** `ctxt capture meeting start --audio-only`, play 30 seconds of synthetic speech via system audio, `ctxt capture meeting stop`, verify KnowledgeObject created via `audio.transcribe` with a transcript matching the synthetic input.
- **Permission-prompt UX tests** (manual, not CI): first-run flow on each OS; deny → graceful error; revoke mid-session → graceful stop.

### Backwards compatibility

- Existing audio/video pipelines: unchanged.
- Existing ambient sources: unchanged; meeting source is additive.
- Existing storage: unchanged in v1; ADR-067's session-id field (already planned) is the only schema touch.
- iOS/Android companions (Phase 6): post to existing `/api/v1/analyze`; dpkms doesn't need to know they exist beyond accepting the standard enqueue payload.

---

## Diagrams

Source-of-truth `.mmd` files live in [`../diagrams/ambient/`](../diagrams/ambient/). See [`../diagrams/README.md`](../diagrams/README.md) for authoring conventions.

### [Recording state machine](../diagrams/ambient/069-recording-state.mmd)

State transitions emit the matching `ctxt.ambient.meeting.*` bus topic from §5. Failure modes (`vetoed`, `denied`, `capture_failed`, `pipeline_failed`) are first-class states, not error returns.

### [Multi-platform architecture](../diagrams/ambient/069-multi-platform.mmd)

Three desktop OSes share the Go `MeetingRecorder` interface (cgo bridges to ScreenCaptureKit / Media Foundation+Graphics Capture / xdg-desktop-portal+gstreamer). Mobile companions (iOS ReplayKit, Android MediaProjection+AudioPlaybackCapture) are separate native apps that POST to the same enqueue endpoint.

### [End-to-end recording sequence](../diagrams/ambient/069-recording-sequence.mmd)

User triggers recording, OS permission flow, capture, file lands, pipeline runs, transcript becomes searchable. SessionID from ADR-067 attaches throughout. Media file evicts at retention TTL while the KnowledgeObject lives forever.

---

## References

- ADR-053 — KnowledgeObject as pipeline draft (the type meeting transcripts become)
- ADR-056 — unified enqueue API (the path mobile companions POST to)
- ADR-066 — ambient capture substrate (parent ADR; meeting is a source under it)
- ADR-067 — sessions / WorkUnit (meetings group with prep/follow-up via SessionID)
- ADR-068 — MCP read-surface (meetings become queryable via `search`, `session`, `compose` tools)
- Existing pipelines:
  - `internal/pipeline/builtins/audio_transcribe.go` — diarization, timestamp alignment, sectioning
  - `internal/pipeline/builtins/video_full.go` — frames + transcript + OCR + timeline assembler
  - `internal/pipeline/builtins/video_audio_only.go` — extract + transcribe
- OS APIs:
  - Apple [ScreenCaptureKit](https://developer.apple.com/documentation/screencapturekit) (macOS 13+)
  - Microsoft [Graphics Capture API](https://learn.microsoft.com/en-us/windows/uwp/audio-video-camera/screen-capture) (Windows 10 1903+)
  - Microsoft [WASAPI loopback](https://learn.microsoft.com/en-us/windows/win32/coreaudio/loopback-recording)
  - [xdg-desktop-portal ScreenCast](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.ScreenCast.html) (Linux Wayland)
  - Apple [ReplayKit](https://developer.apple.com/documentation/replaykit) (iOS)
  - Android [MediaProjection](https://developer.android.com/reference/android/media/projection/MediaProjection) + [AudioPlaybackCapture](https://developer.android.com/guide/topics/media/playback-capture)
- Prior art (proprietary, mostly auto-record-by-default): Otter, Granola, Cleft, Zoom AI Companion, Microsoft Copilot in Teams, Google Meet AI Notes
- US one-party-vs-two-party-consent state survey: [DMLP State Law: Recording](https://www.dmlp.org/legal-guide/state-law-recording)
- tlc track: `ambient-capture` (T-05XX to be added for this ADR's tasks; see below)
