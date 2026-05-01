---
status: paper
---

# US-0211: Passive Clipboard Watcher

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Researchers & OSINT Analysts](../../personas/researchers-osint.md)

---

## User Goal

As a knowledge worker, I want ctxt to silently monitor my clipboard in the background and
automatically ingest content when I copy something worth capturing, so that valuable
information is never lost due to capture friction.

---

## Context

The current clipboard fallback in `ctxt analyze` requires intentional invocation: the user
must run a command and happen to have no other input. This is not passive — it's still a
manual trigger that requires context-switching.

True passive capture means the system runs in the background, notices clipboard changes,
applies heuristics to decide whether content is worth ingesting (URL, code block, long
text), and enqueues a job silently. The user does nothing. Content appears in the knowledge
base without any deliberate action beyond copying.

This removes the single biggest friction point for casual capture: remembering to run the
command. It also unlocks ambient accumulation — the knowledge base grows naturally as the
user works.

The watcher must be safe: it must not ingest noise (single words, passwords, file paths),
must deduplicate via content hash, and must respect an opt-out config. Privacy is
non-negotiable — the clipboard is sensitive; the watcher must never log raw content.

---

## Acceptance Criteria

- [ ] `ctxt watcher start` starts all enabled background watchers including clipboard
- [ ] `ctxt watcher stop` stops all background watchers
- [ ] `ctxt watcher status` shows enabled watchers, last trigger times, and job counts
- [ ] Clipboard watcher polls at configurable interval (default: 2s)
- [ ] Content is only enqueued if it differs from the last captured hash
- [ ] Ingestion heuristics filter noise: min length 80 chars, or valid URL, or fenced code block
- [ ] Enqueued jobs are annotated with `origin=watcher/clipboard`
- [ ] Raw clipboard content is never written to logs or stderr
- [ ] Watcher is disabled by default; must be explicitly enabled in config
- [ ] Config controls poll interval, min length threshold, and auto_ingest flag:

```yaml
watchers:
  clipboard:
    enabled: false
    poll_interval: 2s
    min_length: 80
    auto_ingest: true
```

- [ ] `ctxt watcher enable clipboard` / `ctxt watcher disable clipboard` toggle without
  restart
- [ ] Duplicate content (same hash as last ingested) is silently skipped
- [ ] On macOS, watcher requests clipboard permission gracefully if denied
- [ ] Watcher survives `dpkms serve` restarts without re-ingesting old content

---

## Implementation Notes

### Architecture

```
ClipboardWatcher (polls every 2s)
  -> hash(content) != lastHash?
    -> heuristic(content) passes?
      -> enqueue job (pipeline: text.short or url.generic)
      -> store hash as lastHash
```

Watcher implements the `Watcher` interface defined in Sk8 Task 2.1. Runs as a goroutine
managed by the ctxt watcher supervisor.

### Heuristics (ordered)

```
1. Valid URL (http/https)          -> pipeline: url.generic
2. Fenced code block (``` prefix)  -> pipeline: text.short (code)
3. Length >= min_length chars      -> pipeline: text.short
4. Otherwise: skip
```

### Deduplication

```
Store: ~/.local/share/ctxt/watcher-state.json
  { "clipboard": { "last_hash": "sha256:...", "last_seen": "2026-03-25T..." } }
```

Hash is SHA-256 of raw content. State persists across restarts.

### Privacy

- Never log content — only hash and pipeline name
- `CTXT_NO_CLIPBOARD=1` env var disables watcher unconditionally
- Watcher state file contains hashes only, never plaintext

### CLI

```bash
# Enable and start
ctxt watcher enable clipboard
ctxt watcher start

# Check status
ctxt watcher status
# ->
# Watcher         Status    Last Trigger              Jobs Enqueued
# clipboard       running   2026-03-25T14:32:01Z      47
# directory       stopped   -                         0

# Disable
ctxt watcher disable clipboard
```

---

## E2E Checklist

- [ ] Enable clipboard watcher via config
- [ ] Start `ctxt watcher start`
- [ ] Copy a URL to clipboard
- [ ] Wait 2s; verify job appears in `ctxt jobs`
- [ ] Copy same URL again; verify no duplicate job
- [ ] Copy a single word; verify no job enqueued
- [ ] Copy a code block (```); verify job enqueued with code pipeline
- [ ] Stop watcher; verify no new jobs after copy
- [ ] Restart watcher; verify state file prevents re-ingestion

---

## Related Stories

- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) — Text capture
  (manual trigger; this story adds passive variant)
- [US-0002](../ingestion/US-0002-url-capture-and-extraction.md) — URL capture
- [US-0212](US-0212-screen-monitor.md) — Screen monitor (sibling passive watcher)
- [US-0208](US-0208-temporal-watch.md) — Temporal watch (scheduled re-capture)

---

## Sprint

**Skeleton 8 — Trust & Automation**
Implements Sk8 Task 2.2 (Clipboard Watcher) and depends on Task 2.1 (Watcher Interface).

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
