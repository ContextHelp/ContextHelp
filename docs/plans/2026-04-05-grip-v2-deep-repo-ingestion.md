# GRIP v2 — Deep Repository Ingestion Pipeline

Created: 2026-04-05
Supersedes: 2026-04-04-github-repo-ingestion-design.md (GRIP sections)
Extends: 2026-03-25-github-ingestion-adapters.md (implementation plan)

## Overview

GRIP v2 expands the GitHub Repo Ingestion Pipeline from 7 steps to 12.
New steps extract API metadata (stars history, contributors, stargazers,
dependents, owner), package registry health, README version history,
adaptive code analysis, and marketing surface discovery. A budgeted job
dispatch system drives follow-up ingestion prioritized by occurrence
count and signal strength.

The Repo Context Layer (RCL) design from the April 4 doc remains
unchanged. The March 25 adapters plan remains the implementation
blueprint for per-URL pipelines, bulk importer, and enrichers.

---

## Pipeline Steps

| # | Step | Source | Output |
|---|------|--------|--------|
| 1 | Shallow clone + adaptive xray | git clone | Structural map, DFG/CFG |
| 2 | API metadata | GitHub REST API | Repo record, stars, people, dependents |
| 3 | DeepWiki extraction | deepwiki.com | Architecture/design markdown |
| 4 | GitSummarize extraction | gitsummarize.com | Commit patterns, evolution |
| 5 | Package registry integration | deps.dev + libraries.io APIs | Dep health, drill-down |
| 6 | RSX analysis | `rsx` binary | Health/trust (third-party only) |
| 7 | gym scan | `gym` binary | Learnable skills |
| 8 | README history | GitHub API | Versioned snapshots + diffs |
| 9 | Marketing surface discovery | Probe + search sweep | Landing pages, backlinks |
| 10 | RCL cross-reference | Repo Context Layer | Relevance links |
| 11 | Job dispatch | Internal | Budgeted follow-up jobs |
| 12 | Resync summary | Internal | Living summary object |

**Parallelism:** Steps 1+7 share the clone. Steps 2-6 and 8-9 run
independently in parallel. Steps 10-12 run after all others complete.

---

## Step 1: Shallow Clone + Adaptive Code Analysis

Expands the original structural map to include DFG/CFG on high-value
files.

**Flow:**

1. Shallow clone (`--depth 1`)
2. `xray map` — structural overview with PageRank scores per file
3. `xray strategy detect` — identify frameworks, languages, build systems
4. Entry point detection — `main` funcs, HTTP handlers, exported API
   surface, CLI commands
5. For files above PageRank threshold (configurable, default top 20%)
   OR identified as entry points:
   - `xray map --anchor {file} --with cf` — control flow
   - DFG extraction where supported

**Stored as:**

```
type: "code_analysis", subtype: "github"
metadata:
  repo_full_name: "{owner}/{repo}"
  frameworks: ["Next.js", "Express", "Tailwind"]
  languages: {"TypeScript": 0.72, "CSS": 0.18, "Shell": 0.10}
  entry_points: ["src/index.ts", "src/server.ts"]
  analyzed_files: 34
  total_files: 170
  pagerank_threshold: 0.20
sections:
  - title: "Architecture Overview"
    content: <xray map output>
  - title: "Control Flow: src/index.ts"
    content: <xray --with cf output>
```

**Caps:** `max_analyzed_files` (default 50). xray's `--limit` flag caps
per-file token output.

Gym scan (Step 7) shares this clone and runs in parallel.

---

## Step 2: API Metadata Extraction

Pulls structured data from GitHub's REST API.

### Sub-extractions

| Sub-step | Endpoint | Output |
|---|---|---|
| Repo metadata | `GET /repos/{owner}/{repo}` | Enriches repo object |
| Stars history | `GET /repos/{owner}/{repo}/stargazers` (star+json) | Timestamped array |
| Contributors | `GET /repos/{owner}/{repo}/contributors` | `type: "person"` stubs |
| Stargazers | `GET /repos/{owner}/{repo}/stargazers` | `type: "person"` stubs |
| Dependents | GitHub page + deps.dev + libraries.io | `type: "repo"` stubs |
| Owner | `GET /users/{owner}` or `GET /orgs/{owner}` | Person or org object |
| Org members | `GET /orgs/{owner}/public_members` | `type: "person"` stubs |

### Person Stub

```
type: "person", subtype: "github"
metadata:
  github_login, name, bio, company, location,
  blog_url, twitter_handle, email (if public)
  occurrence_count: N
  occurrence_sources: ["contributor:owner/repo", "stargazer:owner/other"]
```

Each stub is deduped by `github_login`. When the same user appears
across multiple extractions, `occurrence_count` increments and
`occurrence_sources` accumulates. This drives job priority.

