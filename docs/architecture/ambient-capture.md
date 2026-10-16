# Ambient Capture Architecture

Engineer-facing reference for the **ambient capture substrate** designed in
[ADR-066](../decisions/ADR-066-ambient-capture-substrate.md) (substrate),
[ADR-067](../decisions/ADR-067-session-workunit.md) (sessions),
[ADR-068](../decisions/ADR-068-mcp-read-surface.md) (MCP read-surface) and
[ADR-069](../decisions/ADR-069-meeting-capture-source.md) (meeting capture):
a long-running local process that turns clipboard, file, browser, window,
screenshot and meeting signals into knowledge objects grouped by work session.

**Status: mostly unbuilt.** The library under `internal/ambient/` exists and is
unit-tested, but no binary runs it. There is no `ctxd` daemon, and
`ctxt capture --ambient` exits with "ambient mode not yet implemented". For what
runs on a user's machine today, see
[../ambient.md](../ambient.md#what-runs-today).

---

## What exists

| Package | Contents | Used by a command? |
|---|---|---|
| `internal/ambient` | `Source` interface, `RawEvent`, `Runner` (dedup → session tag → enqueue, bus topic per step), source registry | no |
| `internal/ambient/browserhistory` | Chromium-family history reader | **yes** — `ctxt capture history` |
| `internal/ambient/position` | per-instance, per-profile history position file (`CTXT_AMBIENT_HISTORY_STATE_FILE`) | **yes** — `ctxt capture history` |
| `internal/ambient/clipboard`, `filewatch`, `foreground`, `screenshot` | `Source` implementations | no |
| `internal/ambient/meeting` | recording state machine, auto-detect, export, retention; the `MeetingRecorder` interface has no per-OS implementation, so nothing records | no |
| `internal/ambient/session` | ADR-067 cutter (idle 5 min, soft cut 3 min, timeout 2 h) | no |
| `internal/ambient/buffer/{local,memory,s3}` | event buffers; `local` defaults to `<state dir>/ctxt/ambient/` (`CTXT_AMBIENT_BUFFER_DIR`) | no |
| `internal/ambient/dedup`, `redact`, `enqueue` | fingerprint dedup, payload redaction, HTTP enqueue client | no |
| `internal/ambient/mcp` | local MCP server with `current_session`, `recent_local`, `pending_enqueue`, `sources`, `health` | no — never served |

Local capture that does run comes from elsewhere: `ctxt watch` (clipboard and
directory watchers, `internal/watcher/`) and `ctxt capture tabs|history|schedule`.

## What does not exist

- **No daemon or launchers.** No `ctxd` binary or `cmd/ctxd`, no Homebrew
  service, launchd plist or systemd unit.
- **No configuration.** No `ambient:` config block; no code reads
  `policy/ambient.yaml`. `--ambient`, `--input`, `--skip` and `--window` on
  `ctxt capture` error out.
- **No sessions in storage.** No `Session` type in `internal/storage`, no
  `objects.session_id` column, no compose-by-session.
- **No meeting recording.** No ScreenCaptureKit, WASAPI or PipeWire recorder;
  no mobile companion apps.
- **One MCP server, two tools.** dpkms serves `/api/v1/mcp/` with `search` and
  `schema` only — see [../manual/workflows/mcp-agents.md](../manual/workflows/mcp-agents.md).
  The ten-tool dpkms surface, the `:8744` local server and `ctxt mcp install`
  were never built.

## Design summary

The ADRs carry the full design and remain the reference for finishing it. In
short:

- **Client-side.** Ambient capture runs on the user's machine, not in dpkms,
  because dpkms is often remote. Events reach dpkms through the existing
  `POST /api/v1/analyze` enqueue API.
- **Per-event pipeline.** Source → redaction → policy veto → fingerprint dedup
  → burst compression → session tag → buffer → enqueue, with a bus topic at
  each step so policy can veto before anything leaves the machine.
- **Sessions.** A client-side cutter groups events into work units; dpkms
  stores the session id and never re-cuts.
- **Meeting capture.** Explicit trigger only, mandatory recording indicator,
  media routed to the existing `audio.transcribe` / `video.full` pipelines.

Diagrams in [`../diagrams/ambient/`](../diagrams/ambient/) show this design,
not the running system.

## Reference

- [ADR-066](../decisions/ADR-066-ambient-capture-substrate.md),
  [ADR-067](../decisions/ADR-067-session-workunit.md),
  [ADR-068](../decisions/ADR-068-mcp-read-surface.md),
  [ADR-069](../decisions/ADR-069-meeting-capture-source.md)
- [../dpkms-or-ctxt.md](../dpkms-or-ctxt.md) — package boundary
- [../ambient.md](../ambient.md) — local capture summary
