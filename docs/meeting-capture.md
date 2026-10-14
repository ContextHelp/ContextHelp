# Meeting Capture

Not implemented. The meeting-capture source designed in [ADR-069](decisions/ADR-069-meeting-capture-source.md) — `ctxt capture meeting start|stop|list|show|redact|export`, the recording hotkey and indicator, per-OS Screen Recording / Microphone permissions for a `ctxd` daemon, the `ambient.meeting` config block and the media retention tier — does not exist. `internal/ambient/meeting/` holds the recording state machine, auto-detect, export and retention logic with no per-OS recorder behind it, and no command runs it.

A recording made with another tool can be captured like any file; the extension routes it to `audio.transcribe` or `video.full`. See [`manual/workflows/meeting-capture.md`](manual/workflows/meeting-capture.md).

Design reference: [`architecture/ambient-capture.md`](architecture/ambient-capture.md).