A follow-up job is created for deeper social profile ingestion. The job
carries `triggered_by` context referencing what surfaced the person.

### Rate Limiting

GitHub API: 5k req/hour authenticated. Pagination capped:
- `max_stargazers`: 1000
- `max_dependents`: 100
- Contributors: naturally bounded (<100 typical)

---

## Steps 3-4: DeepWiki + GitSummarize (unchanged)

Same as original design. Construct URL from repo org/name. Use
extraction strategy system (dlweb first, IBR fallback). Store as chunk
objects with backlinks to source.

---

## Step 5: Package Registry Integration

Aggregate dependency health from deps.dev and libraries.io, with
selective drill-down on flagged packages.

**Flow:**

1. Read `metadata.dependencies[]` from Step 1 (xray detects manifests:
   `go.mod`, `package.json`, `requirements.txt`, etc.)
2. Query deps.dev: `GET /v3/systems/{eco}/packages/{name}` — version
   history, advisory count, OpenSSF scorecard
3. Query libraries.io: `GET /api/{platform}/{name}` — maintenance
   score, dependent count, latest release date
4. Merge into per-dependency record:

```
type: "package", subtype: "{ecosystem}"
metadata:
  latest_version, current_version_in_repo
  advisories: [{id, severity, summary}]
  scorecard: {score, checks}
  maintenance_score: N
  dependent_count: N
  last_release_at: timestamp
  is_flagged: bool
```

`is_flagged` triggers when: active CVE, outdated >2 major versions, or
no release in >1 year.

5. **Selective drill-down** — only for `is_flagged: true`: fetch full
   advisory details, alternative packages, produce
   `metadata.risk_summary` for LLM composition.

### Dependents (reverse direction)

For the ingested repo itself, fetch who depends on it:
- deps.dev: `GET /v3/systems/{eco}/packages/{name}/dependents`
- libraries.io: `GET /api/{platform}/{name}/dependent_repositories`
- GitHub: scrape `/network/dependents` (fallback, paginated HTML)

Merge + dedup by repo URL. Store as `type: "repo"` stubs with edge
`relation: "depends_on"`. Capped at `max_dependents`.

### API Keys

```yaml
deps_dev: {}                # public API, 150 req/min
libraries_io:
  api_key_env: LIBRARIES_IO_API_KEY  # optional, higher limits
```

---

## Step 6: RSX Analysis (unchanged)

`rsx analyze <repo-url> --markdown`. Skipped for owned/collaborator
repos.

---

## Step 7: gym Scan (unchanged)

Shares clone with Step 1. Identifies learnable skills. Stored as chunk
objects.

---

## Step 8: README History

Full version snapshots of README.md from git history, correlated with
stars data for pattern analysis.

**Extraction via GitHub API** (avoids deepening the clone):

1. `GET /repos/{owner}/{repo}/commits?path=README.md` — list commit
   SHAs that touched README
2. Per commit: `GET /repos/{owner}/{repo}/contents/README.md?ref={sha}`
   — fetch file at that revision

**Stored as independent objects:**

```
type: "document", subtype: "readme_version"
source: "github.com/{owner}/{repo}"
metadata:
  repo_full_name: "{owner}/{repo}"
  commit_sha: "abc123"
  commit_date: "2025-06-15T10:30:00Z"
  commit_message: "Add Docker instructions"
  version_number: 14
  total_versions: 23
  diff_from_previous: "..."        # unified diff
  stars_at_commit: N               # interpolated from stars_history
text_content: <full README at this revision>
```

`stars_at_commit` interpolated from Step 2's stars history — nearest
star count to `commit_date`. Enables direct correlation: README changes
that precede star acceleration, plateau, or decline.

**Cap:** `max_readme_versions` (default 50, most recent).

---

## Step 9: Marketing Surface Discovery

Two phases: known-pattern probing (high precision), then search sweep
(broad recall).

### Phase A — Known-Pattern Probe

| Signal | Method |
|---|---|
| Homepage | `metadata.homepage` from API |
| GitHub Pages | Probe `https://{owner}.github.io/{repo}/` |
| Custom domain | `CNAME` file in clone |
| Docs site | ReadTheDocs, GitBook, `/docs` patterns |
| Package registry page | npm, PyPI, pkg.go.dev, crates.io (inferred) |

Each discovered URL fetched via extraction strategy system (dlweb/ibr),
converted to markdown, stored as `type: "url"` with edge
`relation: "marketing_surface"`.

### Phase B — Search Sweep

Queries constructed from repo name + URL:
- `"{owner}/{repo}"` (exact match)
- `"{repo}" site:reddit.com`
- `"{repo}" site:news.ycombinator.com`
- `"{repo}" site:medium.com`
- `"{repo}" site:dev.to`

