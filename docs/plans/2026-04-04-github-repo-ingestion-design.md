# GitHub Repo Ingestion & Repo Context Layer — Design

Created: 2026-04-04
Status: GRIP sections superseded by `2026-04-05-grip-v2-deep-repo-ingestion.md`
(itself deferred). RCL deferred. Current work:
`2026-07-26-github-ingestion-consolidation.md`.

## Two Features, One Foundational

### Feature 1: Repo Context Layer (RCL)

A persistent knowledge base of repos the user owns or has access to. Any importer can query RCL to cross-reference ingested content against the user's repos — "where does this apply, add value, or bring insight?"

### Feature 2: GitHub Repo Ingestion Pipeline (GRIP)

A pipeline that deeply ingests a GitHub repo through multiple extraction steps, then cross-references findings against RCL.

RCL must exist before GRIP. Other importers benefit from RCL immediately.

---

## Feature 1: Repo Context Layer

### Discovery Sources

Three sources feed RCL, running independently, merging into a single index:

- **Git host API scan.** Config provides host URL, token, and optional org filter. Uses a `HostProvider` interface with adapters for GitHub, GitLab, Gitea.
- **Explicit config.** A `repos.context` key listing remote URLs or `org/*` globs, resolved via host API at sync time.
- **Local directory scan.** Config provides directory roots (e.g. `~/.w/`). Walks directories, reads `.git/config` for remote URLs. No API needed.

Deduplication by `remote_url`. Records merge across sources: local scan adds `local_path`, API adds `access_level`.

### Repo Record

| Field | Source |
|-------|--------|
| `remote_url` | All (canonical key) |
| `full_name` | API or parsed from URL |
| `description` | API or empty |
| `language` | API or empty |
| `local_path` | Local scan only |
| `host` | Parsed from remote URL |
| `access_level` | API (owner/collaborator/member) |
| `discovery_source` | Which source(s) found it |

### Repo Profile (Semantic Fields)

Deep analysis (shallow clone + pipeline) produces a profile beyond metadata:

| Field | Example |
|-------|---------|
| `intent` | "CLI tool for browser automation" |
| `interests` | ["headless browsers", "playwright", "web scraping"] |
| `tech_stack` | ["Go 1.26", "Node.js 22", "SQLite"] |
| `platform_constraints` | ["requires CGO", "macOS/Linux only"] |
| `challenges` | ["sqlite vector search with CGO enabled"] |
| `dependencies_notable` | ["chromedp", "go-sqlite3", "cobra"] |
| `oss_gaps` | ["no OSS alternative to X service"] |
| `build_requirements` | ["Docker for integration tests"] |
| `maturity` | RSX health signals (third-party repos only) |

**Analysis depth by ownership:**

- **Owned/collaborator repos:** Deep analysis. All fields except `maturity` (RSX is for evaluating third-party repos, not your own).
- **Third-party repos:** Lightweight (README + API metadata). Only `intent`, `interests`, `tech_stack` populated. Promoted repos get deep analysis including `maturity` via RSX.

Profiles are stored as ctxt objects — searchable, linkable, referenced by other ingestions.

### Querying RCL

Importers call RCL with a topic, tags, or dependency list and get ranked matches. Ranking uses metadata overlap (language, description keywords, dependency graphs when available).

---

## Feature 2: GitHub Repo Ingestion Pipeline (GRIP)

### Pipeline Steps

When a repo URL is submitted for ingestion:

**Step 1: Shallow clone + pipeline.**
Clone with `--depth 1`. Run ctxt's pipeline on the source. Discard clone after extraction. Gym skill scan (Step 5) runs in parallel, sharing the clone.

**Step 2: DeepWiki extraction.**
Construct DeepWiki URL from repo org/name. Use the extraction strategy system to fetch and convert to markdown. Extract architecture, design decisions, component explanations. Store as chunk objects with backlinks to source.

**Step 3: GitSummarize extraction.**
Same approach for GitSummarize. Different extraction recipe, different insights (commit patterns, contributor analysis, project evolution).

**Step 4: RSX analysis.**
Run `rsx analyze <repo-url> --markdown`. Produces health/trust signals. Skipped for owned/collaborator repos.

**Step 5: gym scan.**
Run gym against the cloned source (shares clone with Step 1). Identify learnable skills. Store findings as chunk objects.

**Step 6: RCL cross-reference.**
Query RCL with the repo's profile. Return ranked matches of owned/accessible repos where this repo could apply, add value, or bring insight. Store as link objects.

