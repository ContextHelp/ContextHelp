---
status: paper
adr: ADR-069
task: T-0520, T-0521
phase: 6, 7
---

# US-0222: Mobile Meeting Companion (iOS + Android)

**System Types:** Mobile (`mobile/ios/`, `mobile/android/` — monorepo)
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

> **Phase 6/7 — out of v1 scope.** Lives in this repo at `mobile/{ios,android}/` per [ADR-069 §1](../../decisions/ADR-069-meeting-capture-source.md). v1 mobile scope is full feature parity with desktop meeting capture: Share-to-ctxt + ReplayKit/MediaProjection recording + auto-detect prompts + on-device session cutter. The substrate's enqueue API + bus event taxonomy is the binding contract; mobile is a peer of desktop.

---

## User Goal

As a knowledge worker on iPhone / iPad / Android, I want a native companion app that:

1. **Acts as a "Share to ctxt" destination** so anything from any app's share sheet (URL, text, image, audio, video) lands in my knowledge graph.
2. **Records video calls** (audio + video) with the same explicit-trigger model as desktop, routing recordings to ctxt's existing transcript + frame-OCR pipelines.
3. **Auto-detects meeting apps** (Zoom, Teams, Meet, FaceTime, Discord, Slack Huddles) when they come to focus and offers to start recording (opt-in).
4. **Cuts sessions on-device** with the same three-rule cutter as desktop (idle / soft-cut + frequent-switching / timeout) so meetings group with prep/follow-up shares.

So meetings on the commute, on-the-go check-ins, family calls, and content shared from mobile apps all flow into the same knowledge graph as desktop captures.

---

## Context

[ADR-069](../../decisions/ADR-069-meeting-capture-source.md) keeps mobile at full feature parity with desktop meeting capture (recording + sessions + auto-detect), plus adds the Share-to-ctxt extension as the primary low-friction path for non-meeting content. Mobile is a peer of desktop, not a thin client — but the implementation is platform-native because Go does not run on iOS and runs awkwardly on Android, and the OS APIs (ReplayKit on iOS, MediaProjection + AudioPlaybackCapture on Android) are designed for in-app integration.

Both apps emit the same bus event taxonomy (forwarded over HTTP/WS to dpkms after upload) and POST to the same `/api/v1/analyze` endpoint with the same payload shape. dpkms doesn't need to know they exist; the substrate absorbs them transparently. Session continuity is per-device in v1 — a phone session and a laptop session for the same activity are two distinct sessions (cross-device merging is deferred).

On-device transcript pipelines are out of v1 mobile scope — recording uploads to dpkms which runs `audio.transcribe` / `video.full` server-side. Mobile apps display the transcript once `transcript_ready` fires.

---

## Acceptance Criteria

### iOS app (`mobile/ios/`, Phase 6, T-0520)

- [ ] Swift project at `mobile/ios/` (monorepo per ADR-069 §1); distributed via App Store
- [ ] iOS 17+ minimum (ReplayKit broadcast extensions + Share Extension stable)

