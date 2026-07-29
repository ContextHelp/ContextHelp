---
title: "GitHub Repo Ingestion & Repo Context Layer"
tracks:
  - github-repo-ingestion-20260404
tasks:
  - title: "Define adapter interfaces (RepoAnalyzer, SkillScanner, SiteDownloader, ContentConverter, SectionLinker, HostProvider)"
    effort: M
    priority: P1
    tags: [phase:1, rcl, interfaces]

  - title: "Implement HostProvider adapters (GitHub, GitLab, Gitea) for git host API scanning"
    effort: L
    priority: P1
    tags: [phase:1, rcl, adapters]
    blocked-by: [0]

  - title: "Add repo_context config section (hosts, explicit, local_roots, promote)"
    effort: S
    priority: P1
    tags: [phase:1, rcl, config]

  - title: "Build repo discovery engine (API scan, explicit resolve, local directory walk)"
    effort: L
    priority: P1
    tags: [phase:1, rcl, discovery]
    blocked-by: [0, 2]

  - title: "Implement repo record storage and deduplication by remote_url"
    effort: M
    priority: P1
    tags: [phase:1, rcl, storage]
    blocked-by: [3]

  - title: "Build repo profile extractor (shallow clone + pipeline for semantic fields)"
    effort: L
    priority: P1
    tags: [phase:1, rcl, profiles]
    blocked-by: [4]

  - title: "Implement ownership-based analysis depth (deep for owned, lightweight for third-party)"
    effort: M
    priority: P1
    tags: [phase:1, rcl, profiles]
    blocked-by: [5]

  - title: "Implement RCL query API (topic, tags, dependency matching with ranked results)"
    effort: M
    priority: P1
    tags: [phase:1, rcl, query]
    blocked-by: [4]

  - title: "Implement SiteDownloader adapters (dlweb, ibr)"
    effort: M
    priority: P2
    tags: [phase:2, strategy, adapters]
    blocked-by: [0]

  - title: "Implement ContentConverter adapters (markitdown, turnitdown)"
    effort: S
    priority: P2
    tags: [phase:2, strategy, adapters]
    blocked-by: [0]

  - title: "Build extraction strategy engine (auto-detect, fallback, cache per site+page_type)"
    effort: L
    priority: P2
    tags: [phase:2, strategy]
    blocked-by: [8, 9]

  - title: "Add strategy config (per-site overrides, adapter selection) and strategy cache storage"
    effort: S
    priority: P2
    tags: [phase:2, strategy, config]
    blocked-by: [10]

  - title: "Implement RepoAnalyzer adapter (rsx --markdown)"
    effort: S
    priority: P2
    tags: [phase:2, adapters]
    blocked-by: [0]

  - title: "Implement SkillScanner adapter (gym)"
    effort: S
    priority: P2
    tags: [phase:2, adapters]
    blocked-by: [0]

  - title: "Implement SectionLinker adapter (xray) with token-heavy fallback"
    effort: M
    priority: P2
    tags: [phase:2, adapters]
    blocked-by: [0]

  - title: "Build GRIP pipeline step 1: shallow clone + ctxt pipeline + discard"
    effort: M
    priority: P3
    tags: [phase:3, grip, pipeline]
    blocked-by: [5]

  - title: "Build GRIP pipeline step 2: DeepWiki extraction via strategy system"
    effort: M
    priority: P3
    tags: [phase:3, grip, pipeline]
    blocked-by: [10]

  - title: "Build GRIP pipeline step 3: GitSummarize extraction via strategy system"
    effort: M
    priority: P3
    tags: [phase:3, grip, pipeline]
    blocked-by: [10]

  - title: "Build GRIP pipeline step 4: RSX analysis (third-party repos only)"
    effort: S
    priority: P3
    tags: [phase:3, grip, pipeline]
    blocked-by: [12]

  - title: "Build GRIP pipeline step 5: gym skill scan (shares clone with step 1)"
    effort: S
    priority: P3
    tags: [phase:3, grip, pipeline]
    blocked-by: [13, 15]

  - title: "Build GRIP pipeline step 6: RCL cross-reference with link object storage"
    effort: M
    priority: P3
    tags: [phase:3, grip, pipeline]
    blocked-by: [7, 15]

  - title: "Build GRIP pipeline orchestrator (parallel execution of independent steps)"
    effort: M
    priority: P3
    tags: [phase:3, grip, pipeline]
    blocked-by: [15, 16, 17, 18, 19, 20]

  - title: "Build resync diff engine (compare new chunks against existing, classify as valid/replaced/new)"
    effort: L
    priority: P4
    tags: [phase:4, grip, resync]
    blocked-by: [21]

  - title: "Build living summary object (references to chunks, replacement pairs, sync metadata)"
    effort: M
    priority: P4
    tags: [phase:4, grip, resync]
    blocked-by: [22]

  - title: "Implement summary versioning (new version supersedes previous, history retained)"
    effort: S
    priority: P4
    tags: [phase:4, grip, resync]
    blocked-by: [23]

  - title: "Implement stale chunk flagging with backlinks to replacements"
    effort: S
    priority: P4
    tags: [phase:4, grip, resync]
    blocked-by: [22]

  - title: "Add GRIP ingestion command (ctxt ingest --github <repo-url>)"
    effort: M
    priority: P4
    tags: [phase:4, grip, cli]
    blocked-by: [21, 24]

  - title: "Integration tests: RCL discovery from all three sources"
    effort: M
    priority: P4
    tags: [phase:4, testing]
    blocked-by: [3]

  - title: "Integration tests: GRIP full pipeline with mock external tools"
    effort: L
    priority: P4
    tags: [phase:4, testing]
    blocked-by: [21]

  - title: "Integration tests: resync diff and living summary evolution"
    effort: M
    priority: P4
    tags: [phase:4, testing]
    blocked-by: [24]
