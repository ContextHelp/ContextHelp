---
status: paper
adr: ADR-066
task: T-0500
---

# US-0211: Passive Clipboard Watcher

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Researchers & OSINT Analysts](../../personas/researchers-osint.md)

> **Re-grounded in ADR-066** (2026-05-05). The clipboard watcher now lands as the **first ambient source** under the substrate defined in [ADR-066](../../decisions/ADR-066-ambient-capture-substrate.md), with the rest of the substrate (runner, fingerprint dedup, buffer, kit/policy CEL guards, bus events) handling the heavy lifting. Implementation task: **T-0500** (Source: clipboard daemon).
>
> The `ctxt watcher` CLI from the original draft is superseded by `ctxt capture --ambient` per [ADR-066 §Decision](../../decisions/ADR-066-ambient-capture-substrate.md#decision). Daemon is `ctxd`; sources are managed via `ctxt capture --ambient sources` / `tail` / `status`.

---

## User Goal

As a knowledge worker, I want `ctxd` to silently monitor my clipboard in the background and
automatically ingest content when I copy something worth capturing, so that valuable
information is never lost due to capture friction.

---

## Context

The current clipboard fallback in `ctxt analyze` requires intentional invocation: the user
must run a command and happen to have no other input. This is not passive — it's still a
manual trigger that requires context-switching.

True passive capture means the daemon runs in the background, notices clipboard changes,
applies heuristics to decide whether content is worth ingesting (URL, code block, long
text), and enqueues a job silently. The user does nothing. Content appears in the knowledge
base without any deliberate action beyond copying.

This removes the single biggest friction point for casual capture: remembering to run the
command. It also unlocks ambient accumulation — the knowledge base grows naturally as the
user works. Sessions ([ADR-067](../../decisions/ADR-067-session-workunit.md)) group these
clipboard captures with the surrounding work (foreground app, browser visits, file edits)
into queryable work units.

The watcher must be safe: it must not ingest noise (single words, passwords, file paths),
must deduplicate via fingerprint, and must respect kit/policy CEL veto rules. Privacy is
non-negotiable — the clipboard is sensitive; redaction happens at the source boundary
(before the event leaves the user's machine), and content never leaves the machine when a
policy rule vetoes the capture.

---

## Acceptance Criteria

- [ ] `ctxt capture --ambient` starts `ctxd` with the clipboard source registered (per ADR-066 substrate)
- [ ] `ctxt capture --ambient stop` stops the daemon cleanly
- [ ] `ctxt capture --ambient status` shows clipboard source state, last-event timestamp, and counts
- [ ] `ctxt capture --ambient tail --source clipboard` follows the live capture stream
- [ ] Clipboard source emits a `RawEvent` per change; uses `golang.design/x/clipboard` or platform-equivalent
- [ ] Source-side redaction strips known-sensitive shapes (passwords, OAuth tokens) before emission → emits `ctxt.ambient.event.redacted`
- [ ] Client-side fingerprint dedup at enqueue (SHA-256 over normalized payload, configurable window) drops duplicates → emits `ctxt.ambient.event.deduped`
- [ ] Routing heuristics: URL → `url.generic`, fenced code block → `text.short` (code), text ≥ min_length → `text.short`
- [ ] Enqueued objects carry `ambient_source=clipboard` + `session_id` + `fingerprint`
- [ ] Raw clipboard content never written to logs or stderr (only fingerprint + pipeline name)
- [ ] Source is enabled by default in config; `ambient.sources.clipboard.enabled = false` disables
- [ ] Config:

```yaml
ambient:
  sources:
    clipboard:
      enabled: true
      poll_interval: 2s          # platform-specific; some OSes notify
      min_length: 80
      route_urls_to: url.generic
      route_text_to: text.short
```

- [ ] kit/policy CEL rules can veto on `ctxt.ambient.event.captured` for `source=clipboard` (e.g. drop while bundle-id matches `com.1password.*`); veto fires `ctxt.ambient.event.filtered`
- [ ] Source survives `dpkms` restarts; buffered events replay on reconnect
- [ ] On macOS, source requests clipboard permission gracefully if denied
- [ ] Bus event taxonomy from ADR-066 §Decision is fully emitted (lifecycle: started/ready/stopped; per-event: captured/redacted/filtered/deduped/buffered/enqueue.*)

---

## Implementation Notes

### Architecture

```
internal/ambient/clipboard/clipboard.go
  implements ambient.AmbientSource
  -> Start(ctx, bus) — register on the runner
  -> Events() <-chan ambient.RawEvent
  -> Stop(ctx) / Drain(ctx)

  Internal loop:
  poll/notify -> content changed?
    -> fingerprint = sha256(normalized payload)
    -> emit RawEvent{Source: "clipboard", Kind: "text"|"url", Payload, Fingerprint, SuggestedPipeline}
    -> runner: redact -> CEL filter -> dedup -> compress -> session-tag -> buffer -> enqueue
```

The clipboard source is a thin shim that implements `AmbientSource` (per ADR-066 §Decision item 2). The substrate (runner, redaction, policy filter, dedup, buffer, enqueue) handles everything downstream — the source's only job is "produce a RawEvent when the clipboard changes."

### Heuristics (ordered)

```
1. Valid URL (http/https)          -> SuggestedPipeline: url.generic
2. Fenced code block (``` prefix)  -> SuggestedPipeline: text.short (code subtype)
3. Length >= min_length chars      -> SuggestedPipeline: text.short
4. Otherwise: skip (no RawEvent emitted)
```

### Deduplication

Handled by the **substrate**, not the source. The runner's enqueue-boundary fingerprint dedup (per ADR-066 §Decision item 3) drops duplicate fingerprints within a configurable window (default 60s). The source emits unconditionally; runner decides.

### Privacy

- Never log content — only fingerprint, kind, and pipeline name
- Source-side redaction (ADR-066 §Phase 4) strips known-sensitive shapes before emission
- kit/policy CEL guard rules veto sensitive contexts (bundle-id deny-list, time-of-day) before content leaves the machine
- Buffer state files contain fingerprints only, never plaintext (raw event payloads in the buffer are encrypted at rest if `ambient.buffer.encrypt=true`)

### CLI (per ADR-066)

```bash
# Start the daemon (clipboard source is registered by default)
ctxt capture --ambient

# Check what's running
ctxt capture --ambient status
# ->
# ctxd: running (PID 4218, uptime 2h 14m)
# sources:
#   clipboard       active   23 events captured / 19 enqueued / 4 deduped

# Tail live
ctxt capture --ambient tail --source clipboard

# Disable in config
# ambient.sources.clipboard.enabled: false

# Stop
ctxt capture --ambient stop
```

---

## E2E Checklist

- [ ] `ctxt capture --ambient` starts ctxd with clipboard source enabled
- [ ] Copy a URL to clipboard; within poll interval, verify object created via `url.generic` pipeline
- [ ] Verify object has `ambient_source=clipboard`, non-empty `session_id`, populated `fingerprint`
- [ ] Copy same URL again; verify dedup (`ctxt.ambient.event.deduped` event fires; no new object)
- [ ] Copy a single word (< min_length); verify no event emitted
- [ ] Copy a code block (```); verify object created with `subtype=code` via `text.short`
- [ ] Configure CEL veto rule for bundle-id `com.1password.*`; verify clipboard from 1Password is dropped (`.filtered` event fires; no enqueue)
- [ ] Stop `ctxt capture --ambient`; verify cutter emits `session.closed`; no new events after
- [ ] Take dpkms offline; copy several items; verify buffer holds them; verify `pending_enqueue()` MCP tool surfaces them; bring dpkms back; verify replay
- [ ] Bus events fire per ADR-066 taxonomy (lifecycle + capture + buffer + enqueue stages)

