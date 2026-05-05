---
status: paper
adr: ADR-069
task: T-0518
---

# US-0218: Meeting Redact + Export

**System Types:** ctxt, dpkms
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Researchers & OSINT Analysts](../../personas/researchers-osint.md)

---

## User Goal

As a knowledge worker, I want to remove a sensitive segment from a meeting transcript (and from the underlying media file) **after the recording is done**, and I want to export the redacted transcript as Markdown / SRT / VTT for distribution — so a meeting capture is safe to retain even when something off-record was said.

---

## Context

A meeting recording is a high-stakes artifact. Without redact, users will hesitate to record at all — defeating the value of meeting capture ([US-0217](US-0217-meeting-capture-desktop.md)).

Per [ADR-069 §6](../../decisions/ADR-069-meeting-capture-source.md), redact uses the **supersede pattern** (per the existing append-only-with-supersede semantics in ctxt's data model): the original transcript revision is preserved with a `superseded_by` link to a new revision that has the segment removed. Search results show only the latest (redacted) revision; the original is queryable only via `--include-superseded` for audit.

Media-side redact deletes the corresponding time window from the local media file (and from S3 archive if enabled). Once the media is evicted (default 48h TTL per `meeting.media_retention_hours`), only the redacted transcript persists — the original audio/video is gone.

Export packages the redacted transcript in standard subtitle formats so users can attach it to a Notion page, ship it as a deliverable, or import it into another tool.

---

## Acceptance Criteria

### Redact

- [ ] CLI: `ctxt capture meeting redact <object_id> --segment HH:MM-HH:MM [--reason STR]`
- [ ] Validates time range against transcript timestamps; out-of-range returns clear error
- [ ] Writes a new KnowledgeObject revision with the time-range stripped from transcript text + `sections` (preserving structural integrity)
- [ ] Marks original revision `superseded_by` the new one (per existing data model)
- [ ] Removes the corresponding time window from the local media file (if still present per retention)
- [ ] Removes the same window from S3 archive if `meeting.s3_archive=true`
- [ ] Records redaction event in audit-log with: object_id, segment, reason, performed_at, performed_by (profile_id)
- [ ] Search results (`ctxt find`) return only the latest revision by default
- [ ] `ctxt show <id> --include-superseded` returns the audit chain
- [ ] Bus events: `ctxt.ambient.meeting.redact_requested` (start) → `…redact_completed` (success)
- [ ] Multiple redact operations on the same object work; supersede chain extends

### Export

- [ ] CLI: `ctxt capture meeting export <object_id> [--format md|srt|vtt] [--output PATH]`
- [ ] Default `--format md` (Markdown with speaker labels, timestamps, frame-OCR'd shared-screen content as inline blockquotes)
- [ ] `--format srt`: SubRip Subtitle format (compatible with most video players)
- [ ] `--format vtt`: WebVTT format (compatible with HTML5 `<track>` elements)
- [ ] Export honors redactions (uses latest revision; redacted segments absent or shown as `[redacted]` placeholder per `--placeholder` flag)
- [ ] Exports include speaker labels (from diarization) and minute-precision timestamps
- [ ] `ctxt.ambient.meeting.exported` bus event with format + output path
- [ ] Stdout output if `--output` omitted

### CEL integration

- [ ] kit/policy CEL rules can subscribe to `ctxt.ambient.meeting.transcript_ready` for **auto-redaction** of known-sensitive patterns (SSNs, credit-card numbers, OAuth tokens) before the KO becomes searchable
- [ ] Auto-redaction emits the same `…redact_completed` event with `reason="auto-CEL-rule"`

### Failure modes

- [ ] Object not found: clear error with hint
- [ ] Segment outside transcript range: clear error with valid range hint
- [ ] Media file already evicted: redact still works on transcript (with warning that media couldn't be redacted)
- [ ] S3 archive credentials missing while `s3_archive=true`: error with credentials-setup hint
- [ ] Redact during active pipeline run (transcript still being assembled): defer to after pipeline completes

---

## Implementation Notes

### Architecture

```
internal/ambient/meeting/
├── redact.go              — segment-level transcript + media redaction
└── export.go              — md / srt / vtt formatters

cmd/ctxt/cmd/
└── capture_meeting.go     — extended with redact + export subcommands
```

### Supersede chain (existing data model)

Reuses ctxt's append-only-with-supersede pattern. No new data-model concepts; the existing `superseded_by` field on KnowledgeObject revisions is the integration point.

### Media segment removal

```
Media file shape (e.g. .mov):
  [00:00 ----- 14:55] [14:55 ---- 14:58 REDACT] [14:58 ----- 66:00]

After redact:
  [00:00 ----- 14:55] [14:58 ----- 66:00]    (concatenated, re-encoded if codec requires)
```

For `.mov` / `.mp4`: ffmpeg-based stream copy where possible; re-encode only if necessary. For `.m4a`: similar.

### Auto-redact via CEL

```yaml
# policy.d/auto-redact-secrets.cel
match: ctxt.ambient.meeting.transcript_ready
where: |
  event.transcript.matches("\\b\\d{3}-\\d{2}-\\d{4}\\b")     // SSN
  || event.transcript.matches("\\b(?:\\d[ -]*?){13,16}\\b")  // credit card
action: invoke
target: ctxt.ambient.meeting.auto_redact
params:
  reason: "auto-CEL-rule: PII pattern detected"
```

The CEL engine fires `auto_redact` against the matching segments. Same supersede semantics; same audit trail.

### CLI

```bash
# Redact
ctxt capture meeting redact obj_xyz --segment 14:55-14:58 --reason "off-record exchange"

# Export
ctxt capture meeting export obj_xyz                          # markdown to stdout
ctxt capture meeting export obj_xyz --format srt > meeting.srt
ctxt capture meeting export obj_xyz --format vtt --output meeting.vtt
ctxt capture meeting export obj_xyz --format md --placeholder "[redacted]"

# Audit
ctxt show obj_xyz --include-superseded
```

---

## E2E Checklist

- [ ] Record a 5-min meeting; wait for transcript_ready
- [ ] `ctxt capture meeting redact <id> --segment 02:00-02:30 --reason "test"`
- [ ] Verify new revision exists; original superseded
- [ ] `ctxt show <id>` returns redacted version
- [ ] `ctxt show <id> --include-superseded` returns the chain
- [ ] Verify media file's 30s segment removed (file now ~30s shorter)
- [ ] `ctxt capture meeting export <id> --format md`; verify Markdown output, redacted section absent
- [ ] `ctxt capture meeting export <id> --format srt`; verify SRT format valid
- [ ] `ctxt capture meeting export <id> --format vtt`; verify VTT format valid
- [ ] CEL rule for SSN pattern; record a meeting with synthetic SSN; verify auto-redact fires
- [ ] Multiple redacts on same object: supersede chain extends correctly
- [ ] Redact after media evicted (>48h): transcript redacted, warning surfaced for media
- [ ] Bus events fire as documented

---

## Related Stories

- [US-0217](US-0217-meeting-capture-desktop.md) — Meeting capture (this story is the post-capture privacy lever)
- [US-0221](US-0221-meeting-auto-detect-prompt.md) — Sibling Phase 5 work
- [US-0220](US-0220-local-mcp-during-network-loss.md) — MCP `recent_local()` could surface redactions to agents
- Existing data model: append-only KnowledgeObjects with `superseded_by` chain (no ADR; foundational)

---

## Sprint

**Skeleton 9.5 — Ambient Capture + Sessions + Agent-Native MCP**
Implements ADR-069 §6 (redact + export commands) — see `tlc track show ambient-capture`, task **T-0518**.

---

## E2E Tests

- planned: `test/integration/us0218_redact_export_test.go::TestRedact_TranscriptSupersede`
- planned: `test/integration/us0218_redact_export_test.go::TestRedact_MediaSegmentRemoval`
- planned: `test/integration/us0218_redact_export_test.go::TestRedact_S3ArchiveAlsoRedacted`
- planned: `test/integration/us0218_redact_export_test.go::TestRedact_MultipleRedactsExtendChain`
- planned: `test/integration/us0218_redact_export_test.go::TestRedact_AfterMediaEvicted`
- planned: `test/integration/us0218_redact_export_test.go::TestExport_MarkdownFormat`
- planned: `test/integration/us0218_redact_export_test.go::TestExport_SRTFormat`
- planned: `test/integration/us0218_redact_export_test.go::TestExport_VTTFormat`
- planned: `test/integration/us0218_redact_export_test.go::TestExport_HonorsRedactions`
- planned: `test/integration/us0218_redact_export_test.go::TestAutoRedact_CELRuleOnPII`
