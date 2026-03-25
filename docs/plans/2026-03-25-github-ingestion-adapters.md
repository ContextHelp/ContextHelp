# GitHub Ingestion Adapters & Plugin Pattern Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build GitHub ingestion adapters (per-URL pipeline) and a bulk importer (starred, watched, following, issues), backed by idx as the fetch/scrape runtime. Four post-ingest enrichers handle dependency linking, repo health scoring, alternative detection, and release watching. The implementation serves dual purpose: genuine utility for any workspace, and canonical reference for plugin authors.

**Architecture:** Five layers — idx client bridge (`internal/client`), shared extraction core (`internal/github`), per-URL pipelines (`url.github.*`), bulk importer (`internal/importer/github`), and post-ingest enricher plugins. All layers share the same typed snapshot structs. The client bridge is configurable: daemon, subprocess, or auto (tries daemon first, falls back to subprocess).

**Type taxonomy:** `type` describes what the thing is (`repo`, `issue`, `pull_request`, `release`, `person`, `org`, `package`). `subtype` describes where it came from (`github`, `gitlab`, `npm`, `pypi`, `go`). `metadata.forge` carries the forge name; `metadata.forge_id` carries the platform's native ID. GitLab and Bitbucket add `subtype: "gitlab"` / `"bitbucket"` and reuse everything else unchanged.

**Tech Stack:** Go, existing `pluginapi.Plugin` + `PostIngestHook` interfaces, existing `URLPatternDetector`, existing jobs system, idx (Playwright-based browser runtime).

---

## Existing code — read before starting

| What | Where |
|---|---|
| `URLPatternDetector` | `internal/pipeline/detectors.go` |
| `url.generic` pipeline (reference) | `internal/pipeline/builtins/url_generic.go` |
| `pluginapi.Plugin` + `PostIngestHook` | `pkg/pluginapi/pluginapi.go` |
| Existing pipeline steps | `internal/pipeline/steps/` |
| Existing importer pattern | `internal/importer/pinboard/api.go` |
| Existing plugin pattern | `plugins/autosuggest/` |
| Jobs system | `internal/jobs/` |
| idx Operations API | `~/.w/ideacrafterslabs/idx/hops/main/src/Operations.js` |
| idx daemon/server | `~/.w/ideacrafterslabs/idx/hops/main/src/daemon.js`, `server.js` |

---

## Phase 1 — Foundation

### Task 1.1 — `internal/client` idx bridge

Create `internal/client/idx.go` with:

```go
type Intent struct {
    URL         string
    Extractions []ExtractionField
    Actions     []string  // optional pre-extraction actions
}

type ExtractionField struct {
    Name   string
    Prompt string
}

type Result struct {
    Fields map[string]any
    Raw    string  // cleaned page text
}

type Client interface {
    Fetch(ctx context.Context, intent Intent) (Result, error)
    Close() error
}
```

Two implementations behind `New(cfg Config) (Client, error)`:
- `DaemonClient` — HTTP POST to idx server endpoint
- `SubprocessClient` — spawns idx binary, reads NDJSON from stdout

Config:

```go
type Config struct {
    Mode     string        // "daemon" | "subprocess" | "auto"
    Endpoint string        // daemon mode: "http://localhost:7842"
    Bin      string        // subprocess mode: path to idx binary, auto-detected if empty
    Timeout  time.Duration // default 30s
}
```

`auto` tries daemon first (health check), falls back to subprocess. Reads from ctxt config block:

```yaml
idx:
  mode: auto
  endpoint: "http://localhost:7842"
  bin: ""
  timeout: 30s
```

**Tests:** unit test `SubprocessClient` with a mock idx script that echoes NDJSON; integration test `DaemonClient` against a real idx daemon (skipped if not running).

### Task 1.2 — Refactor `url_fetcher` step

Replace the raw HTTP fetch in `internal/pipeline/steps/url_fetcher.go` with a call to `client.Client`. Behaviour is unchanged — callers see no difference. The step receives the client via its constructor (injected at pipeline build time).

### Task 1.3 — Type taxonomy constants

Create `internal/types/types.go` with named constants for all `Type` and `Subtype` values. Document the convention: `Type` = what it is, `Subtype` = where it came from.

Write `docs/plugins/type-taxonomy.md` — the canonical type registry. Third-party adapters must use these values or register new ones here.

---

## Phase 2 — GitHub adapter

### Task 2.1 — `internal/github` extraction core

Create `internal/github/snapshot.go` with typed structs:

```go
type RepoSnapshot struct {
    Owner, Name, FullName, URL string
    Description, Language      string
    Topics                     []string
    License                    string
    Stars, Forks, OpenIssues   int
    IsArchived, IsFork         bool
    CreatedAt, UpdatedAt, PushedAt time.Time
    README                     string
    Sections                   []pluginapi.Section
    Dependencies               []Dependency
    Releases                   []Release  // last 5
}

type Dependency struct {
    Name, Version, Ecosystem string  // Ecosystem: "npm"|"go"|"pip"|etc.
}

type Release struct {
    Tag, Name, Body string
    PublishedAt     time.Time
}
```

Implement `FetchRepo(ctx, client.Client, url string) (RepoSnapshot, error)` — builds the idx `Intent` with extraction fields for a GitHub repo page, calls the client, maps result into `RepoSnapshot`.

Implement `FetchIssue` and `FetchPR` similarly.

`ForgeSnapshot` interface with the same shape — GitLab/Bitbucket implement this. Pipelines and enrichers work against `ForgeSnapshot`.

**Tests:** table-driven unit tests with a mock client returning fixture NDJSON.

### Task 2.2 — `url.github.repo` pipeline

Create `internal/pipeline/builtins/url_github.go`.

