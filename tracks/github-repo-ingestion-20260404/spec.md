# GitHub Repo Ingestion & Repo Context Layer

## Overview

Two features that enable deep understanding of GitHub repositories and cross-referencing against the user's own repos.

**Feature 1: Repo Context Layer (RCL)** — a persistent knowledge base of repos the user owns or has access to. Fed by git host API scan, explicit config, and local directory scan. Stores semantic profiles (intent, tech stack, challenges, platform constraints). Any importer can query RCL to find which of the user's repos relate to ingested content.

**Feature 2: GitHub Repo Ingestion Pipeline (GRIP)** — a pipeline that deeply ingests a repo through shallow clone + pipeline analysis, DeepWiki extraction, GitSummarize extraction, RSX health analysis, gym skill scanning, and RCL cross-referencing. Produces chunk objects with source backlinks and a living resync summary that evolves across syncs.

## Functional Requirements

### FR-1: Repo Discovery

Three discovery sources populate RCL: git host API scan (GitHub/GitLab/Gitea via `HostProvider` interface), explicit config entries, and local directory scanning.

- Acceptance: Repos discovered from all three sources, deduplicated by remote URL, records merged across sources

### FR-2: Repo Profiles

Deep analysis (shallow clone + pipeline) produces semantic profiles for owned/collaborator repos. Lightweight analysis (README + API metadata) for third-party repos.

- Acceptance: Owned repos have intent, interests, tech_stack, platform_constraints, challenges, dependencies_notable, oss_gaps, build_requirements. Third-party repos have intent, interests, tech_stack. Promoted third-party repos get full profile plus RSX maturity.

### FR-3: Interface Architecture

Every external tool purpose is an interface with pluggable adapters: `RepoAnalyzer` (rsx), `SkillScanner` (gym), `SiteDownloader` (dlweb, ibr), `ContentConverter` (markitdown, turnitdown), `SectionLinker` (xray), `HostProvider` (github, gitlab, gitea).

- Acceptance: Adapters are user-configurable in config.yaml with sensible defaults

### FR-4: Extraction Strategy System

Site-adaptive method selection for web sources. System tries dlweb+converter first, falls back to IBR if result is too thin. Caches winning strategy per (site_domain, page_type). User can override per-site.

- Acceptance: Strategy auto-detected on first run, cached for future runs, configurable per-site, invalidated on --refresh

### FR-5: GRIP Pipeline Steps

Seven-step pipeline: (1) shallow clone + pipeline, (2) DeepWiki extraction, (3) GitSummarize extraction, (4) RSX analysis (third-party only), (5) gym skill scan, (6) RCL cross-reference, (7) resync summary. Steps 1+5 share clone. Steps 2+3+4 run in parallel.

- Acceptance: All steps produce chunk objects, cross-references stored as link objects, source backlinks present on all chunks

### FR-6: Resync & Living Summary

Resync diffs new chunks against existing, classifying as still-valid, replaced, or new. Living summary object references chunks without duplicating content. Each resync produces a new summary version superseding the previous.

- Acceptance: Stale chunks flagged with backlink to replacement, summary references valid chunks, replacements shown as old-to-new pairs, sync metadata recorded

### FR-7: RCL Cross-Referencing

Any importer can query RCL with topic, tags, or dependency list to find matching repos. GRIP uses this to surface where ingested repos apply, add value, or bring insight to the user's repos.

- Acceptance: Ranked matches returned based on metadata overlap, results stored as link objects

## Non-Functional Requirements

### NFR-1: Optional Dependencies

All external tools except one of dlweb/IBR are optional. Steps degrade gracefully when their tool is missing. xray saves tokens but absence only increases cost, not failure.

### NFR-2: Pluggable Adapters

New tools can be added as adapters without modifying core pipeline logic.

### NFR-3: Cached Strategies

Strategy cache stored in data directory, not config. Survives across runs. User overrides in config take precedence.

## Scope

### In Scope

- Repo Context Layer with three discovery sources
- Semantic repo profiles with ownership-based analysis depth
- Interface architecture with pluggable adapters
- Extraction strategy system with auto-detection and caching
- GRIP pipeline (clone, DeepWiki, GitSummarize, RSX, gym, RCL, resync)
- Living resync summary object
- Configuration for hosts, adapters, strategies, promoted repos

### Out of Scope

- Non-GitHub repo ingestion (future: use HostProvider adapters)
- Automatic resync scheduling (manual trigger only)
- Web UI for strategy or profile management
- gym installation or setup (assumes available on PATH)

## Dependencies

### Internal

- Existing GitHub importer (`internal/importer/github/`)
- Pipeline system (`internal/pipeline/`)
- Storage layer (`internal/storage/`)
- Config system (`internal/config/`)
- IBR integration (tracks/ibr-browser_20260403) — for IBR adapter

### External

- `rsx` (optional) — repo health analysis
- `gym` (optional) — skill scanning
- `dlweb` (required, or IBR) — static site download
- `markitdown` / `turnitdown` (required with dlweb) — HTML conversion
- `ibr` (optional) — browser automation
- `xray` (optional) — section backlinking

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Deep analysis of many repos is slow | Medium | Owned repos only get deep; third-party stays lightweight unless promoted |
| DeepWiki/GitSummarize site structure changes | Medium | Per-site IBR instruction templates; strategy fallback |
| Missing external tools | Low | Steps skip gracefully; clear error messages |
| Large repo clones consume disk | Low | Shallow clone + discard after extraction |

## Open Questions

- [ ] Quality threshold for strategy fallback from dlweb to IBR — what metric?
- [ ] Should RCL profiles auto-refresh on a schedule or only on manual sync?
- [ ] How should gym findings integrate with existing ctxt skill objects?