Search via SearxNG (if configured) or web search API fallback.

```
type: "mention", subtype: "{platform}"
metadata:
  repo_full_name: "{owner}/{repo}"
  platform: "reddit" | "hackernews" | "medium" | "dev.to" | "other"
  mention_url: "..."
  title: "..."
  snippet: "..."
  discovered_at: timestamp
  score: N              # upvotes/points if available
```

Mention stubs only — no content fetch. Full fetch is a follow-up job
via the dispatch system.

**Caps:** `max_mentions_per_platform` (default 20),
`max_mentions` (default 100).

---

## Step 10: RCL Cross-Reference (unchanged)

Query RCL with the repo's profile. Return ranked matches of
owned/accessible repos. Store as link objects.

---

## Step 11: Job Dispatch

After all extraction steps complete, GRIP dispatches follow-up jobs
using priority + budget + on-demand.

### Job Types

| Job type | Trigger | Priority signal |
|---|---|---|
| `social_profile_ingest` | Person stub | `occurrence_count` |
| `dependency_drill_down` | Flagged package | Advisory severity |
| `mention_content_fetch` | Mention stub | Platform score |
| `dependent_repo_expand` | Repo stub (dependent) | Star count |
| `marketing_surface_deep` | URL from Phase A | Homepage > docs > registry |

### Dispatch Flow

1. Collect all dispatchable entities from steps 2, 5, 9
2. Score each by priority signal
3. Sort descending
4. Take top N up to `job_budget` (default 50)
5. Enqueue with `triggered_by` context:

```
job:
  type: "social_profile_ingest"
  target: "github.com/users/somedev"
  priority: 7
  triggered_by:
    source: "grip:github.com/owner/repo"
    reasons: ["contributor:owner/repo", "stargazer:owner/other"]
  status: "queued"
```

6. Remaining entities stay as stubs with `expandable: true`
7. On-demand: `ctxt expand <object-id>` or reinforce the stub

### Budget Config

```yaml
job_budget: 50
job_budget_per_type:
  social_profile_ingest: 20
  dependency_drill_down: 10
  mention_content_fetch: 10
  dependent_repo_expand: 5
  marketing_surface_deep: 5
```

Per-type caps prevent one category from consuming the entire budget.
Unspent budget does not redistribute.

---

## Step 12: Resync + Living Summary

### Resync Triggers

- Manual: `ctxt ingest github.com/owner/repo` on previously ingested repo
- Scheduled: repos with `reinforcement_count >= N` on configurable interval

### Diff-Aware Steps

| Step | Resync behavior |
|---|---|
| Stars history | Append new data points only |
| README history | Fetch versions after last known commit SHA |
| Contributors/stargazers | Full re-fetch, diff stubs, update `occurrence_count` |
| Dependents | Full re-fetch, diff, new stubs get job dispatch |
| deps.dev/libraries.io | Re-check flagged + newly added deps |
| Mentions | Re-run search sweep, dedup against known |
| Code analysis | Re-run xray, diff structural changes |

### Living Summary Object

```
type: "repo_summary", subtype: "github"
metadata:
  repo_full_name: "{owner}/{repo}"
  sync_version: 3
  synced_at: timestamp
  steps_run: [1,2,3,4,5,6,7,8,9,10,11,12]
  strategy_used: {deepwiki: "dlweb", gitsummarize: "ibr"}
links:
  current_findings: [object_ids...]
  replaced: [{old: id, new: id, reason: "..."}]
  new_this_sync: [object_ids...]
  jobs_dispatched: [job_ids...]
  stars_trend: "accelerating" | "plateau" | "declining"
  readme_correlation: "last README change +3 days -> star spike"
```

`stars_trend` and `readme_correlation` are LLM-generated from raw
stars history + README versions during the summary step.

---

## Interface Architecture (unchanged)

| Interface | Purpose | Adapters |
|---|---|---|
| `RepoAnalyzer` | Health/trust signals | `rsx` |
| `SkillScanner` | Skill discovery | `gym` |
| `SiteDownloader` | Fetch web pages | `dlweb`, `ibr` |
| `ContentConverter` | HTML to markdown | `markitdown`, `turnitdown` |
| `SectionLinker` | Backlink enrichment | `xray` |
| `HostProvider` | Git host API | `github`, `gitlab`, `gitea` |

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
    - "~/src/"
  promote:
    - "git@github.com:charmbracelet/bubbletea.git"

