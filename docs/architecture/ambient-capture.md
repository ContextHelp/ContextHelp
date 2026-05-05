# Ambient Capture Architecture

Focused architecture for the **ambient capture substrate** — the long-running, local-machine subsystem that produces a steady stream of small KnowledgeObjects from the user's working environment (clipboard, file-watch, browser history, foreground window, screenshots, meetings) and groups them into work sessions.

This document is the engineer-facing reference. For the design rationale and decisions, see:

- [ADR-066](../decisions/ADR-066-ambient-capture-substrate.md) — the substrate
- [ADR-067](../decisions/ADR-067-session-workunit.md) — sessions / WorkUnits
- [ADR-068](../decisions/ADR-068-mcp-read-surface.md) — MCP read-surface (dpkms + ctxd)
- [ADR-069](../decisions/ADR-069-meeting-capture-source.md) — meeting capture source

For diagrams, see [`../diagrams/ambient/`](../diagrams/ambient/).

---

## Why ambient (vs. one-shot)

Today, every ingestion in ctxt is initiated by the user (`ctxt analyze`, importer runs, fetch adapters). Ambient capture closes the gap: while the user is working, signals worth remembering — a copied URL, a foreground app focus, a saved file, a meeting transcript — flow into the knowledge graph automatically. Sessions group those signals so the resulting graph is queryable as work-units, not as a noisy stream of individual events.

## Critical constraint: dpkms is often remote

The substrate's central architectural decision: **ambient capture runs client-side in `ctxd`, not in dpkms**. A remote dpkms cannot read your clipboard, see what window has focus, watch `~/Inbox`, or capture system audio. Ambient sources, the session cutter, and the local MCP read-surface all live on the user's machine. dpkms remains a pure pipeline+storage worker that absorbs ambient enqueues over its existing HTTP API.

See [`../dpkms-or-ctxt.md`](../dpkms-or-ctxt.md) for the boundary.

---

## High-level architecture

> Diagram: [`../diagrams/ambient/066-architecture.mmd`](../diagrams/ambient/066-architecture.mmd)

The `ctxd` daemon is one binary with three launch paths:

- `ctxt capture --ambient` — auto-config CLI launcher (primary UX)
- `brew services start ctxd` / `launchctl` / `systemctl --user enable --now ctxd.service` — service-manager launchers
- `cmd/ctxd` direct invocation — for debugging or process supervisors

All three launch the same `internal/ambient/` library. The library hosts:

- **Sources** — independent event producers (clipboard, file-watch, browser-history, foreground-window, screenshot, meeting). Each implements `AmbientSource`.
- **Runner** — multiplexes source streams, applies redaction → kit/policy CEL filtering → fingerprint dedup → burst compression → SessionID tagging.
- **Buffer** — pluggable backend (local FS XDG, S3-compatible, in-memory) holds events while waiting for dpkms.
- **Session cutter** — three-rule state machine (idle / soft-cut / timeout) with frequent-switching exception. Tags every RawEvent with the active SessionID.
- **Enqueue client** — POSTs to whichever dpkms is configured via `/api/v1/analyze` (per ADR-056 unified enqueue API). Handles retry, backoff, and replay.
- **Local MCP server** — read-only surface for AI agents that need live local state (active session, pending-enqueue, recent local captures).

---

## Core types

### `AmbientSource`

```go
package ambient

type AmbientSource interface {
    Name() string                                    // unique source identity
    Start(ctx context.Context, b bus.Bus) error      // begin emitting
    Events() <-chan RawEvent                         // event stream
    Drain(ctx context.Context) error                 // stop accepting; flush in-flight
    Stop(ctx context.Context) error                  // hard stop
}

type RawEvent struct {
    Source            string         // matches AmbientSource.Name()
    OccurredAt        time.Time
    Kind              string         // "text" | "url" | "image" | "file" | "window-focus" | "meeting"
    Payload           []byte         // pre-redacted by source
    Fingerprint       string         // SHA-256 over normalized payload
    SuggestedPipeline string         // "text.short" | "url.generic" | "image.ocr" | "audio.transcribe" | ...
    SessionID         string         // populated by runner from cutter
    Metadata          map[string]any // source-specific (bundle id, file path, app name, ...)
}
```

### `Buffer`

Pluggable backend interface. Three implementations:

- **`buffer/local`** — XDG-compliant filesystem (`$XDG_STATE_HOME/ctxt/ambient/`); LRU eviction, 2GB default cap.
- **`buffer/s3`** — S3-compatible (AWS / R2 / B2 / MinIO); provider lifecycle policies handle retention.
- **`buffer/memory`** — bounded ring; tests only.

