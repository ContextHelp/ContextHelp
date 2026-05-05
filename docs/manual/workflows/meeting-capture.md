# Workflow: Meeting Capture

## Goal

Record video calls (Zoom, Meet, Teams, FaceTime, Discord, Slack Huddles) on your local machine, route the recording through ctxt's existing `audio.transcribe` or `video.full` pipelines, and end up with a searchable diarized transcript + frame-OCR'd shared screens, attached to the active work session.

## Scope

- Explicit-trigger recording on macOS 13+, Windows 10+, Linux (Wayland-first, X11 fallback)
- Audio-only or full (audio + video) modes
- OS permission flow on first use
- Recording indicator + pause/resume
- Auto-detect prompt (Phase 5, opt-in) when meeting apps come to focus
- Redact-as-supersede for post-hoc segment removal
- Export transcripts as Markdown / SRT / VTT
- Mobile companion apps (iOS / Android) — Phase 6/7, separate apps

## Primary stories

- `US-0217` (desktop meeting capture)
- `US-0218` (meeting redact + export)
- `US-0221` (auto-detect prompt)
- `US-0222` (mobile meeting companion)

## Prerequisites

1. `ctxd` running (see [ambient-capture.md](ambient-capture.md)). Meeting capture is a source within the ambient substrate.
2. **macOS 13+** for full feature support (ScreenCaptureKit). macOS 12 users: install BlackHole or Loopback.app for system-audio capture; document this separately.
3. **Windows 10 1903+** for Graphics Capture API.
4. **Linux**: any Wayland compositor with xdg-desktop-portal ScreenCast support (GNOME, KDE, Sway, Hyprland). X11 fallback works but is second-class.
5. dpkms reachable, `audio.transcribe` and `video.full` pipelines registered (already shipped — see `internal/pipeline/builtins/`).

## Procedure

### Step 1: Start a recording

Audio + video (default; routes to `video.full`):

```bash
ctxt capture meeting start --label "Q3 planning"
```

Audio-only (smaller files, faster pipeline; routes to `audio.transcribe`):

```bash
ctxt capture meeting start --label "1:1 with Alice" --audio-only
```

Target a specific window (instead of full screen):

```bash
ctxt capture meeting start --label "demo review" --window-title "Zoom"
```

Set a hard duration cap (recording auto-stops):

```bash
ctxt capture meeting start --label "weekly standup" --duration 30m
```

Hotkey alternative (configurable; default ⇧⌘M / Ctrl+Shift+M): start/stop with one keypress.

### Step 2: First-time OS permission flow

The first recording on each OS triggers a system permission dialog:

- **macOS**: System Settings → Privacy & Security → Screen Recording → grant `ctxd`. (And Microphone for mic mixing.)
- **Windows**: Permission flyout when the Graphics Capture API is invoked. Click "Allow."
- **Linux (Wayland)**: xdg-desktop-portal dialog asking which screen/window to share. Pick the meeting window.

These dialogs happen exactly once per OS per major version of `ctxd`. Subsequent recordings start without prompts.

### Step 3: Recording indicator

While a recording is active, you'll see one of:

