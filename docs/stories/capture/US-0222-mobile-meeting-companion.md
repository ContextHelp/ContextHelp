---
status: paper
adr: ADR-069
task: T-0520, T-0521
phase: 6, 7
---

# US-0222: Mobile Meeting Companion (iOS + Android)

**System Types:** ctxt-ios, ctxt-android (separate repos)
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

> **Phase 6/7 — out of v1 scope.** Repo structure (monorepo `mobile/{ios,android}/` vs. separate `ctxt-ios` / `ctxt-android` repos) is **TBD at Phase 6/7 start** — see [ADR-069 §1](../../decisions/ADR-069-meeting-capture-source.md). The substrate's enqueue API + bus event taxonomy is the binding contract; whichever repo structure ships, the wire is unchanged.

---

## User Goal

As a knowledge worker who takes calls on iPhone, iPad, or Android, I want a small native companion app that records meetings on my phone (audio + video) and POSTs the recording to my configured ctxt instance (local or remote dpkms) — so meetings on the commute, on-the-go check-ins, and family calls flow into the same knowledge graph as desktop meetings.

---

## Context

[ADR-069](../../decisions/ADR-069-meeting-capture-source.md) splits meeting capture cleanly: desktop ships in `ctxd` (Go binary, OS-specific cgo bridges to ScreenCaptureKit / Graphics Capture / xdg-desktop-portal). Mobile is fundamentally different — Go does not run usefully on iOS at all, runs awkwardly on Android, and the OS APIs (ReplayKit on iOS, MediaProjection + AudioPlaybackCapture on Android) are designed for in-app integration via native frameworks.

Honest split: ship two thin native apps in separate repos. They share **nothing** structurally with `ctxd` — but they emit the same bus event taxonomy (over HTTP/WS to dpkms) and POST to the same `/api/v1/analyze` endpoint with the same payload shape. dpkms doesn't need to know they exist; the substrate absorbs them transparently.

The apps are intentionally minimal: record locally, retry on transient network failures, POST when ready. No on-device transcript pipeline (that runs on dpkms via `audio.transcribe` / `video.full`); no on-device knowledge graph (that lives in dpkms).

---

## Acceptance Criteria

### iOS app (`ctxt-ios`, Phase 6, T-0520)

- [ ] Swift app (repo location TBD per ADR-069 §1); distributed via App Store
- [ ] iOS 17+ minimum (ReplayKit broadcast extensions stable)
- [ ] Recording UX:
    - Big "record" button on home screen
    - Live duration timer + stop button while recording
    - Optional label entry before start (or after, in editable field)
- [ ] ReplayKit broadcast extension captures screen + audio (system + mic)
- [ ] Recording lands on device first under app's Documents directory
- [ ] Background-task API used for upload after recording stops (continues even if app backgrounded)
- [ ] Configures dpkms endpoint via in-app settings (URL + bearer token if `mcp.host=0.0.0.0`)
- [ ] POSTs recording to `/api/v1/analyze` with `pipeline=video.full`, `ambient_source=mobile-ios`, label, device timestamp
- [ ] Retry logic for transient failures (exponential backoff; cap at 24h)
- [ ] In-app history list of past recordings (with upload status)
- [ ] Mandatory recording indicator (iOS shows red status-bar pill during ReplayKit recording — system-enforced)
- [ ] Privacy: explicit-trigger only; no auto-detect; no background recording

### Android app (`ctxt-android`, Phase 7, T-0521)

- [ ] Kotlin app (repo location TBD per ADR-069 §1); distributed via Play Store
- [ ] Android 10+ minimum (AudioPlaybackCapture API)
- [ ] Recording UX matches iOS (big button, label, history list)
- [ ] MediaProjection API for screen + AudioPlaybackCapture for system audio
- [ ] Foreground service for long-running recording (system-required for MediaProjection)
- [ ] Recording lands on device first under app's private storage
- [ ] Upload via WorkManager (Android's background-task API)
- [ ] Same dpkms endpoint configuration UX
- [ ] Same POST shape (`pipeline=video.full`, `ambient_source=mobile-android`)
- [ ] Same retry logic
- [ ] Mandatory recording indicator (Android shows persistent foreground-service notification — system-required)
- [ ] Privacy posture matches iOS (explicit-trigger only)

### Shared (both apps)

- [ ] Same `RawEvent`-shaped enqueue payload as desktop
- [ ] Same SessionID semantics — apps treat each recording as its own session (no cross-session continuity v1)
- [ ] Same bus event taxonomy emitted to dpkms over HTTP after upload (`ctxt.ambient.meeting.requested|started|stopped|enqueued|transcript_ready` etc.)
- [ ] OAuth or token-based auth to dpkms (per ADR-023; see [US-0219](US-0219-mcp-agent-integration.md) for the auth model)
- [ ] Documentation lives in respective repos; this story is the boundary spec

### Out of scope for v1 mobile

- Live transcription on device (deferred; dpkms handles via `audio.transcribe` / `video.full`)
- On-device knowledge graph queries (no MCP server in mobile apps; users running ctxt mobile rely on dpkms-side MCP via web/desktop)
- Cross-machine session continuity (a meeting on phone is one session; doesn't merge with a desktop session)
- Auto-detect of meeting apps on mobile (technically harder; explicit-trigger only)

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
