# AI Session Importer

Import learnings, decisions, patterns from locally saved AI-assisted
work sessions into dPKMS/ctxt as first-class knowledge objects.

## Problem

AI tool sessions (Claude Code, Cursor, Copilot) produce valuable
artifacts: implementation plans, project memories, feedback records,
lessons learned. These sit in tool-specific directories, unsearchable
by ctxt. Knowledge decays as sessions age.

## Sources (Phase 1 — pre-distilled markdown)

| Source | Path | Format | Kind |
|--------|------|--------|------|
| Claude plans | `~/.claude/plans/*.md` | markdown | plan |
| Claude memory | `~/.claude/projects/*/memory/*.md` | markdown+frontmatter | memory |
| Agent lessons | `~/.agents/lessons/*.md` | markdown+frontmatter | lesson |

## Sources (Phase 2 — structured)

| Source | Path | Format | Kind |
|--------|------|--------|------|
| Claude tasks | `~/.claude/tasks/{session}/*.json` | JSON | task |

## Sources (Phase 3 — raw + LLM extraction)

| Source | Path | Format | Kind |
|--------|------|--------|------|
| Claude history | `~/.claude/history.jsonl` | JSONL | history |

## Design Decisions

- **Phase 1 first**: pre-distilled content is already high-quality
  markdown; importer is mostly scanner + normalizer + deduper
- **ExternalID**: `sha256:<content_hash>` — content-addressed dedup;
  re-import detects changes via hash mismatch
- **Source format**: `ai-session:<tool>:<kind>:<identifier>`
- **Skip index files**: MEMORY.md is an index, not content
- **Project attribution**: decode Claude's URL-encoded dir names back
  to original paths

## Follows pattern of

- `internal/importer/discord/` — parser.go + parser_test.go
- `internal/importer/obsidian/` — WalkVault, ParseNote, RenderContent
- `cmd/ctxt/cmd/import_discord.go` — CLI with --since/--max-items/--dry-run
- `internal/pipeline/steps/discord_parser.go` — pipeline step

## Out of scope (Phase 1)

- Cursor/Copilot/Windsurf log formats
- LLM-powered extraction from raw history
- Bi-directional sync
- Real-time file watching