- **Red pulsing menubar/tray icon** (`ctxd`'s default).
- **OS-native indicator** if available (macOS shows an orange/purple dot in the menubar for screen capture).

The indicator is **mandatory** — its absence during an active recording is a bug. A kit/policy CEL watchdog rule asserts `ctxt.ambient.meeting.indicator_displayed` fires within 500ms of `ctxt.ambient.meeting.started`; if not, recording pauses automatically.

### Step 4: Stop the recording

```bash
ctxt capture meeting stop
```

Or use the same hotkey, or auto-stop fires when the meeting bundle (Zoom, Meet, etc.) closes.

`ctxd` finalizes the file, emits `ctxt.ambient.meeting.stopped` with duration and bytes, and POSTs to `/api/v1/analyze` with the file path, suggested pipeline (`audio.transcribe` / `video.full`), and the active SessionID.

### Step 5: Watch the transcript materialize

```bash
ctxt capture meeting status
```

Sample output during pipeline run:

```
Meeting sess_a1b2c3d4e5f6
  Recorded: 14:32 → 15:38 (66 min, 1.2 GB)
  Pipeline: video.full (running)
    audio_extractor: ✓
    audio_transcriber: ✓ (3 speakers detected)
    speaker_diarizer: ✓
    timestamp_aligner: ✓
    frame_sampler: 87% (sampled 142/162 frames)
    scene_detector: pending
    frame_ocr: pending
    timeline_assembler: pending
  Estimated completion: 4 min
```

When done, you can find the meeting:

```bash
ctxt list --type meeting --since today
ctxt show <object_id>
ctxt session show sess_a1b2c3d4e5f6
```

### Step 6: Auto-detect prompt (Phase 5, opt-in)

Enable in config:

```yaml
ambient:
  meeting:
    auto_detect:
      enabled: true
      bundles:
        - us.zoom.xos
        - com.microsoft.teams2
        - com.tinyspeck.slackmacgap   # Slack Huddles
        - com.electron.discord
      prompt_timeout: 10s
      remember_choice_per_app: true   # via kit/policy
```

When a known meeting bundle comes to focus, you'll get an OS notification + hotkey: "Record this meeting? [Y/N]". Default 10-second timeout (auto-dismiss). **Never** starts recording without explicit confirmation.

### Step 7: Redact a segment

You realize at minute 23 someone said something sensitive that's now in the transcript. Remove it:

```bash
ctxt capture meeting redact <object_id> --segment 14:55-14:58 --reason "off-record exchange"
```

This:
1. Writes a new KnowledgeObject revision with that 3-minute window of transcript text removed.
2. Marks the original revision `superseded_by` the new one (audit trail preserved).
3. Deletes the corresponding 3-minute window from the local media file.
4. If S3 archive is enabled, removes the segment from the archive too.

Search results from `ctxt find` will only see the redacted version. The original is queryable only via `ctxt show <object_id> --include-superseded` for audit.

### Step 8: Export the transcript

```bash
ctxt capture meeting export <object_id> --format md
ctxt capture meeting export <object_id> --format srt > meeting.srt
ctxt capture meeting export <object_id> --format vtt > meeting.vtt
```

Markdown export includes speaker labels, timestamps, and frame-OCR'd shared-screen content as inline blockquotes. SRT/VTT are subtitle-format compatible with most video players.

### Step 9: Storage retention

Media files (the raw `.mov`/`.mp4`/`.webm`) get their own retention tier — separate from the substrate's small-event buffer:

```yaml
ambient:
  meeting:
    media_retention_hours: 48      # default; raw file deleted after this
    local_max_gb: 20               # LRU eviction within cap
    s3_archive: false              # set true for compliance retention
```

The KnowledgeObject (transcript + frame OCR + mentions + embeddings) lives forever in dpkms; only the raw media file is evicted.

## Common patterns

### "Record everything in a 1:1 without screen video"

```bash
ctxt capture meeting start --label "1:1 weekly" --audio-only
```

Smaller file, faster pipeline, no shared-screen OCR (you don't need it for talk-only meetings).

### "Compliance retention for legal-bound meetings"

```yaml
ambient:
  meeting:
    s3_archive: true
    s3_archive_retention_years: 7
```

Plus a per-profile kit/policy rule that forces archive on for `profile=legal`.

### "Quick redact via hotkey during a meeting"

Bind a hotkey to "mark the last 30 seconds for redaction":

```bash
ctxt capture meeting mark-redact --back 30s
```

This stages a redact request that applies after recording ends and the transcript is ready.

## Outputs to validate

- KnowledgeObject of type `meeting` created
- Transcript present with speaker labels
- Mentions extracted (people named, projects discussed)
- SessionID attached and visible in `ctxt session show`
- Frame OCR present for shared-screen segments (full mode)
- Media file deleted at retention TTL (or archived to S3)

## Common failure modes

### "Permission denied when starting"

- Check OS Privacy/Security settings; revoke and re-grant
- Restart `ctxd` after granting

### "Recording stopped after 5 seconds with capture_failed"

- Headphones disconnected mid-recording (macOS): graceful but stops
- Bluetooth audio device swapped: same
- `ctxt capture meeting status` shows partial transcript anyway (whatever was captured runs through pipeline)

### "Transcript is gibberish"

- Whisper struggles with very accented speech or low audio quality
- Try `--whisper-model large-v3` in config (slower but more accurate)
- For non-English meetings, set `ambient.meeting.language: fr` (or auto)

### "Meeting bundle not auto-detected"

- Add to `ambient.meeting.auto_detect.bundles` list
- Or never auto-detect; explicit `ctxt capture meeting start` always works

### "Disk filled up"

- Reduce `ambient.meeting.local_max_gb` or set lower `media_retention_hours`
- Enable `s3_archive` to offload to cold storage

## Privacy & consent

Recording meetings has consent implications that vary by jurisdiction. ctxt provides the capture mechanism; the legal context is the user's:

- **Two-party-consent jurisdictions** (US: CA, FL, IL, MA, MD, MT, NV, NH, PA, WA; most of EU under GDPR): all parties on the call must be aware they're being recorded.
- **One-party-consent jurisdictions** (US: most other states; Canada): the recording party's consent is sufficient.

ctxt does not enforce these laws — but it does:

- **Default to off** (explicit-trigger only; no always-on).
- **Show a recording indicator** that's hard to miss.
- **Ship redact** for post-hoc removal.
- **Emit bus events** that compliance systems can audit.

The DMLP state-by-state survey is a good starting point: <https://www.dmlp.org/legal-guide/state-law-recording>. **This is not legal advice.**

## Related references

- [`ambient-capture.md`](ambient-capture.md) — the parent ambient daemon
- [`sessions.md`](sessions.md) — meetings group with prep/follow-up via SessionID
- [`mcp-agents.md`](mcp-agents.md) — agents observe `transcript_ready` to know when meeting summaries are queryable
- [`../../decisions/ADR-069-meeting-capture-source.md`](../../decisions/ADR-069-meeting-capture-source.md)
- [`../../diagrams/ambient/069-recording-state.mmd`](../../diagrams/ambient/069-recording-state.mmd)
- [`../../diagrams/ambient/069-multi-platform.mmd`](../../diagrams/ambient/069-multi-platform.mmd)
- [`../../diagrams/ambient/069-recording-sequence.mmd`](../../diagrams/ambient/069-recording-sequence.mmd)
