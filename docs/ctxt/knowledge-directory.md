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

Two ambient sensors cover the file-watching use case (see [ambient.md](ambient.md) for the full sensor catalog and `policy/ambient.yaml` schema):

- **`files.local-fs`** — scan-on-tick: each sweep walks the tree and emits the full state. Best default for a knowledge directory; every sweep reconciles the index with reality.
- **`watched-fs`** — fsnotify event stream: emits deltas (created/modified files) since the last sweep. Lower latency between save and ingestion, but a daemon restart loses pending events (Phase 2 limitation noted in the adapter).

### Runnable source config

`$XDG_CONFIG_HOME/contexthelp/policy/ambient.yaml`:

```yaml
sensors:
  - name: files.local-fs
    enabled: true
    config:
      roots:
        - path: ~/work/my-repo/knowhow
          recursive: true
          ignore: [".DS_Store", "*.tmp"]
```

Then sweep:

```bash
ctxt capture --ambient
```

Or, for save-to-index latency, watch instead of scan:

```yaml
sensors:
  - name: watched-fs
    enabled: true
    platform: [darwin, linux]
    config:
      paths:
        - ~/work/my-repo/knowhow
```

Both sensors are **metadata-only emitters**: they produce one ingest object per file carrying `path`, `size`, `modtime`, and `root` — file bodies are loaded downstream by pipelines, not by the sensor. This keeps sweeps cheap even over large trees.

### One-shot alternative: the Obsidian importer

If the knowledge directory is (or resembles) an Obsidian vault, `ctxt import obsidian --vault <dir>` performs a one-shot batch import with markdown-aware parsing — frontmatter, inline `#tags`, wikilinks, attachments. Use the importer for the initial backfill, the ambient sensor for ongoing freshness.

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

### Via ambient sensors (`files.local-fs`, `watched-fs`)

No frontmatter parsing at the sensor layer. Sensors emit file metadata only; body loading and any semantic extraction happen in downstream pipelines. The watcher performs one lightweight pre-semantic pass — `@namespace.slug` mention-hint extraction from paths and metadata — but hints are optimization only; pipelines remain authoritative.

> **Honest gap**: there is currently no ingestion path that maps frontmatter `description` or `weight` into ctxt fields, and the ambient file sensors do not parse frontmatter at all. If you rely on `description:` or `weight:` in your knowledge files today, treat them as author-facing metadata that ctxt preserves in raw form (importer path) or ignores (ambient path). Tags are the reliable frontmatter → ctxt signal, via the importer.

Frontmatter `tags` are also distinct from ctxt **hints** (`#hint` markers — see [hints.md](hints.md)): hints are transient pipeline guidance, while frontmatter/inline tags feed the tagging step that produces persistent tags.

## Authority rule

- **Filesystem is truth.** The markdown files are the canonical artifact — versioned, reviewed, merged like any other repo content.
- **ctxt is index.** Re-ingesting the same content is idempotent: dedup by content hash reinforces the existing object instead of duplicating it (see [../dpkms/jobs-and-ingestion.md](../dpkms/jobs-and-ingestion.md), Step 5). Deleting the index loses nothing; a fresh sweep rebuilds it.
- **ctxt never writes into the watched directory.** Export flows (`ctxt export --format obsidian-md --dest <dir>`) target a destination the operator names explicitly; pointing an export at a watched knowledge directory is an operator choice, not something ctxt does on its own.

## See also

- [ambient.md](ambient.md) — `policy/ambient.yaml` schema, sensor catalog, permission model
- [capture.md](capture.md) — `ctxt capture` command spec
- [hints.md](hints.md) — hints vs tags vs mentions
- [schema-tag.md](schema-tag.md) — tag schema (weight, confidence, provenance)
- [../dpkms/jobs-and-ingestion.md](../dpkms/jobs-and-ingestion.md) — job lifecycle, dedup + reinforcement