Register detector:
```go
MustRegisterDetector(NewURLPatternDetector("url.github.repo",
    regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/?$`)))
```

Register pipeline:
```go
MustRegister("url.github.repo", Def{
    Description: "GitHub repository: metadata, README, dependencies, releases",
    Steps: []string{"github_repo_fetcher", "github_repo_mapper", "tagger", "embedding"},
})
```

`github_repo_fetcher` step: calls `github.FetchRepo`, stores `RepoSnapshot` in `draft.Metadata["_github_snapshot"]`.

`github_repo_mapper` step: reads snapshot from metadata, populates `KnowledgeObject` fields:
- `Type: "repo"`, `Subtype: "github"`
- `TextContent`: README text
- `Sections`: from snapshot sections
- `Metadata`: stars, forks, language, topics, license, forge, forge_id, dependencies, latest release
- `Tags`: from topics + language

**Tests:** contract tests verifying `Requires`/`Produces` on each step; integration test against a fixture snapshot.

### Task 2.3 — `url.github.issue` pipeline

Same pattern as 2.2. Detector: `.../issues/\d+`. Mapper sets `Type: "issue"`, `Subtype: "github"`, populates body, labels, state, linked PR references in metadata.

### Task 2.4 — `url.github.pr` pipeline

Same pattern. Detector: `.../pull/\d+`. Mapper sets `Type: "pull_request"`, `Subtype: "github"`.

### Task 2.5 — `url.github.release` pipeline

Same pattern. Detector: `.../releases`. Mapper sets `Type: "release"`, `Subtype: "github"`, populates changelog body and semver tag.

---

## Phase 3 — Importer

### Task 3.1 — `internal/importer/github`

```go
type Config struct {
    Username        string
    ImportStarred   bool
    ImportWatched   bool
    ImportFollowing bool  // users + orgs
    ImportIssues    bool  // assigned + participating
    MaxItems        int   // 0 = unlimited
    StalenessWindow time.Duration  // default 24h; skip if source exists and updated_at is within window
}
```

`Run(ctx context.Context, bus pluginapi.Bus) error` — iterates each enabled import type, paginates via idx through GitHub web UI, calls `github.FetchRepo` (or FetchUser/FetchOrg) per item, upserts via storage.

Deduplication: skip objects where `source` already exists and `updated_at` is within `StalenessWindow`. Override with `--force`.

Progress via event bus — the existing TUI job display handles it.

### Task 3.2 — CLI wiring

```
ctxt import github --starred
ctxt import github --watched
ctxt import github --following
ctxt import github --issues
ctxt import github            # all of the above
ctxt import github --force    # ignore staleness window
```

---

## Phase 4 — Enrichers

All four are registered as plugins in `plugins/github_enrichers/`.

### Task 4.1 — `repo_health_enricher`

`PostIngestHook` — fires on `type: "repo"`. No idx fetch needed — computes from already-ingested data:
- Commit recency score (days since `metadata.pushed_at`)
- Archive/deprecation flag (`metadata.is_archived` + README deprecation signals in `text_content`)
- Bus factor signal (single-maintainer hints in README)

Writes to `metadata.health: { recency_score, is_archived, bus_factor_risk }`.

### Task 4.2 — `alternative_detector`

`PostIngestHook` — fires on `type: "repo"`. Queries storage for other `type:repo` objects sharing ≥2 topics or same primary language with embedding similarity above threshold. Creates proximity edges with `relation: "alternative"`. Powers cross-repo comparison.

### Task 4.3 — `dependency_enricher`

`PostIngestHook` — fires on `type: "repo"`. Reads `metadata.dependencies`. For each dependency, checks if `source` already exists — if not, enqueues for ingestion as a `package` object. Creates an edge between the repo and each dependency. Depth limit: 1 (no recursive dependency-of-dependency).

### Task 4.4 — `release_watcher`

Periodic job registered in the jobs system. Config:

```yaml
jobs:
  release_watcher:
    enabled: true
    interval: 24h
    min_reinforcement: 1  # only watch repos the user has reinforced
```

For each `type:repo, subtype:github` object with `reinforcement_count >= min_reinforcement`: re-fetches releases via idx, diffs against stored releases, emits events for new entries. New releases surface in the TUI and on the event bus.

---

## Phase 5 — Plugin authoring docs

### Task 5.1 — `docs/plugins/authoring-guide.md`

Overview: concepts, plugin types, when to use each hook. Links to pattern docs.

### Task 5.2 — Pattern docs

Each doc: problem it solves → interface to implement → GitHub example → minimal copy-paste skeleton.

```
docs/plugins/
  authoring-guide.md
  type-taxonomy.md
  patterns/
    url-adapter.md          — detector + pipeline (github_repo as example)
    post-ingest-hook.md     — enricher (dependency_enricher as example)
    periodic-job.md         — watcher (release_watcher as example)
    extraction-core.md      — shared data package (internal/github as example)
  examples/
    github/                 — full annotated source
```

### Task 5.3 — Annotated GitHub source

Copy key files from the GitHub implementation into `docs/plugins/examples/github/` with inline comments explaining every decision. This is the primary teaching artifact.

---

## Build order & dependencies

```
Phase 1 (Tasks 1.1→1.3) — must complete first; everything depends on client bridge
Phase 2 (Tasks 2.1→2.5) — depends on Phase 1; tasks 2.2–2.5 depend on 2.1
Phase 3 (Tasks 3.1→3.2) — depends on Phase 2
Phase 4 (Tasks 4.1→4.4) — depends on Phase 2; tasks 4.1–4.4 are independent of each other
Phase 5 (Tasks 5.1→5.3) — depends on Phase 4 complete; can draft in parallel
```

Each phase is independently mergeable. Within Phase 4, all four enrichers can be built in parallel.
