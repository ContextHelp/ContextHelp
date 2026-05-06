# ctxt Android app

Native Android companion app for ctxt. Phase 7 deferred work — substrate is ready, scaffold here pending implementation.

> See [`../README.md`](../README.md) for the monorepo rationale and overall mobile scope.
> Acceptance criteria: [`../../docs/stories/capture/US-0222-mobile-meeting-companion.md`](../../docs/stories/capture/US-0222-mobile-meeting-companion.md).

## Status

**Scaffolding only.** No Kotlin code yet. This README documents the planned architecture so an implementer (Phase 7) can pick up where this left off.

## Components

When Phase 7 implementation lands, the Gradle project (`build.gradle.kts`) will define:

### Main activity (`MainActivity`)

App's primary UI: settings (dpkms endpoint URL + bearer token in Android Keystore), recording UI (big record button + history list + per-recording status), auto-detect prompt configuration.

- App ID: `io.ctxt.android`
- Min SDK: 29 (Android 10) — required for AudioPlaybackCapture
- Target SDK: 34 (Android 14)
- Built with Jetpack Compose
- Token storage via `EncryptedSharedPreferences` + Android Keystore

### Share intent receiver (`ShareActivity`)

Activity registered for `ACTION_SEND` and `ACTION_SEND_MULTIPLE` intents with text / uri / image / audio / video / file MIME types. Minimal UI (post button + optional label) before background upload via WorkManager.

```xml
<!-- AndroidManifest.xml -->
<activity android:name=".ShareActivity" android:exported="true">
    <intent-filter>
        <action android:name="android.intent.action.SEND" />
        <category android:name="android.intent.category.DEFAULT" />
        <data android:mimeType="*/*" />
    </intent-filter>
    <intent-filter>
        <action android:name="android.intent.action.SEND_MULTIPLE" />
        <category android:name="android.intent.category.DEFAULT" />
        <data android:mimeType="*/*" />
    </intent-filter>
</activity>
```

POSTs to `/api/v1/analyze` with `ambient_source=mobile-android-share`.

### RecordingService (foreground service)

Foreground service holding `MediaProjection` + `AudioPlaybackCapture` + `MediaRecorder`. Foreground service is **system-required** for MediaProjection on Android 10+; the persistent notification (with stop button) is the user-visible recording indicator.

```kotlin
class RecordingService : Service() {
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForeground(NOTIFICATION_ID, buildRecordingNotification())
        // ... MediaProjection setup ...
        return START_NOT_STICKY
    }
}
```

Recording lands on device first under app's private storage; upload via WorkManager after `stopRecording`.

### Wire library

Auto-generated Kotlin data classes mirroring Go's `RawEvent`, `RecordOptions`, bus topics. Generated file: `app/src/main/kotlin/io/ctxt/wire/Wire.kt`. Codegen step (Phase 7 work) runs as a Gradle task; same source-of-truth as iOS (Go source → Swift + Kotlin).

## On-device session cutter

Kotlin port of [`internal/ambient/session/cutter.go`](../../internal/ambient/session/cutter.go). Same three rules, same defaults, same algorithm. Same session id format. Same idempotent PUT to `/api/v1/sessions/{id}`.

Foreground signal source: `UsageStatsManager.queryUsageStats` (most permissions-friendly) or `AccessibilityService` (more invasive but real-time). Phase 7 picks one based on practical testing — `UsageStatsManager` is the lighter touch but has a polling latency; `AccessibilityService` is real-time but requires a scary permission prompt.

## Auto-detect prompt

Configurable list of meeting package names in app Settings. Defaults:

```kotlin
val defaultMeetingPackages = listOf(
    "us.zoom.videomeetings",
    "com.microsoft.teams",
    "com.tinyspeck.chatlyio",
    "com.discord",
    "com.google.android.apps.tachyon",  // Google Meet
)
```

When the user activates a known meeting app (observed via the foreground signal source), post a notification with quick-action buttons "Record" / "Dismiss" and a 10-second timeout. Per-app remember-my-choice stored in `EncryptedSharedPreferences`.

NEVER starts recording without explicit user confirmation OR an explicit `auto-start` per-app rule.

## Permissions

Required (declared in `AndroidManifest.xml`, prompted at runtime):
- `RECORD_AUDIO` — mic mixing
- `FOREGROUND_SERVICE` + `FOREGROUND_SERVICE_MEDIA_PROJECTION` — recording service
- `POST_NOTIFICATIONS` (Android 13+) — auto-detect prompts + recording indicator
- `INTERNET` — upload to dpkms

Optional (for auto-detect):
- `PACKAGE_USAGE_STATS` (special permission; user navigates to Settings → Apps → Special access)

NOT required:
- Storage permissions — recordings live in app's private storage (not user-visible folders)
- `ALLOW_PLAYBACK_CAPTURE` from other apps — AudioPlaybackCapture is opt-in by the app being captured. Most meeting apps don't opt out of capture (audio inside the meeting app is captured), but some apps (DRM-protected media) do opt out — those will record silence.

## CI

```yaml
# .github/workflows/mobile-android.yml (Phase 7 work)
on:
  push:
    paths: ['mobile/android/**']
  pull_request:
    paths: ['mobile/android/**']

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-java@v4
        with:
          distribution: 'temurin'
          java-version: '17'
      - run: ./mobile/android/gradlew --project-dir mobile/android test
      - run: ./mobile/android/gradlew --project-dir mobile/android assembleDebug
      - run: ./mobile/android/gradlew --project-dir mobile/android lint
```

Signing secrets (Play Console): scoped to a separate release workflow.

## Distribution

Play Store. Internal testing track first (Google's equivalent of TestFlight); users opt in. Self-hosting: APK sideload (Settings → Install unknown apps).
