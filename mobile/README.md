# Mobile companions

Native iOS and Android apps that bring ctxt to mobile. Live in the monorepo per [ADR-069 §1](../docs/decisions/ADR-069-meeting-capture-source.md).

```
mobile/
├── ios/          # Swift app + ShareExtension + ReplayKit BroadcastExtension
└── android/      # Kotlin app + share intent receiver + foreground RecordingService
```

> **Status: scaffolding only.** The substrate (ADR-066/067/068/069 + all of `internal/ambient/`) is complete and ready; the Swift / Kotlin implementations are Phase 6/7 deferred work. See [`../docs/stories/capture/US-0222-mobile-meeting-companion.md`](../docs/stories/capture/US-0222-mobile-meeting-companion.md) for the acceptance criteria.

## Why monorepo

Decided 2026-05-06 (revising the earlier "separate repos" framing in ADR-069):

- **One source of truth for the wire contract.** `RawEvent`, `RecordOptions`, bus topics, session shape — all defined once in Go (`internal/ambient/ambient.go`, `internal/ambient/meeting/meeting.go`, `pkg/pluginapi/`). A build-time codegen step (Phase 6 work) generates Swift structs + Kotlin data classes from the Go source, eliminating three-way schema drift.
- **Atomic cross-language refactors.** Adding a new field to `RawEvent.Metadata` is one PR touching Go + Swift + Kotlin, not three coordinated PRs.
- **Easier end-to-end tests.** A test can spin up dpkms, post a recording from a mobile simulator, and assert the right KnowledgeObject lands. Across separate repos this would be CI-orchestration ceremony.
- **Fewer GitHub repos** (releases, CI, security alerts × 1 vs × 3).

Trade-offs accepted:
- **Heterogeneous CI**: Swift on macOS runner, Kotlin on Ubuntu runner, Go on either. Workflows isolated per platform; signing secrets scoped to the right workflows only.
- **Larger repo size**: mobile recordings + screenshots used in tests bloat git history. Mitigated via `.gitignore` (test fixtures aren't checked in) and Git LFS if it ever matters.

## v1 mobile scope (per platform)

| Feature | iOS implementation | Android implementation |
|---|---|---|
| **Share to ctxt** | Share Extension target | Activity for `ACTION_SEND` / `ACTION_SEND_MULTIPLE` intents |
| **Meeting recording** | ReplayKit broadcast extension | MediaProjection + AudioPlaybackCapture in foreground service |
| **Auto-detect prompts** | App-state observer + UNUserNotification | UsageStatsManager + Notification with quick actions |
| **On-device sessions** | Swift port of `internal/ambient/session/cutter.go` | Kotlin port of same |
| **Upload** | Background URLSession | WorkManager with exponential backoff |
| **Auth** | Bearer token in iOS Keychain | Bearer token in Android Keystore |
| **Endpoint config** | App settings: dpkms URL + token | Same |

POSTs to existing `/api/v1/analyze` (per ADR-056); no new dpkms-side work needed beyond what's shipped. dpkms runs `audio.transcribe` / `video.full` pipelines server-side and emits `transcript_ready` when done; mobile apps display the transcript afterwards.

## Out of v1 scope (deferred to Phase 8+)

- On-device transcription (Whisper-on-device is real but expensive; defer)
- Cross-device session merging (phone + laptop sessions for the same meeting are two sessions in v1)
- Multi-account support (one dpkms endpoint per install)
- Background continuous capture (clipboard / browser-history equivalents — tricky on mobile due to OS background-execution restrictions)
- MCP server on mobile (mobile users query via dpkms-side MCP from a desktop/web client)

## When Phase 6/7 starts

1. Decide minimum supported OS (current draft: iOS 17+, Android 10+).
2. Set up `mobile/ios/Package.swift` (or Xcode project) + `mobile/android/build.gradle.kts`.
3. Add codegen step that emits `mobile/ios/Generated/Wire.swift` and `mobile/android/app/src/main/kotlin/io/ctxt/wire/Wire.kt` from the Go RawEvent / RecordOptions / bus topics.
4. Implement Share extensions first (smallest end-to-end surface; validates the auth + upload path).
5. Implement recording (ReplayKit / MediaProjection).
6. Implement on-device session cutter (port of `internal/ambient/session/`).
7. Implement auto-detect prompts.
8. App Store / Play Store submission ceremony.

## See also

- [ADR-069 — Meeting Capture Source](../docs/decisions/ADR-069-meeting-capture-source.md) (§1 for monorepo decision; §3 for capture mechanism per OS)
- [US-0222 — Mobile Meeting Companion](../docs/stories/capture/US-0222-mobile-meeting-companion.md) (full acceptance criteria)
- [ADR-067 — Session/WorkUnit](../docs/decisions/ADR-067-session-workunit.md) (cutter algorithm; mobile ports the same three rules)
- [`internal/ambient/session/cutter.go`](../internal/ambient/session/cutter.go) (Go reference implementation of the cutter — the Swift / Kotlin ports follow this line-by-line)
