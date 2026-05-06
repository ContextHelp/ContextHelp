# Meeting Capture

Record video calls (Zoom, Meet, Teams, FaceTime, Discord, Slack Huddles) and route the audio + video through ctxt's existing transcription + frame-OCR pipelines. Per [ADR-069](decisions/ADR-069-meeting-capture-source.md).

> Companion to [`ambient.md`](ambient.md) (parent ambient daemon). For the task-oriented walkthrough, see [`manual/workflows/meeting-capture.md`](manual/workflows/meeting-capture.md). For ops, see `~/.ops/runbooks/meeting-capture.md`.

## What it does

- Captures **system audio + window framebuffer** (or audio-only) on macOS 13+, Windows 10+, and Linux (Wayland-first, X11 fallback).
- Routes the recording to existing pipelines: `audio.transcribe` (diarization, alignment, sectioning) for audio-only, or `video.full` (audio_extractor → audio_transcriber → frame_sampler → scene_detector → frame_ocr → timeline_assembler) for full mode.
- Attaches the active SessionID ([ADR-067](decisions/ADR-067-session-workunit.md)) so the meeting groups with prep clipboard captures and post-meeting file edits.
- Mandatory **recording indicator** (red menubar/tray pulse) — the OS-native indicator on macOS (orange/purple dot) is also visible.
- First-class **redact-as-supersede**: remove a sensitive segment from transcript + media after the fact via `ctxt capture meeting redact`.

## Critical: explicit-trigger only

Recording **never** starts without your explicit action — hotkey, CLI invocation, or confirmation of the opt-in auto-detect prompt. There is no always-on / silent auto-record mode in v1. This is structural per ADR-069 §Rationale 1, side-stepping the consent-law exposure that always-on tools (Otter, Granola) carry.

## Quick start

```bash
# Start a recording (full audio + video → video.full pipeline)
ctxt capture meeting start --label "Q3 planning"

# Audio-only mode (smaller files, faster pipeline → audio.transcribe)
ctxt capture meeting start --label "1:1 with Alice" --audio-only

# Stop (or use the same hotkey, default ⇧⌘M / Ctrl+Shift+M)
ctxt capture meeting stop

# Browse + view
ctxt capture meeting list --since today
ctxt capture meeting show <id>

# Redact a sensitive segment
ctxt capture meeting redact <id> --segment 14:55-14:58 --reason "off-record"

# Export the transcript
ctxt capture meeting export <id> --format md
ctxt capture meeting export <id> --format srt > meeting.srt
ctxt capture meeting export <id> --format vtt > meeting.vtt
```

Full walkthrough: [`manual/workflows/meeting-capture.md`](manual/workflows/meeting-capture.md).

## Per-OS first-run permission flow

The first time you record on each OS, you'll see a system permission dialog. These appear exactly once per OS per major version of `ctxd`:

| OS | Dialog | What to grant |
|---|---|---|
| **macOS 13+** | System Settings → Privacy & Security → Screen Recording | Add `ctxd` (path: `~/.local/bin/ctxd` for user installs, `/opt/homebrew/bin/ctxd` for Homebrew) |
| **macOS 13+** | System Settings → Privacy & Security → Microphone (mic mixing) | Add `ctxd` |
| **Windows 10+** | Permission flyout when Graphics Capture API is invoked | Click "Allow" |
| **Linux (Wayland)** | xdg-desktop-portal dialog | Pick the meeting window or full screen |
| **Linux (X11)** | None (XComposite + XDamage have no permission system) | — |

If you deny, ctxt will surface a clear error with a setup pointer; revoke + re-grant if you change your mind.

## Recording indicator

The recording indicator is **mandatory** — its absence during an active recording is a bug, and a kit/policy CEL watchdog rule asserts `ctxt.ambient.meeting.indicator_displayed` fires within 500ms of `ctxt.ambient.meeting.started`. If it doesn't, recording auto-pauses.

- **macOS**: red pulsing menubar icon + the OS's orange/purple recording dot in the menubar
- **Windows**: red pulsing tray icon + Graphics Capture overlay
- **Linux**: red pulsing tray icon (compositor-dependent for OS-native indicators)

## Auto-detect (Phase 5, opt-in)

When enabled, `ctxd` watches the foreground source for known meeting bundle-ids (Zoom, Teams, Slack, Discord, FaceTime, browser tabs on `meet.google.com`/`*.zoom.us/j/*`) and prompts you to start recording. Default 10-second timeout; auto-dismiss if no action.

```yaml
ambient:
  meeting:
    auto_detect:
      enabled: true                # opt-in (false by default)
      prompt_timeout: 10s
      bundles:
        - id: us.zoom.xos
          mode: full
        - id: com.microsoft.teams2
          mode: full
        - id: com.tinyspeck.slackmacgap
          mode: audio_only
```

Per-app remember-my-choice via kit/policy CEL rule: "always start when Zoom is focused" or "never auto-prompt for Discord". CEL rules are written to `$XDG_CONFIG_HOME/contexthelp/policy.d/`. **NEVER starts recording without explicit user confirmation OR an explicit per-app `auto-start` rule.**

## Redact + export

