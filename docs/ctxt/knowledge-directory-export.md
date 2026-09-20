# Design note: knowledge-directory export target

> Status: **design note** — no implementation exists yet. Companion to [knowledge-directory.md](knowledge-directory.md), which covers the inbound direction (files → ctxt). This note sketches the outbound direction (ctxt → files).

## Idea

A future compose/export target that materializes **selected entities** into a directory of markdown files with frontmatter, so downstream tools (editors, static-site generators, other agents) can consume curated knowledge as plain files:

```markdown
---
generated-by: ctxt
tags: [ui, best-practice]
description: High-quality UI design guidelines.
---

# UI Best Practice

...
```

The frontmatter shape mirrors the [knowledge-directory source pattern](knowledge-directory.md) (`tags`, `description`), plus one mandatory marker: `generated-by: ctxt`.

## Single-writer rule

A file has exactly one writer — **either** human-edited (and possibly watched as a source) **or** ctxt-generated. Never both:

- The exporter **refuses to overwrite** any existing file that lacks the `generated-by: ctxt` marker. Files it did generate are safe to regenerate; anything else is presumed human-owned and untouchable.
- The export directory must be disjoint from any watched knowledge directory. The source pattern's authority rule (filesystem is truth, ctxt never writes into the watched directory) stays intact: export writes only into directories the operator names explicitly, and even there only over its own marked files.
- Round-tripping (export → hand-edit → re-ingest) is intentionally out of scope: hand-editing a generated file removes it from ctxt ownership only by deleting the marker, at which point the exporter will refuse to touch it again.

## Open questions

- **Selection surface** — what picks the entities: a query expression (`ctxt find` filter), an explicit slug list, a tag/namespace scope, or a saved profile?
- **Update cadence** — one-shot command only, or a recurring compose job re-materializing on change? If recurring, what dedup/versioning avoids churning file mtimes?
- **Deletion semantics** — when an entity leaves the selection, does its generated file get deleted, orphan-marked, or left in place? Deletion conflicts with "never destroy without inspecting"; orphan-marking leaks stale files.
- **Frontmatter fidelity** — `description` and `weight` are not yet mapped on the ingest side; should export emit them anyway (forward-compatible) or only fields the round trip can honor?

## See also

- [knowledge-directory.md](knowledge-directory.md) — inbound source pattern + authority rule
- [resolver-contract.md](resolver-contract.md) — one-shot per-ref retrieval, the pull-based alternative to materialized files