> Diagram: [`../diagrams/ambient/066-buffer-backends.mmd`](../diagrams/ambient/066-buffer-backends.mmd)

Media files (meetings) get a separate retention tier (default 48-hour TTL, 20GB local cap) — see [ADR-069 §4](../decisions/ADR-069-meeting-capture-source.md).

### `Session`

```go
package pluginapi  // stable plugin API

type Session struct {
    ID            string         `json:"id"`              // "sess_<12-hex>"
    StartedAt     time.Time
    EndedAt       *time.Time     // nil while active
    EndReason     string         // "idle" | "soft_cut" | "timeout" | "shutdown" | "daily_safety_net"
    AppMix        []AppShare
    EventCount    int
    SourceMix     []string
    ProfileID     string
    Metadata      map[string]any
    FlushEnd      *time.Time     // reserved Phase 6+ (per-session reducer)
    ClassifiedEnd *time.Time     // reserved Phase 6+
    CreatedAt, UpdatedAt time.Time
}
```

`KnowledgeObject` gains an optional `SessionID string` field; soft-FK on `objects.session_id` so storage absorbs both event-before-session and session-before-event arrival orders during network-loss replay.

---

## Per-event flow

> Diagram: [`../diagrams/ambient/066-event-flow.mmd`](../diagrams/ambient/066-event-flow.mmd)

Each RawEvent emitted by a source traverses these stages, with a kit/bus event at each transition:

1. **Source emits** → `ctxt.ambient.event.captured`
2. **Source-side redaction** (passwords, OAuth tokens) → `ctxt.ambient.event.redacted` if mutated
3. **kit/policy CEL filter** — veto-able by rule → `ctxt.ambient.event.filtered` if dropped
4. **Fingerprint dedup** (SHA-256 over normalized payload, 60-second window default) → `ctxt.ambient.event.deduped` if dropped
5. **Burst compression** (collapse multiple foreground-focus events to one focus-change) → `ctxt.ambient.event.compressed`
6. **Tag SessionID** from cutter
7. **Append to buffer** → `ctxt.ambient.buffer.appended`
8. **Enqueue ring** queues the event for HTTP POST → `ctxt.ambient.enqueue.queued`
9. **Network check** — dpkms reachable?
   - **No:** hold in buffer, emit `ctxt.ambient.enqueue.waiting`, retry per backoff
   - **Yes:** POST `/api/v1/analyze`
10. **Response** — `ctxt.ambient.enqueue.succeeded` or `…failed` (retry per policy)

Privacy enforcement at step 3 is the load-bearing property: events that policy vetoes never leave the user's machine.

---

## Session cutting

> Diagrams: [`../diagrams/ambient/067-lifecycle.mmd`](../diagrams/ambient/067-lifecycle.mmd) (state machine), [`../diagrams/ambient/067-cutter-flowchart.mmd`](../diagrams/ambient/067-cutter-flowchart.mmd) (decision flow)

Three rules, ported verbatim from OpenChronicle's `session/manager.py`:

| Rule | Default | Description |
|---|---|---|
| **Hard cut (idle)** | `gap_minutes = 5` | No capture-worthy event for 5 min → end at last_event_time |
| **Soft cut (single-app)** | `soft_cut_minutes = 3` | One app held focus for 3 min AND user is NOT frequent-switching → end. Frequent-switching exception: ≥2 distinct apps in last 2 min suppresses the cut (IDE+terminal+browser workflows). |
| **Hard timeout** | `max_session_hours = 2` | Session > 2h regardless of activity → end. Safety net. |

Plus:
- **Force end** on `ctxd` shutdown
- **Daily safety net** at local 23:55 closes any session still open
- **Cut tick** (`tick_seconds = 30`) fires the cutter even when no events arrive

Cutter runs **client-side in ctxd**. Sessions are emitted to dpkms as opaque metadata via `session_id` on each enqueue. dpkms persists; doesn't re-cut.

### Replay safety

> Diagram: [`../diagrams/ambient/067-replay-sequence.mmd`](../diagrams/ambient/067-replay-sequence.mmd)

When `ctxd` is buffering due to dpkms-down:
- Session-opened events queue alongside RawEvents. Idempotent PUT on session.id makes replay safe.
- RawEvents tagged with session_id may arrive at dpkms before or after the session row. Soft-FK on `objects.session_id` lets storage absorb both orders.
- Session-closed events also queue; closing happens locally first (cutter advances), then replays to dpkms when network returns.

---

## Daemon hosting

