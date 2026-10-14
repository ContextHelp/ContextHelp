# Workflow: Meeting Capture

Recording calls from ctxt, as designed in [ADR-069](https://github.com/ContextHelp/ContextHelp/blob/next/docs/decisions/ADR-069-meeting-capture-source.md), is not implemented. There is no `ctxt capture meeting` command (`start`, `stop`, `list`, `show`, `redact`, `export`), no recording hotkey or indicator, no `ctxd` daemon to grant Screen Recording or Microphone access to, and no `ambient.meeting` config. `internal/ambient/meeting/` holds the state machine only; no per-OS recorder exists, so nothing records.

What works today is capturing a recording you made with another tool, like any other file:

```bash
ctxt capture ./call.m4a --hint meeting --note "Q3 planning"
ctxt capture ./call.mp4 --wait
```

The file extension picks the pipeline:

| Extensions | Pipeline | Steps |
|---|---|---|
| `.mp3` `.wav` `.ogg` `.flac` `.m4a` | `audio.transcribe` | transcription, speaker diarization, timestamp alignment, sectioning |
| `.mp4` `.mov` `.avi` `.mkv` `.webm` | `video.full` | audio extraction and transcription, frame sampling, scene detection, frame OCR, timeline |

Both run on the dpkms host with the `providers.transcription` (and, for `video.full`, `providers.ocr` and `providers.video`) backends — see [provider backends](https://github.com/ContextHelp/ContextHelp/blob/next/docs/environment-variables/ai-providers.md#backend-model-and-endpoint). Capture flags: [`ctxt capture`](https://github.com/ContextHelp/ContextHelp/blob/next/docs/ctxt/capture.md). Use `--pipeline video.audio_only` to transcribe a video without frame analysis.

## Related references

- [`browser-tab-capture.md`](browser-tab-capture.md), [`browser-history-capture.md`](browser-history-capture.md), `ctxt watch --help` — what runs locally today
- [`docs/architecture/ambient-capture.md`](https://github.com/ContextHelp/ContextHelp/blob/next/docs/architecture/ambient-capture.md) — design reference for the unbuilt substrate
- [`docs/decisions/ADR-069-meeting-capture-source.md`](https://github.com/ContextHelp/ContextHelp/blob/next/docs/decisions/ADR-069-meeting-capture-source.md)
