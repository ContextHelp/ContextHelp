# Knowledge-directory source pattern

A **knowledge directory** is a repo-local folder of markdown files — team conventions, runbooks, decision notes, lessons — that a team maintains in version control and wants surfaced through ctxt search and composition. This document describes how to wire such a directory in as a content source, what its files' frontmatter does (and does not) map to today, and the authority rule that governs the relationship.

> The filesystem is the source of truth. ctxt is an index over it. ctxt **never writes into the watched directory** — no sidecar files, no frontmatter rewrites, no generated content. Edits flow one way: author → files → ingestion → ctxt.

## The pattern

```text
my-repo/
  docs/
  knowhow/                  ← knowledge directory
    onboarding.md
    incident-response.md
    api-conventions.md
```

Each file is a self-contained markdown note, optionally carrying YAML frontmatter:

```markdown
---
title: Incident response
tags: [ops, runbook]
description: First 30 minutes of any production incident.
weight: 8
---

# Incident response

When paged, first ...
```

The directory stays a plain folder of plain files: editable in any editor, reviewable in PRs, diffable, greppable. ctxt observes it; it never owns it.

## Wiring it in as a source

Use the directory watcher in `ctxt watch`. It runs on your machine and sends each file's content to the dpkms instance ctxt resolves, over the API.

`ctxt.yaml`:

```yaml
watch:
  dirs:
    - ~/work/my-repo/knowhow         # a leading ~ is expanded; must exist
  patterns: ["**/*.md"]
  debounce: 500ms
```

Then:

```bash
ctxt watch start     # blocks; Ctrl-C or `ctxt watch stop` to end
ctxt watch status
```

On start each directory is scanned once and files changed since the last run against this instance are sent; after that, saves are sent as they happen. What was already sent is recorded per instance under `$XDG_STATE_HOME/ctxt/watch/`. A deleted file deletes the object it produced (that needs an `admin` token on a protected instance).

Each file is sent whole as one object (`type` from the extension, `source` = `watch:<mode>:<relative path>`). A directory containing `.obsidian/` or `logseq/` is tagged with that mode in `source`; the content is sent the same way.

> The `files.local-fs` and `watched-fs` sensors and `policy/ambient.yaml` from the [ADR-065 amendment](../decisions/ADR-065-pluggable-adapters.md#amendment-2026-05-06--service-vs-sensor-distinction-ambient-capture-os-platform-slots) are not run by any command: `ctxt capture --ambient` exits "not yet implemented" and nothing reads `policy/ambient.yaml`. See [../ambient.md](../ambient.md#not-implemented).

### One-shot alternative: the Obsidian importer

If the knowledge directory is (or resembles) an Obsidian vault, `ctxt import obsidian --vault <dir>` performs a one-shot batch import with markdown-aware parsing — frontmatter, inline `#tags`, wikilinks, attachments. Use the importer for a backfill that maps titles and tags, `ctxt watch` for ongoing freshness.

Like `ctxt watch`, it sends to the dpkms instance ctxt resolves (`--server`, `--instance`, the current instance, then config); start dpkms first.

```bash
ctxt import obsidian --vault ~/work/my-repo/knowhow --dry-run   # preview
ctxt import obsidian --vault ~/work/my-repo/knowhow             # enqueue
```

## Frontmatter mapping — current behavior

Frontmatter handling differs by path, and today only the **importer** path parses it.

### Via `ctxt import obsidian` (markdown-aware)

| Frontmatter field | Current behavior |
|---|---|
| `title` | Mapped. Becomes the note title (falls back to filename stem). |
| `tags` / `tag` | Mapped. Merged with inline `#tags`, deduplicated, rendered into the submitted content's `Tags:` line; the pipeline tagging step turns them into ctxt tags. |
| `description` | **Not yet mapped.** Parsed into the note's raw frontmatter map but not carried into any ctxt field. Knowledge objects have a pipeline-produced `summary`, not an author-supplied description. |
| `weight` | **Not yet mapped.** Parsed but unused. ctxt tag `weight` (see [schema-tag.md](schema-tag.md)) is pipeline-assigned per tag, not read from frontmatter. |
| anything else | Parsed into the raw frontmatter map; not mapped. |

### Via `ctxt watch`

No frontmatter parsing. The file is sent as text with its frontmatter block included; the markdown pipeline splits sections by heading and does not read frontmatter keys, so `title`, `tags`, `description` and `weight` are not mapped to fields.

> **Gap**: no ingestion path maps frontmatter `description` or `weight` into ctxt fields, and `ctxt watch` maps no frontmatter at all. Tags are the reliable frontmatter → ctxt signal, via the importer.

Frontmatter `tags` are also distinct from ctxt **hints** (`#hint` markers — see [hints.md](hints.md)): hints are transient pipeline guidance, while frontmatter/inline tags feed the tagging step that produces persistent tags.

## Authority rule

- **Filesystem is truth.** The markdown files are the canonical artifact — versioned, reviewed, merged like any other repo content.
- **ctxt is index.** Re-ingesting the same content is idempotent: dedup by content hash reinforces the existing object instead of duplicating it (see [../dpkms/jobs-and-ingestion.md](../dpkms/jobs-and-ingestion.md), Step 5). Deleting the index loses nothing: re-run `ctxt import obsidian`, or remove the instance's state file under `$XDG_STATE_HOME/ctxt/watch/` so `ctxt watch start` sends every file again.
- **ctxt never writes into the watched directory.** `ctxt watch` and the importer only read it. `ctxt export` prints one object as JSON or YAML to stdout; there is no directory export.

For the outbound direction — materializing selected entities into a generated markdown directory — see the [export design note](knowledge-directory-export.md).

## See also

- [ADR-065 amendment](../decisions/ADR-065-pluggable-adapters.md#amendment-2026-05-06--service-vs-sensor-distinction-ambient-capture-os-platform-slots) — sensor catalog and `policy/ambient.yaml` design (not implemented)
- [../ambient.md](../ambient.md) — local capture that runs today
- [capture.md](capture.md) — `ctxt capture` command spec
- [hints.md](hints.md) — hints vs tags vs mentions
- [schema-tag.md](schema-tag.md) — tag schema (weight, confidence, provenance)
- [../dpkms/jobs-and-ingestion.md](../dpkms/jobs-and-ingestion.md) — job lifecycle, dedup + reinforcement