ingestion:
  github_repo:
    # Extraction strategies
    strategies:
      deepwiki.com:
        method: dlweb
      gitsummarize.com:
        method: ibr

    # Adapter selection
    adapters:
      repo_analyzer: rsx
      skill_scanner: gym
      site_downloader: dlweb
      content_converter: markitdown
      section_linker: xray

    # Caps
    max_stargazers: 1000
    max_dependents: 100
    max_readme_versions: 50
    max_mentions_per_platform: 20
    max_mentions: 100
    max_analyzed_files: 50
    pagerank_threshold: 0.20

    # Job dispatch
    job_budget: 50
    job_budget_per_type:
      social_profile_ingest: 20
      dependency_drill_down: 10
      mention_content_fetch: 10
      dependent_repo_expand: 5
      marketing_surface_deep: 5

    # Resync
    resync_interval: 168h
    resync_min_reinforcement: 1

  # External APIs
  deps_dev: {}
  libraries_io:
    api_key_env: LIBRARIES_IO_API_KEY
  searxng:
    url: "http://localhost:8080"
```

---

## External Tool Dependencies

| Tool | Purpose | Required? |
|------|---------|-----------|
| `rsx` | Health/trust signals | Optional — step skipped |
| `gym` | Skill discovery | Optional — step skipped |
| `dlweb` | Static site download | Required (one of dlweb or ibr) |
| `ibr` | Browser automation | Optional — fallback to dlweb |
| `markitdown` / `turnitdown` | HTML to markdown | Required with dlweb |
| `xray` | Code analysis + backlinking | Optional — degrades gracefully |

No new binaries. All new steps use GitHub API, deps.dev API,
libraries.io API, SearxNG, and existing tools.

---

## Build Order

```
Phase 1 — Foundation (from March 25 plan)
  idx client bridge, url_fetcher refactor, type taxonomy

Phase 2 — GitHub adapter core (from March 25 plan)
  Extraction core, per-URL pipelines (repo, issue, PR, release)

Phase 3 — API metadata extraction (NEW)
  GitHub API client, stars history, contributors, stargazers,
  dependents, owner/org, person stub model, occurrence tracking

Phase 4 — Package registry integration (NEW)
  deps.dev client, libraries.io client, aggregate fetch,
  flagging logic, selective drill-down

Phase 5 — README history (NEW)
  GitHub API commit-path queries, version snapshots,
  stars interpolation, diff generation

Phase 6 — Code analysis expansion (NEW)
  Adaptive xray (PageRank threshold + entry points),
  DFG/CFG extraction, framework detection storage

Phase 7 — Marketing surface discovery (NEW)
  Known-pattern prober, search sweep via SearxNG,
  mention stub model

Phase 8 — Job dispatch (NEW)
  Priority scoring, budget allocation, per-type caps,
  on-demand expansion CLI (ctxt expand)

Phase 9 — Bulk importer + enrichers (from March 25 plan)
  Starred/watched/following/issues import, health enricher,
  alternative detector, dependency enricher, release watcher

Phase 10 — Resync + living summary (extends original)
  Diff-aware re-extraction, summary object,
  stars trend + README correlation computation

Phase 11 — Plugin authoring docs (from March 25 plan)
  Authoring guide, pattern docs, annotated examples
```

**Dependencies:**
- Phase 1 -> everything
- Phase 2 -> Phases 3-10
- Phases 3-7 -> independent of each other, all need Phase 2
- Phase 8 -> needs Phases 3, 4, 7 (entities to dispatch)
- Phase 9 -> needs Phase 2
- Phase 10 -> needs all extraction phases (3-7)
- Phase 11 -> needs Phase 9

---

## Future Exploration (lower confidence)

Ideas that surfaced during design. Revisit when evidence or need
clarifies.

- **Funding signals** — OpenCollective, GitHub Sponsors, Tidelift.
  Correlates with maintenance sustainability. API availability unclear.
- **CI/CD health** — GitHub Actions success rates, build times.
  Public via API but noisy, hard to normalize.
- **Issue/PR sentiment** — LLM-scored discussion tone. Signals
  community health. Expensive at scale.
- **Fork network analysis** — active divergent forks = potential
  alternatives or abandoned upstream. Volume can be huge.
- **Conference/talk discovery** — YouTube, SlideShare, Speaker Deck.
  Signals maturity. Search-dependent, low precision.
- **Changelog extraction** — CHANGELOG.md, releases, conventional
  commits. Complements README history.
- **Security audit history** — past CVEs, audit reports, disclosure
  pages. Scattered sources.
- **License compatibility graph** — dep license cross-reference for
  conflict detection. Data exists (deps.dev) but interpretation is
  nuanced.
- **Contributor mobility** — where contributors go after leaving.
  Signals ecosystem relationships. Needs ethical framing.
- **Package download trends** — npm, PyPI, crates.io stats over time.
  Correlates with adoption. APIs stable.
