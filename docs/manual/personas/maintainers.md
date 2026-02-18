# Maintainers Guide

This chapter is for core maintainers responsible for contract stability, release discipline, and ecosystem coherence.

## Goal

Evolve features without breaking plugin/runtime contracts, and keep documentation aligned with executable behavior.

## Prerequisites

1. Working repository checkout and current branch context.
2. Runtime commands available:

```bash
ctxt version
dpkms version
```

3. Configuration validates:

```bash
ctxt config validate
```

## Current Implementation Path (Use This Today)

### 1. Audit command surface and runtime contracts

```bash
ctxt --help
dpkms --help
```

Inspect critical command groups:

```bash
ctxt analyze --help
ctxt find --help
ctxt make --help
dpkms pipeline --help
dpkms pipeline step --help
```

### 2. Verify docs against runtime behavior

Check manual and API references for drift:

```bash
rg -n "query-schema|RSQL|ctxt add|ctxt analyze" docs/manual docs/stories docs/api
```

Validate key indexes and mappings:

```bash
sed -n '1,220p' docs/manual/appendix/story-to-chapter-index.md
sed -n '1,220p' docs/manual/appendix/persona-to-chapter-index.md
```

### 3. Validate pipeline and extension governance

```bash
dpkms pipeline list --include-archived
dpkms pipeline step registry list
```

Confirm config/permission references are in sync:

```bash
sed -n '1,220p' docs/manual/reference/config-and-permissions.md
sed -n '1,220p' docs/plugins/plugin-isolation.md
```

### 4. Validate operations-readiness docs

```bash
sed -n '1,260p' docs/manual/operations/runbook.md
sed -n '1,260p' docs/manual/troubleshooting/faq.md
```

### 5. Release-quality doc checks

Before release:

1. Confirm story-status snapshot is current.
2. Confirm manual chapters use current command names.
3. Confirm any forward-looking contracts are clearly labeled.
4. Confirm runbook commands are executable in current runtime.

## 30-Minute Maintainer Smoke Test

1. Validate config and command surfaces.
2. Check for obvious command/doc drift via `rg`.
3. Review pipeline/step registry state.
4. Review appendices for persona/story coverage gaps.
5. Verify runbook and troubleshooting references remain aligned.

## Quality Checklist

- No undocumented breaking command/API changes.
- Manual examples match current CLI flags and subcommands.
- Story-target vs runtime behavior is explicitly separated.
- Extension and permission guidance remains coherent.
- Operations guidance is actionable and current.

## Common Pitfalls and Fixes

### Breaking contracts without migration path

- Add compatibility notes and transition windows.
- Keep deprecated and replacement paths explicit in docs.

### Story/docs drift

- Update story mappings when chapters are added or moved.
- Re-run command-surface checks before release tags.

### Cross-layer ownership ambiguity

- Assign clear owners for runtime, docs, and plugin interfaces per release.

## Story Alignment

- Maintainer-focused stories: `US-0008`, `US-0015`, `US-0027` to `US-0035`, `US-0042`, `US-0043`, `US-0101` to `US-0112`, `US-0300` to `US-0317`

## Related Manual Sections

- [`../admin-extensibility/admin-configuration.md`](../admin-extensibility/admin-configuration.md)
- [`../admin-extensibility/plugin-development.md`](../admin-extensibility/plugin-development.md)
- [`../appendix/feature-maturity.md`](../appendix/feature-maturity.md)
- [`../appendix/story-to-chapter-index.md`](../appendix/story-to-chapter-index.md)