#### Share extension (Share-to-ctxt)
- [ ] Appears in the iOS share sheet for URL, text, image, audio, video, file content types
- [ ] On select: shows minimal UI (cancel + post buttons; optional label/note field)
- [ ] POSTs the shared content to `/api/v1/analyze` with appropriate `type` field (url/text/image/audio/video/file) and `ambient_source=mobile-ios-share`
- [ ] Handles auth via shared App Group token (set in main app's Settings)
- [ ] Failure path: queue locally; retry on next share or via main app's "Pending" view

#### Recording (ReplayKit broadcast extension)
- [ ] Recording UX: big "record" button on home screen; live duration timer + stop button while recording; optional label entry
- [ ] ReplayKit broadcast extension captures screen + system audio + mic
- [ ] Recording lands on device first under App Group's Documents directory
- [ ] Background-task API used for upload after recording stops (continues if app backgrounded)
- [ ] POSTs recording with `pipeline=video.full` (or `audio.transcribe` if audio-only), `ambient_source=mobile-ios`
- [ ] Retry logic with exponential backoff (1s, 2s, 4s … cap at 1h)
- [ ] In-app history list of past recordings + their pipeline status (pending → uploading → indexed)
- [ ] Mandatory recording indicator (iOS shows red status-bar pill during ReplayKit recording — system-enforced)

#### Auto-detect prompts (opt-in)
- [ ] Configurable list of meeting bundle-ids (defaults: Zoom, Teams, Meet web, FaceTime, Discord, Slack)
- [ ] When detected (via app-state observation or shortcut intent), surface a UNUserNotification with "Record this meeting?" + 10s confirm window
- [ ] Per-app remember-my-choice (auto-start / never / prompt) stored in app Settings
- [ ] NEVER starts recording without explicit user confirmation OR an explicit `auto-start` per-app rule

#### On-device session cutter
- [ ] Swift port of the three-rule cutter (idle / soft-cut + frequent-switching / timeout) — same defaults as desktop (5min / 3min / 2h)
- [ ] Cutter consumes share + recording events to drive session boundaries
- [ ] Active SessionID attached to every POSTed event (for grouping with prep/follow-up shares)
- [ ] PUT to `/api/v1/sessions/{id}` on session open and close (idempotent, per ADR-067 §Wire shape)

### Android app (`mobile/android/`, Phase 7, T-0521)

- [ ] Kotlin project at `mobile/android/`; distributed via Play Store
- [ ] Android 10+ minimum (AudioPlaybackCapture API)

#### Share intent receiver (Share-to-ctxt)
- [ ] Activity registered for ACTION_SEND / ACTION_SEND_MULTIPLE intents with text/uri/image/audio/video/file MIME types
- [ ] On receive: minimal UI (post button + optional label) before background upload
- [ ] POSTs to `/api/v1/analyze` with appropriate `type` and `ambient_source=mobile-android-share`
- [ ] Failure path: WorkManager re-queues for retry

#### Recording (foreground service)
- [ ] Recording UX matches iOS (big button, label, history list)
- [ ] MediaProjection API for screen + AudioPlaybackCapture for system audio + standard MediaRecorder for mic
- [ ] RecordingService is a foreground service (system-required for MediaProjection): persistent notification with stop button
- [ ] Recording lands on device first under app's private storage
- [ ] Upload via WorkManager with exponential backoff
- [ ] Same POST shape as iOS (with `ambient_source=mobile-android`)
- [ ] Mandatory recording indicator (foreground service notification + Android's MediaProjection overlay — system-enforced)

#### Auto-detect prompts (opt-in)
- [ ] App-state listener (UsageStatsManager or AccessibilityService — pick the lighter-touch option) observes meeting package focus
- [ ] On detection, post a notification with quick-action "Record" / "Dismiss" (10s timeout)
- [ ] Per-app remember-my-choice stored in app Settings; same auto-start / never / prompt semantics as iOS

#### On-device session cutter
- [ ] Kotlin port of the three-rule cutter (parity with iOS + desktop)
- [ ] Same SessionID propagation + idempotent PUT to dpkms

### Shared (both apps)

- [ ] Same `RawEvent`-shaped enqueue payload as desktop (Source name, Kind, Payload, Fingerprint, SessionID, Metadata)
- [ ] Same SessionID semantics — sessions cut on-device using the three-rule cutter; sessions are per-device in v1 (no cross-device merging)
- [ ] Same bus event taxonomy from ADR-069 §5 emitted to dpkms via HTTP/WS after upload
- [ ] Bearer-token auth to dpkms (per ADR-023). Mobile stores token in OS keychain (iOS Keychain / Android Keystore); never plaintext on disk
- [ ] Schema sync: a small build-time codegen step generates Swift structs + Kotlin data classes from a shared spec (the existing Go RawEvent struct, exported via JSON Schema or similar). This is the "monorepo single source of truth" benefit cited in ADR-069 §1
- [ ] CI: `mobile/ios/` builds via xcodebuild on a macOS GitHub Actions runner; `mobile/android/` builds via Gradle on an Ubuntu runner. Both gated on test pass + lint clean

### Out of scope for v1 mobile

- Live (on-device) transcription — uploads to dpkms which runs `audio.transcribe` / `video.full` server-side
- On-device knowledge graph queries — no MCP server in mobile apps; users query via the dpkms-side MCP from a desktop / web client
- Cross-device session merging — a phone session and a laptop session for the same meeting are two distinct sessions
- Multi-account support — single dpkms endpoint per app install
- Background continuous capture (clipboard, browser-history equivalents) — these are tricky on mobile due to OS background-execution restrictions; deferred
- Live transcript display during recording — Phase 8+ (whisper-on-device is real but expensive)

---

## Implementation Notes

### iOS architecture

```
ctxt-ios/
├── ContextHelpApp.swift       — main app, settings, history list
├── BroadcastExtension/         — ReplayKit broadcast extension
│   └── SampleHandler.swift     — receives audio + video samples, writes to .mp4
├── Upload/
│   └── DPKMSClient.swift       — POSTs to /api/v1/analyze with retry
└── Tests/
```

ReplayKit broadcast extensions are sandboxed, system-managed background processes. The main app coordinates start/stop; the extension handles the recording. After recording stops, the main app picks up the file from the extension's shared container and uploads.

### Android architecture

```
ctxt-android/
├── app/src/main/kotlin/io/ctxt/
│   ├── MainActivity.kt         — main UI, settings, history
│   ├── RecordingService.kt     — foreground service holding MediaProjection
│   ├── AudioCapture.kt         — AudioPlaybackCapture wrapper
│   ├── ScreenCapture.kt        — MediaProjection wrapper
│   └── upload/
│       └── DPKMSClient.kt      — POSTs to /api/v1/analyze with retry
└── tests/
```

MediaProjection requires a foreground service with a persistent notification — system-enforced. AudioPlaybackCapture requires apps to declare `ALLOW_PLAYBACK_CAPTURE` (or be on a system app allowlist for restricted apps).

### Distribution

- iOS: App Store via TestFlight first; users opt in to beta
- Android: Play Store + sideload APK for self-hosted users
- Signing: per-app certs; see respective repo READMEs

### dpkms endpoint configuration

- Manual entry: URL + token
- QR code import: desktop ctxt generates a QR encoding `{url, token, profile}`; mobile app scans
- Discovery via Bonjour / mDNS for local-network dpkms (Phase 6.5)

---

## E2E Checklist (per app)

- [ ] Install the app
- [ ] Configure dpkms endpoint (URL + token)
- [ ] Press record; verify ReplayKit / MediaProjection prompt appears (system-enforced)
- [ ] Grant; verify recording starts; verify system recording indicator
- [ ] Stop after 1 minute; verify file on device
- [ ] App backgrounds; verify upload completes (background task / WorkManager)
- [ ] Verify dpkms received POST with correct shape (`ambient_source=mobile-ios|mobile-android`)
- [ ] Verify KO created via `video.full` pipeline; transcript materializes
- [ ] Take dpkms offline; record; verify upload retries; bring dpkms back; verify upload succeeds
- [ ] OAuth / token expiration; verify clear error in app + setup pointer
- [ ] Bus event `ctxt.ambient.meeting.transcript_ready` fires server-side after pipeline completes

---

## Related Stories

- [US-0217](US-0217-meeting-capture-desktop.md) — Desktop meeting capture (sibling; same enqueue path)
- [US-0218](US-0218-meeting-redact-export.md) — Redact + export (works for mobile-captured recordings too)
- [US-0219](US-0219-mcp-agent-integration.md) — Agents query mobile-captured recordings via dpkms-side MCP

---

## Sprint

**Phase 6 / Phase 7** — separate from `ambient-capture` track; tracked under future mobile-companion tracks per [ADR-069 §Phasing](../../decisions/ADR-069-meeting-capture-source.md). Tasks **T-0520** (iOS) and **T-0521** (Android) carry the work scaffolding under the parent track for now.

---

## E2E Tests (per app, lives in respective repo)

- planned (iOS): `ctxt-ios/Tests/RecordingTests.swift::testReplayKitRecording`
- planned (iOS): `ctxt-ios/Tests/UploadTests.swift::testRetryOnNetworkFailure`
- planned (iOS): `ctxt-ios/Tests/AuthTests.swift::testTokenAuth`
- planned (Android): `ctxt-android/app/src/test/kotlin/RecordingServiceTests.kt::testMediaProjectionRecording`
- planned (Android): `ctxt-android/app/src/test/kotlin/UploadTests.kt::testWorkManagerRetry`
- planned (Android): `ctxt-android/app/src/test/kotlin/AuthTests.kt::testTokenAuth`