---

## Related Stories

- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) — Text capture
  (manual trigger; this story adds passive variant)
- [US-0002](../ingestion/US-0002-url-capture-and-extraction.md) — URL capture
- [US-0212](US-0212-screen-monitor.md) — Screen monitor (sibling ambient source)
- [US-0213](US-0213-file-watch-source.md) — File-watch source (sibling ambient source)
- [US-0214](US-0214-browser-history-source.md) — Browser-history source (sibling ambient source)
- [US-0216](US-0216-work-sessions.md) — Work sessions (groups clipboard captures)
- [US-0219](US-0219-mcp-agent-integration.md) — MCP agent integration

---

## Sprint

**Skeleton 9.5 — Ambient Capture + Sessions + Agent-Native MCP**
Implements ADR-066 Phase 3a (clipboard source) — see `tlc track show ambient-capture`, task **T-0500**.

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Solo Developer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/solo-developer.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

- planned: `test/integration/us0211_clipboard_watcher_test.go::TestClipboardWatcher_CapturesPlainText`
- planned: `test/integration/us0211_clipboard_watcher_test.go::TestClipboardWatcher_DedupeRapidUpdates`
- planned: `test/integration/us0211_clipboard_watcher_test.go::TestClipboardWatcher_RespectsAppAllowlist`
- planned: `test/integration/us0211_clipboard_watcher_test.go::TestClipboardWatcher_PauseResume`
- planned: `test/integration/us0211_clipboard_watcher_test.go::TestClipboardWatcher_RoutesToTextPipeline`