`ctxd` is the canonical long-running binary. Three launch paths exist for ergonomic flexibility:

| Path | Best for | Mechanism |
|---|---|---|
| `ctxt capture --ambient` | Quick start, zero-config users | CLI auto-configures + fork-exec's `ctxd` |
| `brew services start ctxd` (macOS) | Casual macOS users | Homebrew formula installs launchd plist; service-manager handles restart |
| `launchctl load ~/Library/LaunchAgents/io.ctxt.ctxd.plist` (macOS) | Power users with custom plist | Direct launchd control |
| `systemctl --user enable --now ctxd.service` (Linux) | Linux desktop users | Package installs systemd user unit |
| `cmd/ctxd` direct (foreground) | Debugging, container deployments | Bare invocation; manage with tmux / supervisor |

**dpkms is never a host** for the ambient daemon. dpkms is often deployed remote and cannot read local-machine signals.

---

## Bus event taxonomy

The ambient substrate emits 22+ kit/runtime/bus topics covering every transition. Topics follow the 4-segment past-tense convention: `ctxt.<category>.<object>.<action>`.

Stages:

- **Source lifecycle** — started / ready / drained / stopped / failed
- **Event capture** — captured / redacted / filtered
- **Compression / dedup** — debounced / deduped / compressed
- **Buffer** — appended / evicted / replayed
- **Enqueue** — queued / waiting / attempted / succeeded / failed
- **Sessions** — opened / closed / event_joined / cut_evaluated
- **Meeting** (per ADR-069) — requested / policy_vetoed / permission_* / started / indicator_displayed / paused / resumed / stopped / capture_failed / device_changed / enqueued / transcript_progress / transcript_ready / transcript_failed / media_archived / media_evicted / redact_* / exported / auto_detected / prompt_dismissed

Full taxonomy reference in [ADR-066 §Decision](../decisions/ADR-066-ambient-capture-substrate.md) and [ADR-069 §5](../decisions/ADR-069-meeting-capture-source.md).

### kit/runtime/policy CEL integration

CEL rules subscribe to any `ctxt.ambient.*.*` topic and can:

- **Veto** at `*.captured` to drop sensitive events before fingerprint or buffer (e.g. "drop clipboard while bundle-id matches `com.1password.*`"). Veto fires before the event leaves the machine.
- **Audit** at `*.started` / `*.stopped` for compliance logging.
- **Alert** at `*.enqueue.waiting` lasting > N minutes (dpkms-down detection).
- **Watchdog** at `*.indicator_displayed` to assert the meeting recording indicator is shown within 500ms of `*.started`.
- **Auto-redaction** at `*.transcript_ready` to remove known-sensitive patterns (SSNs, credit cards) before the KO becomes searchable.

Privacy enforcement is structural, not incidental: every transformation is independently observable and policy-gateable.

---

## MCP read-surface

> Diagrams: [`../diagrams/ambient/068-topology.mmd`](../diagrams/ambient/068-topology.mmd) (two-server topology), [`../diagrams/ambient/068-tool-dispatch.mmd`](../diagrams/ambient/068-tool-dispatch.mmd) (dispatch), [`../diagrams/ambient/068-tool-surface.mmd`](../diagrams/ambient/068-tool-surface.mmd) (tools)

Two MCP servers, both read-only, both streamable-HTTP per MCP spec 2025-03-26:

- **dpkms-side** (`/api/v1/mcp/`): authoritative; 10 tools (search, list, get, entity, recent, sessions, session, compose, mentions, schema). Mounts in-process on the existing dpkms HTTP listener — reuses storage drivers, FTS, vector, RSQL, profile resolution. ~500 LoC of tool-dispatch shim.
- **ctxd-side** (`:8744/mcp`): local-only; 5 tools (current_session, recent_local, pending_enqueue, sources, health). Surfaces information dpkms cannot see when remote: live cutter state, buffered events awaiting enqueue, fingerprint dedup state.

Agents on the user's machine attach to **both**. Agents on other machines attach only to dpkms. No proxy; no double-counting.

`ctxt mcp install <client>` writes the appropriate config for `claude-code`, `claude-desktop`, `cursor`, `codex`, `opencode`, or `mcp-json`. Idempotent; reversible via `ctxt mcp uninstall`.

---

## Meeting capture (specialized source)

> Diagrams: [`../diagrams/ambient/069-recording-state.mmd`](../diagrams/ambient/069-recording-state.mmd), [`../diagrams/ambient/069-multi-platform.mmd`](../diagrams/ambient/069-multi-platform.mmd), [`../diagrams/ambient/069-recording-sequence.mmd`](../diagrams/ambient/069-recording-sequence.mmd)

