---
status: paper
adr: ADR-066
task: T-0501
---

# US-0213: File-Watch / Drop-Folder Source

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/researchers-osint.md), [Maintainers](../../personas/maintainers.md)

---

## User Goal

As a knowledge worker, I want to drop a file into a watched folder (default `~/Inbox/ctxt`) and have it ingested into my knowledge graph automatically — routed to the right pipeline by extension — so capturing files becomes "save to a known directory" rather than "remember to run a command."

---

## Context

The clipboard source (US-0211) handles inline content. Files are different: a saved PDF brief, a downloaded research paper, an exported chat log, a screenshot taken by another tool — these are bulky enough that pasting them is wrong. A drop-folder pattern lets the user's existing file workflow drive ingestion.

This is a sibling ambient source under [ADR-066](../../decisions/ADR-066-ambient-capture-substrate.md). The substrate handles fingerprint dedup, kit/policy CEL guards, session tagging, buffer, and enqueue. The source's only job is "watch directories with fsnotify; emit a RawEvent per new file."

After successful pipeline run, the source moves the file to a `processed/` subfolder so the user can see what's been ingested and can re-trigger by moving back.

---

## Acceptance Criteria

- [ ] Source registered as `filewatch` in the ambient runner
- [ ] Watches configured directories via `fsnotify` (default `~/Inbox/ctxt`; configurable list)
- [ ] Routes by extension:
    - `.md` / `.txt` → `text.long`
    - `.pdf` → `text.long` (with optional Docling backend per ADR-035)
    - `.png` / `.jpg` / `.jpeg` / `.heic` → `image.ocr`
    - `.mp3` / `.wav` / `.m4a` / `.flac` / `.ogg` → `audio.transcribe`
    - `.mp4` / `.mov` / `.webm` / `.mkv` → `video.full`
    - Unknown extensions → `text.long` if file detects as text; otherwise routed to `inbox` for triage
- [ ] After successful enqueue, file moved to `<watch_dir>/processed/<YYYY-MM>/`
- [ ] On enqueue failure, file kept in place; retry on next change event or daemon restart
- [ ] Fingerprint = SHA-256 of file content; substrate-level dedup catches re-saves of identical content
- [ ] Bus events emit per ADR-066 taxonomy
- [ ] kit/policy CEL veto on `ctxt.ambient.event.captured` for `source=filewatch` can drop sensitive paths (e.g. `~/Inbox/ctxt/private/*` for personal-profile-only)
- [ ] Config:

```yaml
ambient:
  sources:
    filewatch:
      enabled: true
      directories:
        - ~/Inbox/ctxt
      processed_subdir: processed
      max_file_size_mb: 500
```

- [ ] Files exceeding `max_file_size_mb` are skipped with a clear log message + bus event
- [ ] Hidden files (`.*`) and system metadata (`.DS_Store`, `Thumbs.db`) are ignored
- [ ] Cross-platform: works on macOS / Linux / Windows via fsnotify
- [ ] Daemon restart picks up files dropped while it was offline (full directory scan on Start)

---

## Implementation Notes

### Architecture

```
internal/ambient/filewatch/filewatch.go
  implements ambient.AmbientSource
  -> Start: fsnotify.Watcher per configured dir + initial directory scan
  -> on CREATE / MOVED_TO event → emit RawEvent with file path + extension-routed pipeline
  -> on enqueue success: rename file to processed/<YYYY-MM>/<filename>
```

### Routing logic (`route.go`)

Extension table maps to existing pipelines (already shipped in `internal/pipeline/builtins/`). New file types added via plugin extension point per [ADR-012](../../decisions/ADR-012-plugins-extend-any-layer.md).

### Privacy

Per-directory profile scoping:

```yaml
ambient:
  sources:
    filewatch:
      directories:
        - path: ~/Inbox/ctxt          # global / default profile
        - path: ~/Inbox/ctxt-work     # profile=work only
          profile: work
        - path: ~/Inbox/ctxt-private  # profile=personal only; CEL deny-list
          profile: personal
```

### CLI

```bash
# Enable in config; daemon picks up directory list on next start.
# Once running:
ctxt capture --ambient tail --source filewatch
ctxt capture --ambient sources    # shows watched directories + recent-events count
```

---

## E2E Checklist

- [ ] `ctxt capture --ambient` starts ctxd with filewatch source
- [ ] Drop a `.md` file into `~/Inbox/ctxt`; verify object via `text.long` within 5s; verify file moved to `processed/`
- [ ] Drop a `.pdf`; verify routed to `text.long`
- [ ] Drop a `.png`; verify routed to `image.ocr`
- [ ] Drop a `.mp4`; verify routed to `video.full`
- [ ] Drop a 600MB file (over max_file_size_mb); verify skipped with bus event
- [ ] Drop a `.DS_Store`; verify ignored silently
- [ ] Stop daemon; drop 3 files; restart daemon; verify all 3 picked up via initial scan
- [ ] Configure profile-scoped directory; drop file; verify object has correct ProfileID
- [ ] CEL veto for path containing `secret`; verify dropped before pipeline
- [ ] Bus events fire (lifecycle + per-event + buffer + enqueue stages)

---

## Related Stories

- [US-0211](US-0211-passive-clipboard-watcher.md) — Sibling clipboard ambient source
- [US-0214](US-0214-browser-history-source.md) — Sibling browser-history ambient source
- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) — `--file` flag for one-shot ingestion (this story is the daemonized variant)
- [US-0006](../ingestion/US-0006-document-parsing-and-decomposition.md) — Document parsing pipelines (file-watch routes to these)
- [US-0216](US-0216-work-sessions.md) — Sessions group dropped files with surrounding work
- [US-0219](US-0219-mcp-agent-integration.md) — Agent queries dropped-file ingests via MCP

---

## Sprint

**Skeleton 9.5 — Ambient Capture + Sessions + Agent-Native MCP**
Implements ADR-066 Phase 3 (file-watch source) — see `tlc track show ambient-capture`, task **T-0501**.

---

## E2E Tests

- planned: `test/integration/us0213_filewatch_test.go::TestFilewatch_RoutesByExtension`
- planned: `test/integration/us0213_filewatch_test.go::TestFilewatch_MovesToProcessed`
- planned: `test/integration/us0213_filewatch_test.go::TestFilewatch_RestartCatchUp`
- planned: `test/integration/us0213_filewatch_test.go::TestFilewatch_ProfileScopedDirs`
- planned: `test/integration/us0213_filewatch_test.go::TestFilewatch_MaxFileSizeSkip`
- planned: `test/integration/us0213_filewatch_test.go::TestFilewatch_CELVetoOnPath`