```bash
# Redact a 3-second segment from transcript + media
ctxt capture meeting redact <object_id> --segment 14:55-14:58 --reason "off-record exchange"
```

This:
1. Writes a new KnowledgeObject revision with the segment's transcript text removed.
2. Marks the original revision `superseded_by` the new one (audit trail preserved).
3. Deletes the corresponding media segment from local disk.
4. If S3 archive is enabled, removes from the archive too.

Search results show only the latest (redacted) revision. Original is queryable only via `ctxt show <id> --include-superseded` for audit.

```bash
# Export
ctxt capture meeting export <id> --format md      # Markdown (speaker-tagged + timestamps)
ctxt capture meeting export <id> --format srt     # SRT subtitles (HH:MM:SS,mmm)
ctxt capture meeting export <id> --format vtt     # WebVTT (HH:MM:SS.mmm)
```

## Storage retention

Meeting media files (~500 MB / hour at 1080p) get a separate retention tier from the substrate's small-event buffer:

```yaml
ambient:
  meeting:
    media_retention_hours: 48          # raw file deleted after this
    local_max_gb: 20                   # LRU eviction within cap
    s3_archive: false                  # set true for compliance retention
    s3_archive_retention_years: 7      # via S3 lifecycle policy when enabled
```

The KnowledgeObject (transcript + frame OCR + mentions + embeddings) lives forever in dpkms; only the raw media file is evicted.

## S3 archive (compliance retention)

For legal/compliance scenarios (interviews, court-of-record meetings, regulated industries):

```yaml
ambient:
  meeting:
    s3_archive: true
  buffer:
    backend: s3
    s3:
      bucket: ctxt-meeting-archive
      endpoint: https://s3.us-east-1.amazonaws.com
      region: us-east-1
      # credentials via standard env vars or kit/runtime/secrets
```

Once S3 archive is configured, media files are uploaded to S3 BEFORE local deletion. Provider lifecycle policies handle long-term retention (e.g. 7 years for legal hold).

## Privacy & consent

Recording meetings has consent implications that vary by jurisdiction. ctxt provides the capture mechanism; the legal context is the user's:

- **Two-party-consent jurisdictions** (US: CA, FL, IL, MA, MD, MT, NV, NH, PA, WA; most of the EU under GDPR): all parties on the call must be aware they're being recorded.
- **One-party-consent jurisdictions** (US: most other states; Canada): the recording party's consent is sufficient.

ctxt does not enforce these laws — but it does:

- **Default to off** (explicit-trigger only; no always-on).
- **Show a recording indicator** that's hard to miss.
- **Ship redact** for post-hoc removal.
- **Emit bus events** that compliance systems can audit (`ctxt.ambient.meeting.requested`, `started`, `indicator_displayed`, `stopped`, `transcript_ready`, `redact_completed`).

The DMLP state-by-state survey is a good starting point for US: <https://www.dmlp.org/legal-guide/state-law-recording>. **This is not legal advice.**

## Bus events

22+ topics covering every state transition. See the full taxonomy in [ADR-069 §5](decisions/ADR-069-meeting-capture-source.md). Common ops uses:

```bash
ctxt watch --topic 'ctxt.ambient.meeting.#'                    # everything
ctxt watch --topic 'ctxt.ambient.meeting.transcript_ready'    # alert when done
ctxt watch --topic 'ctxt.ambient.meeting.policy_vetoed'       # audit veto fires
ctxt watch --topic 'ctxt.ambient.meeting.indicator_displayed' # watchdog signal
```

## Failure modes

| Symptom | Cause | Recovery |
|---|---|---|
| Recording stopped after 5s with `capture_failed` | Headphones unplugged mid-recording (macOS) | Re-record; ctxd surfaces partial-file path |
| Permission revoked mid-recording | macOS Privacy revoke | Re-grant; restart; re-record |
| Transcript is gibberish | Whisper struggles with accent / low audio | Try `meeting.whisper_model: large-v3` (slower, more accurate) |
| Disk filled up | Default 20 GB cap exceeded | Reduce `local_max_gb`, lower `media_retention_hours`, OR enable `s3_archive` |
| Auto-detect not firing | Bundle not in default list | Add to `ambient.meeting.auto_detect.bundles` |

For ops scenarios (daemon recovery, S3 credential rotation, compliance retention setup), see `~/.ops/runbooks/meeting-capture.md`.

## Related

- [`ambient.md`](ambient.md) — parent ambient daemon
- [`manual/workflows/meeting-capture.md`](manual/workflows/meeting-capture.md) — task walkthrough
- [`architecture/ambient-capture.md`](architecture/ambient-capture.md) — meeting source architecture (under "Meeting capture (specialized source)")
- [`decisions/ADR-069-meeting-capture-source.md`](decisions/ADR-069-meeting-capture-source.md)
- [`diagrams/ambient/069-recording-state.mmd`](diagrams/ambient/069-recording-state.mmd) — state machine
- [`diagrams/ambient/069-multi-platform.mmd`](diagrams/ambient/069-multi-platform.mmd) — multi-platform architecture
- [`diagrams/ambient/069-recording-sequence.mmd`](diagrams/ambient/069-recording-sequence.mmd) — end-to-end recording flow