Meeting capture is one source among many but warrants its own architectural treatment because:

1. **Multi-platform with hard split.** Desktop (Mac/Linux/Windows) ships in `ctxd`; mobile (iOS/Android) requires separate native companion apps that POST to the same enqueue endpoint.
2. **System audio is the hard problem.** Public OS APIs vary widely: ScreenCaptureKit (macOS 13+), WASAPI loopback + Graphics Capture (Windows 10+), xdg-desktop-portal + PipeWire (Linux Wayland).
3. **Privacy is structural.** Explicit-trigger only; mandatory recording indicator; first-class redact-as-supersede; consent-law landscape varies by jurisdiction.
4. **Storage profile is different.** Media files (~500MB / hour at 1080p) get their own retention tier, separate from the substrate's small-event buffer.
5. **Existing pipelines do the work.** `audio.transcribe` (diarization, alignment, sectioning) and `video.full` (transcript + frame OCR + scene-aligned timeline) already exist; capture source just produces the file.

Recording is **explicit-trigger only in v1**. Auto-detect prompt (Phase 5) suggests starting recording when known meeting bundle-ids come to focus, but never starts without user confirmation.

---

## Phasing

| Phase | Scope | Reference |
|---|---|---|
| **1** | ADRs (066/067/068/069) + documentation update | This doc + ADRs |
| **2** | Substrate skeleton: source interface, runner, fingerprint dedup at enqueue, in-memory + local-FS buffers | T-0498/0499/0511 |
| **3** | Real sources: clipboard, file-watch, browser-history, foreground (macOS), screenshot, meeting (macOS audio-only → full → Windows → Linux) | T-0500–0504, T-0513–0516, T-0519 |
| **4** | Quality: session cutter implementation + storage migration, buffer retention, S3 backend, redaction hooks | T-0505–0510 |
| **5** | UX: compose-by-session, meeting redact + export, auto-detect prompt, docs, CLI quickref | T-0508/0509, T-0517/0518/0522 |
| **6** | iOS companion app (separate repo, separate track) | T-0520 |
| **7** | Android companion app (separate repo, separate track) | T-0521 |

Track: `tlc track show ambient-capture`.

---

## Composition with other ctxt subsystems

| Subsystem | How ambient composes |
|---|---|
| **Pipelines** ([ADR-053](../decisions/ADR-053-knowledgeobject-as-pipeline-draft.md)) | Ambient sources route to existing pipelines via `SuggestedPipeline`. No new pipelines required. Meeting capture → `audio.transcribe` / `video.full`. |
| **Enqueue API** ([ADR-056](../decisions/ADR-056-unified-enqueue-api.md)) | `ctxd` POSTs to `/api/v1/analyze` with new optional fields (`session_id`, `ambient_source`, `fingerprint`). Backwards compatible. |
| **Adapters** ([ADR-065](../decisions/ADR-065-pluggable-adapters.md)) | Sibling concept, distinct concern. Adapters are protocol-shaped (one-platform-per-protocol: email, contacts). Ambient sources are event producers; multiple run concurrently; no protocol identity. |
| **Federation** ([ADR-064](../decisions/ADR-064-federation.md)) | Each federated dpkms instance has its own MCP server. Cross-instance ambient is not v1; `ctxd` enqueues to the configured dpkms only. |
| **kit/runtime/bus** | Substrate emits 22+ topics; observers subscribe for monitoring, policy gating, audit. |
| **kit/runtime/policy** | CEL rules veto/audit at any bus topic. Privacy posture is enforced where the data is, not after egress. |
| **kit/runtime/secrets** | S3 buffer credentials, dpkms auth tokens (ADR-023 when public-bind enabled). |
| **Plugin API** | `Session` lives in `pkg/pluginapi/` next to `KnowledgeObject`. Plugins can read `SessionID` for session-aware behavior; opt-in. |

---

## Reference

- [ADR-066 Ambient capture substrate](../decisions/ADR-066-ambient-capture-substrate.md)
- [ADR-067 Session/WorkUnit type](../decisions/ADR-067-session-workunit.md)
- [ADR-068 MCP read-surface](../decisions/ADR-068-mcp-read-surface.md)
- [ADR-069 Meeting capture source](../decisions/ADR-069-meeting-capture-source.md)
- [Diagrams index](../diagrams/README.md)
- [`dpkms-or-ctxt.md`](../dpkms-or-ctxt.md) — package boundary
- Track: `tlc track show ambient-capture`
- OpenChronicle prior art: `~/.p/sandbox/OpenChronicle/`
