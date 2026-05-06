# ctxt iOS app

Native iOS companion app for ctxt. Phase 6 deferred work — substrate is ready, scaffold here pending implementation.

> See [`../README.md`](../README.md) for the monorepo rationale and overall mobile scope.
> Acceptance criteria: [`../../docs/stories/capture/US-0222-mobile-meeting-companion.md`](../../docs/stories/capture/US-0222-mobile-meeting-companion.md).

## Status

**Scaffolding only.** No Swift code yet. This README documents the planned architecture so an implementer (Phase 6) can pick up where this left off.

## Targets

When Phase 6 implementation lands, the Xcode project (`Ctxt.xcodeproj`) will define three targets:

### App (`Ctxt`)

The main app: settings (dpkms endpoint URL + bearer token in Keychain), recording UI (big record button + history list + per-recording status), auto-detect prompt configuration.

- Bundle id: `io.ctxt.Ctxt`
- Min iOS: 17.0
- Frameworks: SwiftUI, UserNotifications, KeychainServices, AVFoundation
- App Group: `group.io.ctxt.shared` (shared with extensions for token + queue)

### ShareExtension (`CtxtShare`)

Appears in the iOS share sheet for URL / text / image / audio / video / file content types. Minimal UI (cancel + post + optional label/note). POSTs to `/api/v1/analyze` with `ambient_source=mobile-ios-share`.

- Bundle id: `io.ctxt.Ctxt.Share`
- `NSExtensionActivationRule` configured for the supported UTIs
- Reads bearer token from App Group keychain
- Failure path: serialise to App Group queue; main app's "Pending" view retries

### BroadcastExtension (`CtxtBroadcast`)

ReplayKit broadcast extension that captures screen + system audio + mic. Recording lands in App Group's Documents directory; main app uploads via `URLSession` background task after `broadcastFinished`.

- Bundle id: `io.ctxt.Ctxt.Broadcast`
- `RPBroadcastSampleHandler` subclass; samples written to `.mp4` via `AVAssetWriter`
- File handoff to main app via App Group container

## On-device session cutter

Swift port of [`internal/ambient/session/cutter.go`](../../internal/ambient/session/cutter.go). Same three rules:

```swift
struct CutterConfig {
    var gapMinutes: Int = 5
    var softCutMinutes: Int = 3
    var maxSessionHours: Int = 2
    var recentSwitchWindow: TimeInterval = 120  // 2 minutes
}
```

Same default values as desktop. Same algorithm (idle / soft-cut + frequent-switching / timeout). Same session id format (`sess_<12-hex>`). Same idempotent PUT to `/api/v1/sessions/{id}` on open + close.

Foreground signal source: app-state observation via `UIApplication.didBecomeActiveNotification` + Shortcut intents. ReplayKit recording start/stop also drives the cutter.

## Auto-detect prompt

Configurable list of meeting bundle-ids in app Settings. Defaults from ADR-069 §Phase 5:

```swift
let defaultMeetingBundles = [
    "us.zoom.videomeetings",
    "com.microsoft.teams2",
    "com.tinyspeck.chatlyio",
    "com.hammerandchisel.discord",
    "com.apple.facetime"
]
```

When the user activates a known meeting app (observed via app-state listener), post a `UNUserNotification` with "Record this meeting?" and a 10-second confirmation window. Per-app remember-my-choice (auto-start / never / prompt) stored in `UserDefaults`.

NEVER starts recording without explicit user confirmation OR an explicit `auto-start` per-app rule.

## Wire format

`RawEvent`, `RecordOptions`, and bus topics are defined in Go and mirrored to Swift via build-time codegen (Phase 6 work). Generated file: `Generated/Wire.swift`. The codegen tool — likely `gomobile bind` or a custom JSON-Schema-driven generator — runs as part of the iOS build's prebuild script.

## CI

```yaml
# .github/workflows/mobile-ios.yml (Phase 6 work)
on:
  push:
    paths: ['mobile/ios/**']
  pull_request:
    paths: ['mobile/ios/**']

jobs:
  build:
    runs-on: macos-14
    steps:
      - uses: actions/checkout@v4
      - run: xcodebuild test -project mobile/ios/Ctxt.xcodeproj -scheme Ctxt -destination 'platform=iOS Simulator,name=iPhone 15'
      - run: xcodebuild build -project mobile/ios/Ctxt.xcodeproj -scheme CtxtShare
      - run: xcodebuild build -project mobile/ios/Ctxt.xcodeproj -scheme CtxtBroadcast
```

Signing secrets: scoped to a separate release workflow that runs only on tagged commits.

## Distribution

App Store. TestFlight beta first; users opt in. Self-hosting users with a self-hosted dpkms can sideload via Xcode (Apple's per-device signing certificate); Play Store equivalent on Android via APK sideload.