---

# Implementation Plan: GitHub Repo Ingestion & Repo Context Layer

Track ID: `github-repo-ingestion_20260404`
Created: 2026-04-04
Status: pending

## Overview

Two features: (1) Repo Context Layer — a persistent knowledge base of the user's repos, queryable by any importer. (2) GitHub Repo Ingestion Pipeline — deep repo analysis through clone, web extraction, RSX, gym, and cross-referencing.

Detailed design: `docs/plans/2026-04-04-github-repo-ingestion-design.md`

## Phase 1: Repo Context Layer (RCL)

### Tasks

- [ ] **Task 1.1**: Define adapter interfaces (`RepoAnalyzer`, `SkillScanner`, `SiteDownloader`, `ContentConverter`, `SectionLinker`, `HostProvider`)
  - Create `internal/adapters/` package with interface definitions
  - Write contract tests for each interface
- [ ] **Task 1.2**: Implement `HostProvider` adapters (GitHub, GitLab, Gitea)
  - Extend existing GitHub client with `HostProvider` interface
  - Add GitLab and Gitea adapters
- [ ] **Task 1.3**: Add `repo_context` config section
  - Add `hosts`, `explicit`, `local_roots`, `promote` to config struct
  - Write test for config loading with defaults
- [ ] **Task 1.4**: Build repo discovery engine
  - API scan, explicit resolve, local directory walk
  - Merge and deduplicate by `remote_url`
- [ ] **Task 1.5**: Implement repo record storage and deduplication
  - Store in ctxt storage layer, deduplicate by `remote_url`
- [ ] **Task 1.6**: Build repo profile extractor
  - Shallow clone + pipeline for semantic fields (intent, interests, tech_stack, platform_constraints, challenges, dependencies_notable, oss_gaps, build_requirements)
- [ ] **Task 1.7**: Implement ownership-based analysis depth
  - Deep for owned/collaborator, lightweight for third-party, deep+RSX for promoted
- [ ] **Task 1.8**: Implement RCL query API
  - Accept topic, tags, dependency list; return ranked matches

