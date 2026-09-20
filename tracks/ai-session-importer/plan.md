---
title: "AI Session Importer"
tracks:
  - ai-session-importer
tasks:
  - title: "Record types + renderer (Kind, Record, RenderContent, FormatSource)"
    description: |
      Create internal/importer/aisession/record.go + record_test.go.
      Types: Kind (plan|memory|lesson|task|history), Record struct
      (ExternalID, Kind, Title, Content, ContentHash, Source, Project,
      Tool, Timestamp, Tags, RelPath).
      Funcs: RenderContent(Record) string, FormatSource(tool, kind, id) string.
      Tests: render plan/memory/lesson, stable ExternalID.
      Ref: internal/importer/discord/parser.go:240-304 (RenderContent pattern)
    effort: S
    priority: P1
    tags: [phase:1, importer, types]

  - title: "Markdown scanner (ScanDir, frontmatter extraction, content hashing)"
    description: |
      Create internal/importer/aisession/scanner.go + scanner_test.go + testdata/.
      Func: ScanDir(dir, tool, kind, project) ([]Record, error).
      Walk .md files, skip MEMORY.md (index), compute sha256 content hash,
      extract YAML frontmatter (name/type fields), fallback title from H1
      heading or filename stem. ExternalID = "sha256:<hash>".
      Golden fixtures: testdata/plans/, testdata/memory/, testdata/lessons/.
      Tests: scan plans, skip index, scan lessons, empty dir, nonexistent,
      content hash stability.
      Ref: internal/importer/obsidian/parser.go:51-114 (WalkVault pattern)
      Ref: internal/importer/logseq/parser.go:75-138 (WalkGraph pattern)
    effort: M
    priority: P1
    tags: [phase:1, importer, scanner]
    blocked-by: [0]

  - title: "Claude Code directory scanner (ScanClaudeDir)"
    description: |
      Create internal/importer/aisession/claude.go + claude_test.go.
      Func: ScanClaudeDir(claudeDir) ([]Record, error).
      Scans ~/.claude/plans/ as KindPlan, ~/.claude/projects/*/memory/
      as KindMemory. Decodes URL-encoded project dir names back to paths
      (e.g. "-Users-me-proj" -> "/Users/me/proj").
      Tests: structure scan, project attribution, empty dir.
    effort: S
    priority: P1
    tags: [phase:1, importer, claude]
    blocked-by: [1]

  - title: "Agent directory scanner (ScanAgentDir)"
    description: |
      Create internal/importer/aisession/agent.go + agent_test.go.
      Func: ScanAgentDir(agentDir) ([]Record, error).
      Scans ~/.agents/lessons/ as KindLesson.
      Tests: scan lessons, empty dir.
    effort: XS
    priority: P1
    tags: [phase:1, importer, agent]
    blocked-by: [1]

  - title: "Unified ScanAll entry point"
    description: |
      Create internal/importer/aisession/scan.go + scan_test.go.
      Types: ScanOptions{ClaudeDir, AgentDir string}.
      Func: ScanAll(ScanOptions) ([]Record, error).
      Combines Claude + Agent scanning. Missing dirs silently skipped.
      Tests: combined scan, missing dirs produce 0 records.
    effort: XS
    priority: P1
    tags: [phase:1, importer, scan]
    blocked-by: [2, 3]

  - title: "CLI command: ctxt import ai-sessions"
    description: |
      Create cmd/ctxt/cmd/import_aisessions.go.
      Cobra subcommand under importCmd. Flags: --claude-dir (default
      ~/.claude), --agent-dir (default ~/.agents), --since, --max-items,
      --server, --pipeline, --dry-run.
      Dry-run prints kind counts + preview (20 items max).
      Live mode enqueues via enqueueContent() with rendered markdown.
      Ref: cmd/ctxt/cmd/import_discord.go (full pattern)
    effort: S
    priority: P1
    tags: [phase:1, cli, importer]
    blocked-by: [4]

  - title: "Pipeline step: aisession_parser"
    description: |
      Create internal/pipeline/steps/aisession_parser.go +
      aisession_parser_test.go.
      Type: AISessionParser with BaseContract, Since, MaxItems.
      Contract: Requires=["Source"], Produces=["Metadata"].
      Run reads draft.Source as AI tool dir, calls ScanAll, applies
      since/max filters, stores []map[string]any in
      draft.Metadata["aisession_records"] + count in
      "aisession_import_count".
      Tests: name, contract, run with temp dir.
      Ref: internal/pipeline/steps/discord_parser.go (full pattern)
    effort: S
    priority: P1
    tags: [phase:1, pipeline, importer]
    blocked-by: [4]
---

# AI Session Importer — Plan

Phase 1 implementation following existing importer patterns.
See spec.md for design rationale, source formats, out-of-scope items.

## File Layout

```
internal/importer/aisession/
  record.go          # Kind, Record, RenderContent, FormatSource
  record_test.go
  scanner.go         # ScanDir, frontmatter, hashing
  scanner_test.go
  testdata/          # golden fixtures
  claude.go          # ScanClaudeDir
  claude_test.go
  agent.go           # ScanAgentDir
  agent_test.go
  scan.go            # ScanAll entry point
  scan_test.go

cmd/ctxt/cmd/
  import_aisessions.go   # CLI: ctxt import ai-sessions

internal/pipeline/steps/
  aisession_parser.go       # pipeline step
  aisession_parser_test.go
```

## Dependency Chain

```
T0 record types
 └─ T1 scanner
     ├─ T2 claude scanner
     ├─ T3 agent scanner
     │   └─ T4 ScanAll
     │       ├─ T5 CLI command
     │       └─ T6 pipeline step
     └───────┘
```