**Step 7: Resync summary.**
Create or update the living summary object (see Resync section).

Steps 1+5 share the clone. Steps 2, 3, 4 are independent and run in parallel.

### Extraction Strategy System

Two methods for web sources:

- **dlweb + ContentConverter** — download static HTML, convert to markdown. Fast, light.
- **IBR** — headless browser automation with tailored instructions. Handles JS-rendered content.

Strategy selection flow:

1. First run: try dlweb + converter (cheaper). If result is too thin or JS-dependent, fall back to IBR.
2. Cache the winning strategy in a registry keyed by `(site_domain, page_type)`.
3. Subsequent runs reuse the cached strategy.
4. User can override per-site in config.
5. Cache invalidated on `--refresh` or when the cached method produces poor results.

IBR instructions are per-site template files, not hardcoded logic.

### Resync

Running ingestion on a previously ingested repo triggers resync mode.

**Resync flow:**

1. Re-run all GRIP steps, producing new chunk objects.
2. Diff new chunks against existing chunks for the same repo.
3. Classify each finding:
   - **Still valid** — semantically equivalent. No new chunk, existing retained.
   - **Replaced** — old chunk superseded. New chunk created, old marked stale with reference to replacement.
   - **New** — no prior equivalent. New chunk created.

**Living Summary Object:**

One object per ingested repo, evolving across resyncs. Contains no duplicated content — only references:

- **Current findings** — links to valid chunk objects
- **Replacements** — old-to-new chunk pairs with explanation of what changed
- **New this sync** — links to chunks that appeared for the first time
- **Sync metadata** — timestamp, steps run, strategy used

Each resync produces a new version. Previous versions retained as history. Latest version is canonical. Chunks marked stale keep a `stale` flag and backlink to their replacement.

---

## Interface Architecture

Every external tool purpose is an interface with pluggable adapters:

| Interface | Purpose | Adapters |
|-----------|---------|----------|
| `RepoAnalyzer` | Health/trust signals | `rsx`, future tools |
| `SkillScanner` | Skill discovery | `gym`, future tools |
| `SiteDownloader` | Fetch web pages | `dlweb`, `ibr`, future tools |
| `ContentConverter` | HTML to markdown | `markitdown`, `turnitdown`, future tools |
| `SectionLinker` | Backlink enrichment | `xray`, token-heavy fallback |
| `HostProvider` | Git host API | `github`, `gitlab`, `gitea` |

Users pick adapters per interface in config, with sensible defaults. The strategy system applies on top for `SiteDownloader` + `ContentConverter` pairings.

---

## Configuration

```yaml
repo_context:
  hosts:
    - url: github.com
      token_env: GITHUB_TOKEN
      orgs: ["ideacrafterslabs"]
    - url: git.custom-domain.com
      token_env: GITEA_TOKEN

  explicit:
    - "git@github.com:someorg/specific-repo.git"
    - "ideacrafterslabs/*"

  local_roots:
    - "~/.w/"

  promote:
    - "git@github.com:charmbracelet/bubbletea.git"

ingestion:
  github_repo:
    strategies:
      deepwiki.com:
        method: dlweb
      gitsummarize.com:
        method: ibr

    adapters:
      repo_analyzer: rsx
      skill_scanner: gym
      site_downloader: dlweb
      content_converter: markitdown
      section_linker: xray

    # Binary paths (defaults to $PATH lookup)
    rsx_binary: rsx
    gym_binary: gym
    dlweb_binary: dlweb
```

Strategy cache is stored in ctxt's data directory, not config. Config holds only explicit user overrides.

---

## External Tool Dependencies

| Tool | Purpose | Required? |
|------|---------|-----------|
| `rsx` | Repo health/trust signals | Optional — step skipped if missing |
| `gym` | Skill discovery | Optional — step skipped if missing |
| `dlweb` | Static site download | Required (one of dlweb or IBR) |
| `ibr` | Browser automation | Optional — fallback/alternative to dlweb |
| `markitdown` / `turnitdown` | HTML to markdown | Required when using dlweb |
| `xray` | Section backlinking | Optional — saves tokens, degrades gracefully without |

---

## Build Order

1. **RCL** — foundational. Other importers use it immediately.
2. **Extraction strategy system** — site-adaptive method selection + caching. Reusable beyond GRIP.
3. **GRIP pipeline** — wires together clone, extraction, RSX, gym, RCL cross-reference, and resync summary.