### Verification

- [ ] **Verify 1.1**: `go test ./internal/adapters/ -v` passes
- [ ] **Verify 1.2**: `go test ./internal/adapters/github/ ./internal/adapters/gitlab/ ./internal/adapters/gitea/ -v` passes
- [ ] **Verify 1.3**: `go test ./internal/config/ -run TestRepoContext -v` passes
- [ ] **Verify 1.4**: Discovery returns repos from all three sources, deduplicated

## Phase 2: Extraction Strategy System & Adapters

### Tasks

- [ ] **Task 2.1**: Implement `SiteDownloader` adapters (dlweb, ibr)
- [ ] **Task 2.2**: Implement `ContentConverter` adapters (markitdown, turnitdown)
- [ ] **Task 2.3**: Build extraction strategy engine
  - Auto-detect best method, fallback on poor results, cache per `(site_domain, page_type)`
- [ ] **Task 2.4**: Add strategy config and cache storage
  - Per-site overrides in config, strategy cache in data directory
- [ ] **Task 2.5**: Implement `RepoAnalyzer` adapter (rsx `--markdown`)
- [ ] **Task 2.6**: Implement `SkillScanner` adapter (gym)
- [ ] **Task 2.7**: Implement `SectionLinker` adapter (xray, with token-heavy fallback)

### Verification

- [ ] **Verify 2.1**: `go test ./internal/adapters/... -v` passes for all adapters
- [ ] **Verify 2.2**: Strategy engine selects dlweb for static sites, IBR for JS-heavy sites
- [ ] **Verify 2.3**: Strategy cache persists across runs

## Phase 3: GRIP Pipeline

### Tasks

- [ ] **Task 3.1**: Build step 1 — shallow clone + ctxt pipeline + discard
- [ ] **Task 3.2**: Build step 2 — DeepWiki extraction via strategy system
- [ ] **Task 3.3**: Build step 3 — GitSummarize extraction via strategy system
- [ ] **Task 3.4**: Build step 4 — RSX analysis (skipped for owned repos)
- [ ] **Task 3.5**: Build step 5 — gym skill scan (shares clone with step 1)
- [ ] **Task 3.6**: Build step 6 — RCL cross-reference with link object storage
- [ ] **Task 3.7**: Build pipeline orchestrator (steps 1+5 share clone, steps 2+3+4 parallel)

### Verification

- [ ] **Verify 3.1**: Each step produces chunk objects independently
- [ ] **Verify 3.2**: Orchestrator runs independent steps in parallel
- [ ] **Verify 3.3**: All chunks have source backlinks (xray or fallback)

## Phase 4: Resync, CLI & Integration Tests

### Tasks

- [ ] **Task 4.1**: Build resync diff engine (valid/replaced/new classification)
- [ ] **Task 4.2**: Build living summary object (chunk references, replacement pairs, sync metadata)
- [ ] **Task 4.3**: Implement summary versioning (new supersedes previous, history retained)
- [ ] **Task 4.4**: Implement stale chunk flagging with backlinks to replacements
- [ ] **Task 4.5**: Add GRIP ingestion command (`ctxt ingest --github <repo-url>`)
- [ ] **Task 4.6**: Integration tests — RCL discovery from all three sources
- [ ] **Task 4.7**: Integration tests — GRIP full pipeline with mock external tools
- [ ] **Task 4.8**: Integration tests — resync diff and living summary evolution

### Verification

- [ ] **Verify 4.1**: Resync correctly classifies chunks across two successive runs
- [ ] **Verify 4.2**: Living summary references chunks without duplicating content
- [ ] **Verify 4.3**: Full pipeline produces expected objects for a real public repo
- [ ] **Verify 4.4**: `go test ./... -v` all green

## Checkpoints

| Phase | Checkpoint SHA | Date | Status |
|-------|---------------|------|--------|
| Phase 1 | | | pending |
| Phase 2 | | | pending |
| Phase 3 | | | pending |
| Phase 4 | | | pending |
